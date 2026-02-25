package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// mockUpstream returns a handler that simulates Anthropic API responses.
func mockUpstream(usage apiUsage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test_001",
			"type":  "message",
			"model": "claude-opus-4-6",
			"usage": map[string]int64{
				"input_tokens":                usage.InputTokens,
				"output_tokens":               usage.OutputTokens,
				"cache_creation_input_tokens":  usage.CacheCreationInputTokens,
				"cache_read_input_tokens":      usage.CacheReadInputTokens,
			},
			"content": []map[string]string{
				{"type": "text", "text": "Hello from mock upstream"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}

func TestHealthEndpoint(t *testing.T) {
	dir := t.TempDir()
	rec, err := NewRecorder(filepath.Join(dir, "test.jsonl"), "test-run", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	upstream := httptest.NewServer(mockUpstream(apiUsage{}))
	defer upstream.Close()

	proxy, err := NewProxyServer(upstream.URL, rec, false)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health: got %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Errorf("health body: got %q", string(body))
	}
}

func TestProxyForwardsRequest(t *testing.T) {
	usage := apiUsage{
		InputTokens:              1000,
		OutputTokens:             200,
		CacheCreationInputTokens: 500,
		CacheReadInputTokens:     300,
	}

	upstream := httptest.NewServer(mockUpstream(usage))
	defer upstream.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "test.jsonl")
	rec, err := NewRecorder(jsonlPath, "test-run", "claude")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := NewProxyServer(upstream.URL, rec, false)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	// Send a messages request
	reqBody := `{"model":"claude-opus-4-6","max_tokens":4096,"stream":false,"messages":[{"role":"user","content":"test"}]}`
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("messages: got %d, want 200", resp.StatusCode)
	}

	// Verify response is forwarded correctly
	var respJSON map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respJSON); err != nil {
		t.Fatal(err)
	}
	if respJSON["id"] != "msg_test_001" {
		t.Errorf("response id: got %v", respJSON["id"])
	}

	// Close recorder to flush
	rec.Close()

	// Read JSONL and verify record
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}

	var record TrafficRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &record); err != nil {
		t.Fatalf("parse JSONL record: %v (data: %s)", err, string(data))
	}

	if record.RunID != "test-run" {
		t.Errorf("run_id: got %q, want %q", record.RunID, "test-run")
	}
	if record.Tool != "claude" {
		t.Errorf("tool: got %q, want %q", record.Tool, "claude")
	}
	if record.InputTokens != 1000 {
		t.Errorf("input_tokens: got %d, want 1000", record.InputTokens)
	}
	if record.OutputTokens != 200 {
		t.Errorf("output_tokens: got %d, want 200", record.OutputTokens)
	}
	if record.CacheCreationInputTokens != 500 {
		t.Errorf("cache_creation_input_tokens: got %d, want 500", record.CacheCreationInputTokens)
	}
	if record.CacheReadInputTokens != 300 {
		t.Errorf("cache_read_input_tokens: got %d, want 300", record.CacheReadInputTokens)
	}
	if record.Model != "claude-opus-4-6" {
		t.Errorf("model: got %q", record.Model)
	}
	if record.Path != "/v1/messages" {
		t.Errorf("path: got %q", record.Path)
	}
	if record.LatencyMS < 0 {
		t.Errorf("latency: got %d, want >= 0", record.LatencyMS)
	}
	if record.CallSeq != 0 {
		t.Errorf("call_seq: got %d, want 0", record.CallSeq)
	}
}

func TestProxySSEExtraction(t *testing.T) {
	// Simulate SSE stream response
	sseBody := `event: message_start
data: {"type":"message_start","message":{"id":"msg_01","type":"message","model":"claude-opus-4-6","usage":{"input_tokens":2000,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":350}}

event: message_stop
data: {"type":"message_stop"}

`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseBody))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "sse.jsonl")
	rec, err := NewRecorder(jsonlPath, "sse-run", "genesis")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := NewProxyServer(upstream.URL, rec, false)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	reqBody := `{"model":"claude-opus-4-6","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"test"}]}`
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.Close()

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}

	var record TrafficRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &record); err != nil {
		t.Fatalf("parse JSONL: %v (data: %s)", err, string(data))
	}

	if record.InputTokens != 2000 {
		t.Errorf("SSE input_tokens: got %d, want 2000", record.InputTokens)
	}
	if record.OutputTokens != 350 {
		t.Errorf("SSE output_tokens: got %d, want 350", record.OutputTokens)
	}
	if record.CacheCreationInputTokens != 100 {
		t.Errorf("SSE cache_creation: got %d, want 100", record.CacheCreationInputTokens)
	}
	if record.CacheReadInputTokens != 50 {
		t.Errorf("SSE cache_read: got %d, want 50", record.CacheReadInputTokens)
	}
	if !record.Stream {
		t.Error("stream: got false, want true")
	}
}

func TestProxyMultipleCalls(t *testing.T) {
	callCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"id":   fmt.Sprintf("msg_%d", callCount),
			"type": "message",
			"usage": map[string]int64{
				"input_tokens":               int64(callCount * 100),
				"output_tokens":              int64(callCount * 50),
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "multi.jsonl")
	rec, err := NewRecorder(jsonlPath, "multi-run", "claude")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := NewProxyServer(upstream.URL, rec, false)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	// Make 3 requests
	for i := 0; i < 3; i++ {
		reqBody := fmt.Sprintf(`{"model":"claude-opus-4-6","max_tokens":4096,"messages":[{"role":"user","content":"call %d"}]}`, i)
		resp, err := http.Post(srv.URL+"/v1/messages", "application/json", bytes.NewBufferString(reqBody))
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	rec.Close()

	// Parse all JSONL records
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}

	for i, line := range lines {
		var rec TrafficRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("parse line %d: %v", i, err)
		}
		if rec.CallSeq != i {
			t.Errorf("line %d: call_seq got %d, want %d", i, rec.CallSeq, i)
		}
		wantIn := int64((i + 1) * 100)
		if rec.InputTokens != wantIn {
			t.Errorf("line %d: input_tokens got %d, want %d", i, rec.InputTokens, wantIn)
		}
	}
}

func TestNonMessagePathNotRecorded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "norecord.jsonl")
	rec, err := NewRecorder(jsonlPath, "test", "claude")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := NewProxyServer(upstream.URL, rec, false)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	// Call a non-messages endpoint
	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.Close()

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(data)) > 0 {
		t.Errorf("expected no records for /v1/models, got: %s", string(data))
	}
}

func TestRecorderSaveBodies(t *testing.T) {
	usage := apiUsage{InputTokens: 100, OutputTokens: 50}
	upstream := httptest.NewServer(mockUpstream(usage))
	defer upstream.Close()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "bodies.jsonl")
	rec, err := NewRecorder(jsonlPath, "body-run", "claude")
	if err != nil {
		t.Fatal(err)
	}

	proxy, err := NewProxyServer(upstream.URL, rec, true) // saveBodies = true
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(proxy.Handler())
	defer srv.Close()

	reqBody := `{"model":"claude-opus-4-6","messages":[{"role":"user","content":"save me"}]}`
	resp, err := http.Post(srv.URL+"/v1/messages", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()
	rec.Close()

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}

	var record TrafficRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &record); err != nil {
		t.Fatal(err)
	}

	if record.RequestBodyGz == "" {
		t.Error("expected non-empty req_gz with save-bodies enabled")
	}
	if record.ResponseBodyGz == "" {
		t.Error("expected non-empty resp_gz with save-bodies enabled")
	}
}
