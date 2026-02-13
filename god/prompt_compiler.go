package god

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// CandidateShard represents a shard fetched from Heaven that is a candidate
// for inclusion in a mission pack. Content is the raw blob bytes.
type CandidateShard struct {
	Kind           string `json:"kind"`    // symdef, callers, tests, slice, search, status
	BlobID         string `json:"blob_id"` // content-addressed blob ID
	Content        []byte `json:"-"`       // raw blob content
	Symbol         string `json:"symbol,omitempty"`
	Path           string `json:"path,omitempty"`
	TestRelevant   bool   `json:"test_relevant,omitempty"`
	HotsetHit      bool   `json:"hotset_hit,omitempty"`
	RecentlyTouched bool  `json:"recently_touched,omitempty"`
}

// ScoredShard is a CandidateShard with its computed salience score and token estimate.
type ScoredShard struct {
	Shard           CandidateShard `json:"shard"`
	Score           float64        `json:"score"`
	EstimatedTokens int            `json:"estimated_tokens"`
}

// MissionPack is the final compiled prompt sent to an Angel. It contains
// a stable header, the mission JSON, selected inline shards, PF endpoint
// info, and budget metadata.
type MissionPack struct {
	Header       string        `json:"header"`
	Mission      Mission       `json:"mission"`
	InlineShards []PackedShard `json:"inline_shards"`
	PFEndpoint   string        `json:"pf_endpoint"`
	BudgetMeta   BudgetMeta    `json:"budget_meta"`
	PromptRef    *PromptRef    `json:"prompt_ref,omitempty"`
	Phase        string        `json:"phase,omitempty"` // solo phase: understand/plan/execute/verify
}

// PromptRef references a content-addressed prompt stored in Heaven.
// Only pinned sections are inlined in the pack; agents page the rest on demand.
type PromptRef struct {
	PromptID          string           `json:"prompt_id"`
	PinnedSections    []int            `json:"pinned_sections"`
	RecommendedByRole map[string][]int `json:"recommended_by_role"`
	TotalSections     int              `json:"total_sections"`
	TotalTokens       int              `json:"total_tokens"`
	InlinedTokens     int              `json:"inlined_tokens"`
}

// PromptSectionInfo is a lightweight descriptor for a prompt section used in scoring.
type PromptSectionInfo struct {
	Index       int    `json:"index"`
	SectionType string `json:"section_type"`
	Title       string `json:"title"`
	Content     string `json:"content"`
	ByteLen     int    `json:"byte_len"`
}

// ScorePromptSection returns a salience score for a prompt section by type.
func ScorePromptSection(sectionType string) float64 {
	scores := map[string]float64{
		"constraints": 3.0,
		"acceptance":  2.5,
		"api":         2.0,
		"security":    2.0,
		"style":       1.5,
		"spec":        1.0,
		"examples":    1.0,
		"glossary":    0.5,
		"other":       0.0,
	}
	if s, ok := scores[sectionType]; ok {
		return s
	}
	return 0.0
}

// PinnedSectionsForRole returns section indices that should be inlined for the given role.
func PinnedSectionsForRole(role string, sections []PromptSectionInfo) []int {
	pinTypes := map[string]map[string]bool{
		"planner":  {"constraints": true, "acceptance": true, "spec": true},
		"builder":  {"api": true, "constraints": true, "examples": true},
		"reviewer": {"acceptance": true, "security": true, "constraints": true},
	}

	types, ok := pinTypes[role]
	if !ok {
		types = pinTypes["builder"] // default
	}

	var pinned []int
	for _, sec := range sections {
		if types[sec.SectionType] {
			pinned = append(pinned, sec.Index)
		}
	}
	return pinned
}

// PackedShard is a shard included in a mission pack with its content inlined.
type PackedShard struct {
	Kind    string          `json:"kind"`
	BlobID  string          `json:"blob_id"`
	Content json.RawMessage `json:"content"`
	Meta    map[string]any  `json:"meta,omitempty"`
}

// BudgetMeta tracks token budget accounting for a mission pack.
type BudgetMeta struct {
	TokenBudget    int `json:"token_budget"`
	HeaderTokens   int `json:"header_tokens"`
	MissionTokens  int `json:"mission_tokens"`
	ShardTokens    int `json:"shard_tokens"`
	TotalTokens    int `json:"total_tokens"`
	ShardsIncluded int `json:"shards_included"`
	ShardsDropped  int `json:"shards_dropped"`
}

// HotsetSummary provides hotset context for shard scoring.
// Keys are blob IDs or symbol names that are in the hot set.
type HotsetSummary map[string]bool

// PromptCompiler selects and packs shards into a mission pack under a token budget.
type PromptCompiler struct {
	PFEndpoint string
}

// NewPromptCompiler creates a PromptCompiler with the given PF endpoint URL.
func NewPromptCompiler(pfEndpoint string) *PromptCompiler {
	return &PromptCompiler{PFEndpoint: pfEndpoint}
}

// EstimateTokens returns a conservative token estimate for a byte slice.
// Uses chars/4 + fixed overhead per shard.
func EstimateTokens(data []byte) int {
	const overhead = 10
	return len(data)/4 + overhead
}

// ScoreShard computes the salience score for a candidate shard.
//
// Scoring formula:
//
//	+3.0 if symdef
//	+2.0 if test_relevant
//	+1.5 if callers
//	+1.0 if hotset_hit
//	+0.5 if recently_touched
//	-0.01 * estimated_tokens
func ScoreShard(shard CandidateShard) float64 {
	score := 0.0

	switch shard.Kind {
	case "symdef":
		score += 3.0
	case "callers":
		score += 1.5
	}

	if shard.TestRelevant {
		score += 2.0
	}
	if shard.HotsetHit {
		score += 1.0
	}
	if shard.RecentlyTouched {
		score += 0.5
	}

	tokens := EstimateTokens(shard.Content)
	score -= 0.01 * float64(tokens)

	return score
}

// ScoreShards deduplicates by BlobID, scores all candidate shards, and returns
// them sorted by descending score. Duplicate shards (same BlobID) are dropped,
// keeping the highest-scoring variant.
func ScoreShards(candidates []CandidateShard) []ScoredShard {
	// Deduplicate by BlobID — same content should only appear once
	seen := make(map[string]int) // BlobID -> index in scored
	var scored []ScoredShard

	for _, c := range candidates {
		s := ScoredShard{
			Shard:           c,
			Score:           ScoreShard(c),
			EstimatedTokens: EstimateTokens(c.Content),
		}
		if c.BlobID != "" {
			if idx, exists := seen[c.BlobID]; exists {
				// Keep higher-scoring duplicate
				if s.Score > scored[idx].Score {
					scored[idx] = s
				}
				continue
			}
			seen[c.BlobID] = len(scored)
		}
		scored = append(scored, s)
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	return scored
}

// Compile takes a mission and candidate shards, scores and packs shards
// under the mission's token budget, and returns a MissionPack.
func (pc *PromptCompiler) Compile(mission Mission, candidates []CandidateShard) (*MissionPack, error) {
	return pc.CompileWithPromptRef(mission, candidates, nil)
}

// CompileWithPromptRef compiles a MissionPack optionally using a PromptRef.
// When a PromptRef is provided, only pinned sections are inlined as prompt_section
// shards, and PF_PROMPT_* instructions are added to the header.
func (pc *PromptCompiler) CompileWithPromptRef(mission Mission, candidates []CandidateShard, promptRef *PromptRef) (*MissionPack, error) {
	// Build stable header
	header := "GENESIS SSMP — Angel Mission Pack\n" +
		"Generated: " + nowFunc().UTC().Format(time.RFC3339) + "\n" +
		"Mission: " + mission.MissionID + "\n" +
		"PF Endpoint: " + pc.PFEndpoint

	// Add PF_PROMPT_* instructions if PromptRef is set
	if promptRef != nil {
		header += "\n\nPROMPT VM:\n" +
			"The full user prompt is stored as artifact " + promptRef.PromptID + ".\n" +
			"Pinned sections are inlined below. To access other sections:\n" +
			"- PF_PROMPT_SECTION(prompt_id, section_index) — page in one section\n" +
			"- PF_PROMPT_SEARCH(prompt_id, query) — find relevant content\n" +
			"- PF_PROMPT_SUMMARY(prompt_id) — list all section titles + types"
	}

	// Estimate fixed overhead tokens
	headerTokens := EstimateTokens([]byte(header))
	missionJSON, err := json.Marshal(mission)
	if err != nil {
		return nil, err
	}
	missionTokens := EstimateTokens(missionJSON)

	budget := mission.TokenBudget
	remaining := budget - headerTokens - missionTokens
	if remaining < 0 {
		remaining = 0
	}

	// If PromptRef is set, add pinned prompt sections as shards first
	var packed []PackedShard
	shardTokensTotal := 0
	if promptRef != nil && len(promptRef.PinnedSections) > 0 {
		for _, sec := range promptRef.PinnedSections {
			// Inline the pinned section as a prompt_section shard
			// The actual content is provided via candidates with kind "prompt_section"
			for _, c := range candidates {
				if c.Kind == "prompt_section" && c.Symbol == fmt.Sprintf("section_%d", sec) {
					tokens := EstimateTokens(c.Content)
					if tokens > remaining {
						continue
					}
					packed = append(packed, PackedShard{
						Kind:    "prompt_section",
						BlobID:  c.BlobID,
						Content: json.RawMessage(c.Content),
						Meta: map[string]any{
							"section_index": sec,
							"source":        "pinned",
						},
					})
					shardTokensTotal += tokens
					remaining -= tokens
					break
				}
			}
		}
	}

	// Score and rank remaining shards (non-prompt-section)
	var regularCandidates []CandidateShard
	for _, c := range candidates {
		if c.Kind != "prompt_section" {
			regularCandidates = append(regularCandidates, c)
		}
	}
	scored := ScoreShards(regularCandidates)

	// Greedy pack: add highest-score shards until budget exhausted
	shardsDropped := 0

	for _, ss := range scored {
		if remaining <= 0 {
			shardsDropped++
			continue
		}
		if ss.EstimatedTokens > remaining {
			shardsDropped++
			continue
		}

		meta := map[string]any{
			"score":            ss.Score,
			"estimated_tokens": ss.EstimatedTokens,
		}
		if ss.Shard.Symbol != "" {
			meta["symbol"] = ss.Shard.Symbol
		}
		if ss.Shard.Path != "" {
			meta["path"] = ss.Shard.Path
		}

		packed = append(packed, PackedShard{
			Kind:    ss.Shard.Kind,
			BlobID:  ss.Shard.BlobID,
			Content: json.RawMessage(ss.Shard.Content),
			Meta:    meta,
		})

		shardTokensTotal += ss.EstimatedTokens
		remaining -= ss.EstimatedTokens
	}

	return &MissionPack{
		Header:       header,
		Mission:      mission,
		InlineShards: packed,
		PFEndpoint:   pc.PFEndpoint,
		PromptRef:    promptRef,
		BudgetMeta: BudgetMeta{
			TokenBudget:    budget,
			HeaderTokens:   headerTokens,
			MissionTokens:  missionTokens,
			ShardTokens:    shardTokensTotal,
			TotalTokens:    headerTokens + missionTokens + shardTokensTotal,
			ShardsIncluded: len(packed),
			ShardsDropped:  shardsDropped,
		},
	}, nil
}
