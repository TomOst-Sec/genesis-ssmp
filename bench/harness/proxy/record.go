package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// TrafficRecord is a single intercepted API call.
type TrafficRecord struct {
	RunID          string `json:"run_id"`
	Tool           string `json:"tool"`
	CallSeq        int    `json:"call_seq"`
	TimestampStart string `json:"ts_start"`
	TimestampEnd   string `json:"ts_end"`
	LatencyMS      int64  `json:"latency_ms"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	StatusCode     int    `json:"status_code"`
	RequestBytes   int    `json:"request_bytes"`
	ResponseBytes  int    `json:"response_bytes"`

	// From API response usage block
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`

	// Request metadata
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Stream    bool   `json:"stream"`

	// Compressed bodies (gzip+base64, optional)
	RequestBodyGz  string `json:"req_gz,omitempty"`
	ResponseBodyGz string `json:"resp_gz,omitempty"`
}

// Recorder writes TrafficRecords to a JSONL file. Thread-safe.
type Recorder struct {
	mu      sync.Mutex
	file    *os.File
	enc     *json.Encoder
	callSeq int
	runID   string
	tool    string
}

// NewRecorder creates a new JSONL recorder.
func NewRecorder(path, runID, tool string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("recorder: open %s: %w", path, err)
	}
	return &Recorder{
		file:  f,
		enc:   json.NewEncoder(f),
		runID: runID,
		tool:  tool,
	}, nil
}

// NextSeq returns the next call sequence number.
func (r *Recorder) NextSeq() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := r.callSeq
	r.callSeq++
	return seq
}

// Record writes a single traffic record to the JSONL file.
func (r *Recorder) Record(rec TrafficRecord) error {
	rec.RunID = r.runID
	rec.Tool = r.tool
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enc.Encode(rec)
}

// Close closes the underlying file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.file.Close()
}
