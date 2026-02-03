package heaven

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

// PFRequest is a Page Fault request from an Angel.
type PFRequest struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Args    PFArgs `json:"args"`
}

// PFArgs holds command-specific arguments for a PF request.
type PFArgs struct {
	MissionID    string `json:"mission_id,omitempty"`
	Symbol       string `json:"symbol,omitempty"`
	Path         string `json:"path,omitempty"`
	StartLine    int    `json:"start_line,omitempty"`
	N            int    `json:"n,omitempty"`
	Query        string `json:"query,omitempty"`
	TopK         int    `json:"top_k,omitempty"`
	Depth        string `json:"depth,omitempty"` // "full" (default), "signature", "summary"
	PromptID     string `json:"prompt_id,omitempty"`
	SectionIndex int    `json:"section_index,omitempty"`
}

// PFResponse is a Page Fault response from Heaven.
type PFResponse struct {
	Type   string  `json:"type"`
	Shards []Shard `json:"shards"`
	Meta   PFMeta  `json:"meta"`
}

// Shard is a content-addressed piece of context returned by a PF.
type Shard struct {
	Kind   string         `json:"kind"`
	BlobID string         `json:"blob_id"`
	Meta   map[string]any `json:"meta"`
}

// PFMeta holds per-response metadata including mission-scoped counters.
type PFMeta struct {
	PFCount         int64              `json:"pf_count"`
	ShardBytes      int64              `json:"shard_bytes"`
	Prefetched      bool               `json:"prefetched"`
	BudgetRemaining *PFBudgetRemaining `json:"budget_remaining,omitempty"`
}

// PFBudgetRemaining reports how much PF budget remains for a mission.
type PFBudgetRemaining struct {
	PFCallsLeft    int   `json:"pf_calls_left"`
	ShardBytesLeft int64 `json:"shard_bytes_left"`
}

// PFRouter dispatches PF requests to the appropriate IR/blob operations.
type PFRouter struct {
	blobs   *BlobStore
	irIndex *IRIndex
	events  *EventLog
	prompts *PromptStore

	mu       sync.Mutex
	missions map[string]*missionMetrics
}

type missionMetrics struct {
	pfCount    atomic.Int64
	shardBytes atomic.Int64
}

// NewPFRouter creates a PF router backed by the given stores.
func NewPFRouter(blobs *BlobStore, irIndex *IRIndex, events *EventLog, prompts *PromptStore) *PFRouter {
	return &PFRouter{
		blobs:    blobs,
		irIndex:  irIndex,
		events:   events,
		prompts:  prompts,
		missions: make(map[string]*missionMetrics),
	}
}

// Handle dispatches a PF request and returns the response.
func (r *PFRouter) Handle(req PFRequest) (PFResponse, error) {
	if req.Type != "request" {
		return PFResponse{}, fmt.Errorf("pf: invalid type %q, want \"request\"", req.Type)
	}

	m := r.getMetrics(req.Args.MissionID)
	m.pfCount.Add(1)

	var shards []Shard
	var prefetched bool
	var err error

	switch req.Command {
	case "PF_STATUS":
		shards, err = r.handleStatus(req.Args)
	case "PF_SYMDEF":
		shards, prefetched, err = r.handleSymdef(req.Args)
	case "PF_CALLERS":
		shards, err = r.handleCallers(req.Args)
	case "PF_SLICE":
		shards, err = r.handleSlice(req.Args)
	case "PF_SEARCH":
		shards, err = r.handleSearch(req.Args)
	case "PF_TESTS":
		shards, err = r.handleTests(req.Args)
	case "PF_PROMPT_SECTION":
		shards, err = r.handlePromptSection(req.Args)
	case "PF_PROMPT_SEARCH":
		shards, err = r.handlePromptSearch(req.Args)
	case "PF_PROMPT_SUMMARY":
		shards, err = r.handlePromptSummary(req.Args)
	default:
		return PFResponse{}, fmt.Errorf("pf: unknown command %q", req.Command)
	}
	if err != nil {
		return PFResponse{}, err
	}

	// Track shard bytes
	var totalBytes int64
	for _, s := range shards {
		data, err := r.blobs.Get(s.BlobID)
		if err == nil {
			totalBytes += int64(len(data))
		}
	}
	m.shardBytes.Add(totalBytes)

	// Log PF event for metering
	r.logPFEvent(req, len(shards), totalBytes)

	return PFResponse{
		Type:   "response",
		Shards: shards,
		Meta: PFMeta{
			PFCount:    m.pfCount.Load(),
			ShardBytes: m.shardBytes.Load(),
			Prefetched: prefetched,
		},
	}, nil
}

func (r *PFRouter) getMetrics(missionID string) *missionMetrics {
	if missionID == "" {
		missionID = "_default"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.missions[missionID]
	if !ok {
		m = &missionMetrics{}
		r.missions[missionID] = m
	}
	return m
}

func (r *PFRouter) logPFEvent(req PFRequest, shardCount int, shardBytes int64) {
	evt, _ := json.Marshal(map[string]any{
		"type":        "pf",
		"command":     req.Command,
		"mission_id":  req.Args.MissionID,
		"shard_count": shardCount,
		"shard_bytes": shardBytes,
	})
	r.events.Append(json.RawMessage(evt))
}

// storeShard serializes data to JSON, stores it as a blob, and returns a Shard.
func (r *PFRouter) storeShard(kind string, data any, meta map[string]any) (Shard, error) {
	content, err := json.Marshal(data)
	if err != nil {
		return Shard{}, fmt.Errorf("pf shard marshal: %w", err)
	}
	blobID, err := r.blobs.Put(content)
	if err != nil {
		return Shard{}, fmt.Errorf("pf shard store: %w", err)
	}
	return Shard{Kind: kind, BlobID: blobID, Meta: meta}, nil
}

// --- Command handlers ---

func (r *PFRouter) handleStatus(args PFArgs) ([]Shard, error) {
	stats, err := r.irIndex.Stats()
	if err != nil {
		return nil, err
	}
	m := r.getMetrics(args.MissionID)
	statusData := map[string]any{
		"mission_id":  args.MissionID,
		"pf_count":    m.pfCount.Load(),
		"shard_bytes": m.shardBytes.Load(),
		"index_stats": stats,
	}
	shard, err := r.storeShard("status", statusData, map[string]any{"mission_id": args.MissionID})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handleSymdef(args PFArgs) ([]Shard, bool, error) {
	if args.Symbol == "" {
		return nil, false, fmt.Errorf("pf: PF_SYMDEF requires symbol arg")
	}

	depth := args.Depth
	if depth == "" {
		depth = "full"
	}

	var shardData any
	meta := map[string]any{"symbol": args.Symbol, "depth": depth}

	if depth != "full" {
		slices, err := r.irIndex.SymdefWithDepth(args.Symbol, depth)
		if err != nil {
			return nil, false, err
		}
		shardData = slices
	} else {
		syms, err := r.irIndex.Symdef(args.Symbol)
		if err != nil {
			return nil, false, err
		}
		shardData = syms
	}

	symShard, err := r.storeShard("symdef", shardData, meta)
	if err != nil {
		return nil, false, err
	}

	shards := []Shard{symShard}
	prefetched := false

	// Prefetch: include callers
	topK := args.TopK
	if topK <= 0 {
		topK = 10
	}
	refs, err := r.irIndex.Callers(args.Symbol, topK)
	if err == nil && len(refs) > 0 {
		callerShard, err := r.storeShard("callers", refs, map[string]any{"symbol": args.Symbol})
		if err == nil {
			shards = append(shards, callerShard)
			prefetched = true
		}
	}

	// Prefetch: include tests (stub - returns empty for now)
	testsShard, err := r.storeShard("tests", []any{}, map[string]any{"symbol": args.Symbol, "stub": true})
	if err == nil {
		shards = append(shards, testsShard)
	}

	return shards, prefetched, nil
}

func (r *PFRouter) handleCallers(args PFArgs) ([]Shard, error) {
	if args.Symbol == "" {
		return nil, fmt.Errorf("pf: PF_CALLERS requires symbol arg")
	}
	topK := args.TopK
	if topK <= 0 {
		topK = 20
	}
	refs, err := r.irIndex.Callers(args.Symbol, topK)
	if err != nil {
		return nil, err
	}
	shard, err := r.storeShard("callers", refs, map[string]any{"symbol": args.Symbol})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handleSlice(args PFArgs) ([]Shard, error) {
	if args.Path == "" {
		return nil, fmt.Errorf("pf: PF_SLICE requires path arg")
	}
	startLine := args.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	n := args.N
	if n <= 0 {
		n = 20
	}
	content, err := Slice(args.Path, startLine, n)
	if err != nil {
		return nil, err
	}
	shard, err := r.storeShard("slice", map[string]string{"content": content}, map[string]any{
		"path":       args.Path,
		"start_line": startLine,
		"n":          n,
	})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handleSearch(args PFArgs) ([]Shard, error) {
	if args.Query == "" {
		return nil, fmt.Errorf("pf: PF_SEARCH requires query arg")
	}
	topK := args.TopK
	if topK <= 0 {
		topK = 20
	}
	syms, err := r.irIndex.Search(args.Query, topK)
	if err != nil {
		return nil, err
	}
	shard, err := r.storeShard("search", syms, map[string]any{"query": args.Query})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handleTests(args PFArgs) ([]Shard, error) {
	if args.Symbol == "" {
		return nil, fmt.Errorf("pf: PF_TESTS requires symbol arg")
	}
	// Stub: return empty tests until test mapping is implemented
	shard, err := r.storeShard("tests", []any{}, map[string]any{"symbol": args.Symbol, "stub": true})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handlePromptSection(args PFArgs) ([]Shard, error) {
	if args.PromptID == "" {
		return nil, fmt.Errorf("pf: PF_PROMPT_SECTION requires prompt_id arg")
	}
	sec, err := r.prompts.GetSection(args.PromptID, args.SectionIndex)
	if err != nil {
		return nil, err
	}
	shard, err := r.storeShard("prompt_section", sec, map[string]any{
		"prompt_id":     args.PromptID,
		"section_index": args.SectionIndex,
		"section_type":  sec.SectionType,
		"title":         sec.Title,
	})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handlePromptSearch(args PFArgs) ([]Shard, error) {
	if args.PromptID == "" {
		return nil, fmt.Errorf("pf: PF_PROMPT_SEARCH requires prompt_id arg")
	}
	if args.Query == "" {
		return nil, fmt.Errorf("pf: PF_PROMPT_SEARCH requires query arg")
	}
	matches, err := r.prompts.SearchSections(args.PromptID, args.Query)
	if err != nil {
		return nil, err
	}
	// Return each match as a summary (index, title, snippet)
	type matchSummary struct {
		Index       int    `json:"index"`
		Title       string `json:"title"`
		SectionType string `json:"section_type"`
		Snippet     string `json:"snippet"`
	}
	var summaries []matchSummary
	for _, m := range matches {
		snippet := m.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		summaries = append(summaries, matchSummary{
			Index:       m.Index,
			Title:       m.Title,
			SectionType: m.SectionType,
			Snippet:     snippet,
		})
	}
	if summaries == nil {
		summaries = []matchSummary{}
	}
	shard, err := r.storeShard("prompt_search", summaries, map[string]any{
		"prompt_id": args.PromptID,
		"query":     args.Query,
		"matches":   len(summaries),
	})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}

func (r *PFRouter) handlePromptSummary(args PFArgs) ([]Shard, error) {
	if args.PromptID == "" {
		return nil, fmt.Errorf("pf: PF_PROMPT_SUMMARY requires prompt_id arg")
	}
	artifact, err := r.prompts.Get(args.PromptID)
	if err != nil {
		return nil, err
	}
	type sectionSummary struct {
		Index       int    `json:"index"`
		Title       string `json:"title"`
		SectionType string `json:"section_type"`
		ByteLen     int    `json:"byte_len"`
	}
	var summaries []sectionSummary
	for _, sec := range artifact.Sections {
		summaries = append(summaries, sectionSummary{
			Index:       sec.Index,
			Title:       sec.Title,
			SectionType: sec.SectionType,
			ByteLen:     sec.ByteLen,
		})
	}
	shard, err := r.storeShard("prompt_summary", summaries, map[string]any{
		"prompt_id":     args.PromptID,
		"section_count": len(summaries),
		"total_bytes":   artifact.TotalBytes,
	})
	if err != nil {
		return nil, err
	}
	return []Shard{shard}, nil
}
