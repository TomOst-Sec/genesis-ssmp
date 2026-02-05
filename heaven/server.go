package heaven

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
)

// Server is the Heaven SSMP daemon.
type Server struct {
	blobs     *BlobStore
	events    *EventLog
	irIndex   *IRIndex
	pf        *PFRouter
	prompts   *PromptStore
	leases    *LeaseManager
	fileClock *FileClock
	dataDir   string
	stateRev  atomic.Int64
	mux       *http.ServeMux
}

// NewServer creates a Heaven server backed by the given data directory.
func NewServer(dataDir string) (*Server, error) {
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		return nil, err
	}
	events, err := NewEventLog(dataDir)
	if err != nil {
		return nil, err
	}
	irIndex, err := NewIRIndex(dataDir)
	if err != nil {
		return nil, err
	}
	leaseMgr, err := NewLeaseManager(events)
	if err != nil {
		return nil, err
	}
	fileClock, err := NewFileClock(events)
	if err != nil {
		return nil, err
	}

	// Reconstruct state_rev from existing event count.
	count, err := events.Len()
	if err != nil {
		return nil, fmt.Errorf("heaven server init: %w", err)
	}

	promptStore := NewPromptStore(blobs)
	pfRouter := NewPFRouter(blobs, irIndex, events, promptStore)

	s := &Server{
		blobs:     blobs,
		events:    events,
		irIndex:   irIndex,
		pf:        pfRouter,
		prompts:   promptStore,
		leases:    leaseMgr,
		fileClock: fileClock,
		dataDir:   dataDir,
		mux:       http.NewServeMux(),
	}
	s.stateRev.Store(int64(count))

	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("POST /blob", s.handlePutBlob)
	s.mux.HandleFunc("GET /blob/{id}", s.handleGetBlob)
	s.mux.HandleFunc("POST /event", s.handleAppendEvent)
	s.mux.HandleFunc("GET /events/tail", s.handleTailEvents)
	s.mux.HandleFunc("POST /ir/build", s.handleIRBuild)
	s.mux.HandleFunc("GET /ir/symdef", s.handleIRSymdef)
	s.mux.HandleFunc("GET /ir/callers", s.handleIRCallers)
	s.mux.HandleFunc("GET /ir/slice", s.handleIRSlice)
	s.mux.HandleFunc("GET /ir/search", s.handleIRSearch)
	s.mux.HandleFunc("POST /pf", s.handlePF)
	s.mux.HandleFunc("POST /lease/acquire", s.handleLeaseAcquire)
	s.mux.HandleFunc("POST /lease/release", s.handleLeaseRelease)
	s.mux.HandleFunc("GET /lease/list", s.handleLeaseList)
	s.mux.HandleFunc("POST /file-clock/get", s.handleFileClockGet)
	s.mux.HandleFunc("POST /file-clock/inc", s.handleFileClockInc)
	s.mux.HandleFunc("POST /validate-manifest", s.handleValidateManifest)
	s.mux.HandleFunc("POST /prompt/store", s.handlePromptStore)
	s.mux.HandleFunc("GET /prompt/{id}", s.handlePromptGet)
	s.mux.HandleFunc("GET /prompt/{id}/section/{index}", s.handlePromptSection)
	s.mux.HandleFunc("GET /prompt/{id}/reconstruct", s.handlePromptReconstruct)

	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the Heaven daemon on the given address.
// Pass "" to default to 127.0.0.1:4444.
func (s *Server) ListenAndServe(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:4444"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("heaven listen: %w", err)
	}
	return http.Serve(ln, s)
}

// StatusResponse is the JSON shape returned by GET /status.
type StatusResponse struct {
	StateRev          int64             `json:"state_rev"`
	ActiveLeasesCount int               `json:"active_leases_count"`
	HotsetSummary     map[string]string `json:"hotset_summary"`
	FileClockSummary  map[string]string `json:"file_clock_summary"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	fcSummary := make(map[string]string)
	for k, v := range s.fileClock.Summary() {
		fcSummary[k] = fmt.Sprintf("%d", v)
	}
	resp := StatusResponse{
		StateRev:          s.stateRev.Load(),
		ActiveLeasesCount: s.leases.ActiveCount(),
		HotsetSummary:     map[string]string{},
		FileClockSummary:  fcSummary,
	}
	writeJSON(w, http.StatusOK, resp)
}

// PutBlobResponse is the JSON shape returned by POST /blob.
type PutBlobResponse struct {
	BlobID string `json:"blob_id"`
}

func (s *Server) handlePutBlob(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: %v", err)
		return
	}
	if len(body) == 0 {
		writeErr(w, http.StatusBadRequest, "empty body")
		return
	}

	id, err := s.blobs.Put(body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}

	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, PutBlobResponse{BlobID: id})
}

// GetBlobResponse is the JSON shape returned by GET /blob/{id}.
type GetBlobResponse struct {
	BlobID  string `json:"blob_id"`
	Content string `json:"content"`
}

func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "missing blob id")
		return
	}

	data, err := s.blobs.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "blob not found: %s", id)
		return
	}
	writeJSON(w, http.StatusOK, GetBlobResponse{BlobID: id, Content: string(data)})
}

// AppendEventResponse is the JSON shape returned by POST /event.
type AppendEventResponse struct {
	Offset int64 `json:"offset"`
}

func (s *Server) handleAppendEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: %v", err)
		return
	}
	if !json.Valid(body) {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	offset, err := s.events.Append(json.RawMessage(body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}

	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, AppendEventResponse{Offset: offset})
}

// TailEventsResponse is the JSON shape returned by GET /events/tail.
type TailEventsResponse struct {
	Events []json.RawMessage `json:"events"`
}

func (s *Server) handleTailEvents(w http.ResponseWriter, r *http.Request) {
	nStr := r.URL.Query().Get("n")
	n := 10
	if nStr != "" {
		parsed, err := strconv.Atoi(nStr)
		if err != nil || parsed < 1 {
			writeErr(w, http.StatusBadRequest, "invalid n parameter")
			return
		}
		n = parsed
	}

	events, err := s.events.Tail(n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if events == nil {
		events = []json.RawMessage{}
	}

	writeJSON(w, http.StatusOK, TailEventsResponse{Events: events})
}

// --- Lease endpoints ---

func (s *Server) handleLeaseAcquire(w http.ResponseWriter, r *http.Request) {
	var req AcquireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	if req.OwnerID == "" || req.MissionID == "" || len(req.Scopes) == 0 {
		writeErr(w, http.StatusBadRequest, "owner_id, mission_id, and scopes are required")
		return
	}

	result, err := s.leases.Acquire(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, result)
}

// LeaseReleaseRequest is the JSON shape for POST /lease/release.
type LeaseReleaseRequest struct {
	LeaseIDs []string `json:"lease_ids"`
}

// LeaseReleaseResponse is the JSON shape returned by POST /lease/release.
type LeaseReleaseResponse struct {
	Released int `json:"released"`
}

func (s *Server) handleLeaseRelease(w http.ResponseWriter, r *http.Request) {
	var req LeaseReleaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}

	n, err := s.leases.Release(req.LeaseIDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, LeaseReleaseResponse{Released: n})
}

// LeaseListResponse is the JSON shape returned by GET /lease/list.
type LeaseListResponse struct {
	Leases []Lease `json:"leases"`
}

func (s *Server) handleLeaseList(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active_only") != "false"
	leases := s.leases.List(activeOnly)
	if leases == nil {
		leases = []Lease{}
	}
	writeJSON(w, http.StatusOK, LeaseListResponse{Leases: leases})
}

// --- File Clock endpoints ---

// FileClockGetRequest is the JSON shape for POST /file-clock/get.
type FileClockGetRequest struct {
	Paths []string `json:"paths"`
}

// FileClockGetResponse is the JSON shape returned by POST /file-clock/get.
type FileClockGetResponse struct {
	Clocks map[string]int64 `json:"clocks"`
}

func (s *Server) handleFileClockGet(w http.ResponseWriter, r *http.Request) {
	var req FileClockGetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	clocks := s.fileClock.Get(req.Paths)
	writeJSON(w, http.StatusOK, FileClockGetResponse{Clocks: clocks})
}

// FileClockIncRequest is the JSON shape for POST /file-clock/inc.
type FileClockIncRequest struct {
	Paths []string `json:"paths"`
}

// FileClockIncResponse is the JSON shape returned by POST /file-clock/inc.
type FileClockIncResponse struct {
	Clocks map[string]int64 `json:"clocks"`
}

func (s *Server) handleFileClockInc(w http.ResponseWriter, r *http.Request) {
	var req FileClockIncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	if len(req.Paths) == 0 {
		writeErr(w, http.StatusBadRequest, "paths required")
		return
	}
	if err := s.fileClock.Increment(req.Paths); err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	s.stateRev.Add(1)
	clocks := s.fileClock.Get(req.Paths)
	writeJSON(w, http.StatusOK, FileClockIncResponse{Clocks: clocks})
}

// --- Validate Manifest endpoint ---

// ValidateManifestRequest is the JSON shape for POST /validate-manifest.
type ValidateManifestRequest struct {
	OwnerID        string           `json:"owner_id"`
	MissionID      string           `json:"mission_id"`
	SymbolsTouched []string         `json:"symbols_touched"`
	FilesTouched   []string         `json:"files_touched"`
	ExpectedClocks map[string]int64 `json:"expected_clocks,omitempty"`
}

// ValidateManifestResponse is the JSON shape returned by POST /validate-manifest.
type ValidateManifestResponse struct {
	Allowed       bool     `json:"allowed"`
	Reason        string   `json:"reason,omitempty"`
	MissingLeases []string `json:"missing_leases,omitempty"`
	ClockDrift    []string `json:"clock_drift,omitempty"`
}

func (s *Server) handleValidateManifest(w http.ResponseWriter, r *http.Request) {
	var req ValidateManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	if req.OwnerID == "" {
		writeErr(w, http.StatusBadRequest, "owner_id is required")
		return
	}

	var missingLeases []string
	var clockDrift []string

	// Check symbol leases
	for _, sym := range req.SymbolsTouched {
		if !s.leases.OwnerHoldsScope(req.OwnerID, "symbol", sym) {
			missingLeases = append(missingLeases, "symbol:"+sym)
		}
	}

	// Check file leases
	for _, f := range req.FilesTouched {
		if !s.leases.OwnerHoldsScope(req.OwnerID, "file", f) {
			missingLeases = append(missingLeases, "file:"+f)
		}
	}

	// Check file clock drift
	if req.ExpectedClocks != nil {
		paths := make([]string, 0, len(req.ExpectedClocks))
		for p := range req.ExpectedClocks {
			paths = append(paths, p)
		}
		currentClocks := s.fileClock.Get(paths)
		for p, expected := range req.ExpectedClocks {
			if currentClocks[p] != expected {
				clockDrift = append(clockDrift, fmt.Sprintf("%s: expected=%d actual=%d", p, expected, currentClocks[p]))
			}
		}
	}

	if len(missingLeases) == 0 && len(clockDrift) == 0 {
		writeJSON(w, http.StatusOK, ValidateManifestResponse{Allowed: true})
		return
	}

	reason := ""
	if len(missingLeases) > 0 {
		reason = "missing leases"
	}
	if len(clockDrift) > 0 {
		if reason != "" {
			reason += "; "
		}
		reason += "file clock drift"
	}
	if missingLeases == nil {
		missingLeases = []string{}
	}
	if clockDrift == nil {
		clockDrift = []string{}
	}

	writeJSON(w, http.StatusOK, ValidateManifestResponse{
		Allowed:       false,
		Reason:        reason,
		MissingLeases: missingLeases,
		ClockDrift:    clockDrift,
	})
}

// --- Page Fault endpoint ---

func (s *Server) handlePF(w http.ResponseWriter, r *http.Request) {
	var req PFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid PF request: %v", err)
		return
	}

	resp, err := s.pf.Handle(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "%v", err)
		return
	}

	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, resp)
}

// --- IR Index endpoints ---

// IRBuildRequest is the JSON shape for POST /ir/build.
type IRBuildRequest struct {
	RepoPath string `json:"repo_path"`
}

// IRBuildResponse is the JSON shape returned by POST /ir/build.
type IRBuildResponse struct {
	FilesIndexed int        `json:"files_indexed"`
	Stats        IndexStats `json:"stats"`
}

func (s *Server) handleIRBuild(w http.ResponseWriter, r *http.Request) {
	var req IRBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request: %v", err)
		return
	}
	if req.RepoPath == "" {
		writeErr(w, http.StatusBadRequest, "repo_path is required")
		return
	}

	n, err := BuildIndex(context.Background(), s.irIndex, req.RepoPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "index build: %v", err)
		return
	}

	stats, err := s.irIndex.Stats()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "stats: %v", err)
		return
	}

	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, IRBuildResponse{FilesIndexed: n, Stats: stats})
}

// IRSymdefResponse is the JSON shape returned by GET /ir/symdef.
type IRSymdefResponse struct {
	Symbols []Symbol `json:"symbols"`
}

func (s *Server) handleIRSymdef(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name parameter is required")
		return
	}

	syms, err := s.irIndex.Symdef(name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if syms == nil {
		syms = []Symbol{}
	}
	writeJSON(w, http.StatusOK, IRSymdefResponse{Symbols: syms})
}

// IRCallersResponse is the JSON shape returned by GET /ir/callers.
type IRCallersResponse struct {
	Refs []Ref `json:"refs"`
}

func (s *Server) handleIRCallers(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name parameter is required")
		return
	}
	topK := 20
	if v := r.URL.Query().Get("top_k"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeErr(w, http.StatusBadRequest, "invalid top_k parameter")
			return
		}
		topK = parsed
	}

	refs, err := s.irIndex.Callers(name, topK)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if refs == nil {
		refs = []Ref{}
	}
	writeJSON(w, http.StatusOK, IRCallersResponse{Refs: refs})
}

// IRSliceResponse is the JSON shape returned by GET /ir/slice.
type IRSliceResponse struct {
	Content string `json:"content"`
}

func (s *Server) handleIRSlice(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "path parameter is required")
		return
	}
	startLine := 1
	if v := r.URL.Query().Get("start_line"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeErr(w, http.StatusBadRequest, "invalid start_line parameter")
			return
		}
		startLine = parsed
	}
	nLines := 20
	if v := r.URL.Query().Get("n"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeErr(w, http.StatusBadRequest, "invalid n parameter")
			return
		}
		nLines = parsed
	}

	content, err := Slice(path, startLine, nLines)
	if err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, IRSliceResponse{Content: content})
}

// IRSearchResponse is the JSON shape returned by GET /ir/search.
type IRSearchResponse struct {
	Symbols []Symbol `json:"symbols"`
}

func (s *Server) handleIRSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeErr(w, http.StatusBadRequest, "q parameter is required")
		return
	}
	topK := 20
	if v := r.URL.Query().Get("top_k"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 1 {
			writeErr(w, http.StatusBadRequest, "invalid top_k parameter")
			return
		}
		topK = parsed
	}

	syms, err := s.irIndex.Search(query, topK)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}
	if syms == nil {
		syms = []Symbol{}
	}
	writeJSON(w, http.StatusOK, IRSearchResponse{Symbols: syms})
}

// --- Prompt VM endpoints ---

func (s *Server) handlePromptStore(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: %v", err)
		return
	}
	if len(body) == 0 {
		writeErr(w, http.StatusBadRequest, "empty body")
		return
	}

	artifact, err := s.prompts.Store(body)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "%v", err)
		return
	}

	s.stateRev.Add(1)
	writeJSON(w, http.StatusOK, artifact)
}

func (s *Server) handlePromptGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "missing prompt id")
		return
	}

	artifact, err := s.prompts.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, artifact)
}

func (s *Server) handlePromptSection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	indexStr := r.PathValue("index")
	if id == "" || indexStr == "" {
		writeErr(w, http.StatusBadRequest, "missing prompt id or section index")
		return
	}

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid section index: %v", err)
		return
	}

	section, err := s.prompts.GetSection(id, index)
	if err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, section)
}

// PromptReconstructResponse is the JSON shape returned by GET /prompt/{id}/reconstruct.
type PromptReconstructResponse struct {
	PromptID string `json:"prompt_id"`
	Content  string `json:"content"`
}

func (s *Server) handlePromptReconstruct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "missing prompt id")
		return
	}

	data, err := s.prompts.Reconstruct(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "%v", err)
		return
	}
	writeJSON(w, http.StatusOK, PromptReconstructResponse{
		PromptID: id,
		Content:  string(data),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	msg := fmt.Sprintf(format, args...)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
