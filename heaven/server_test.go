package heaven

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func TestStatusEndpoint(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want 200", resp.StatusCode)
	}

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.StateRev < 0 {
		t.Fatalf("state_rev = %d, want >= 0", status.StateRev)
	}
	if status.HotsetSummary == nil {
		t.Fatal("hotset_summary is nil")
	}
	if status.FileClockSummary == nil {
		t.Fatal("file_clock_summary is nil")
	}
}

func TestBlobEndpoints(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	// PUT blob
	putResp, err := http.Post(ts.URL+"/blob", "text/plain", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("POST /blob: %v", err)
	}
	defer putResp.Body.Close()

	if putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		t.Fatalf("POST /blob status = %d, body = %s", putResp.StatusCode, body)
	}

	var putResult PutBlobResponse
	if err := json.NewDecoder(putResp.Body).Decode(&putResult); err != nil {
		t.Fatalf("decode put response: %v", err)
	}
	if putResult.BlobID == "" {
		t.Fatal("blob_id is empty")
	}

	// GET blob
	getResp, err := http.Get(ts.URL + "/blob/" + putResult.BlobID)
	if err != nil {
		t.Fatalf("GET /blob: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /blob status = %d", getResp.StatusCode)
	}

	var getResult GetBlobResponse
	if err := json.NewDecoder(getResp.Body).Decode(&getResult); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResult.Content != "hello world" {
		t.Fatalf("content = %q, want %q", getResult.Content, "hello world")
	}
}

func TestBlobNotFoundEndpoint(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/blob/deadbeef")
	if err != nil {
		t.Fatalf("GET /blob: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEventEndpoints(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Append events
	for _, ev := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		resp, err := http.Post(ts.URL+"/event", "application/json", strings.NewReader(ev))
		if err != nil {
			t.Fatalf("POST /event: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /event status = %d", resp.StatusCode)
		}
	}

	// Tail events
	resp, err := http.Get(ts.URL + "/events/tail?n=2")
	if err != nil {
		t.Fatalf("GET /events/tail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events/tail status = %d", resp.StatusCode)
	}

	var tailResult TailEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tailResult); err != nil {
		t.Fatalf("decode tail response: %v", err)
	}
	if len(tailResult.Events) != 2 {
		t.Fatalf("tail returned %d events, want 2", len(tailResult.Events))
	}
}

func TestAppendInvalidJSON(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/event", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST /event: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestStateRevIncrementsOnWrites(t *testing.T) {
	s := newTestServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()

	getStateRev := func() int64 {
		resp, err := http.Get(ts.URL + "/status")
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		defer resp.Body.Close()
		var status StatusResponse
		json.NewDecoder(resp.Body).Decode(&status)
		return status.StateRev
	}

	rev0 := getStateRev()

	// Put a blob
	http.Post(ts.URL+"/blob", "text/plain", strings.NewReader("data"))
	rev1 := getStateRev()
	if rev1 <= rev0 {
		t.Fatalf("state_rev did not increment after put_blob: %d -> %d", rev0, rev1)
	}

	// Append an event
	http.Post(ts.URL+"/event", "application/json", strings.NewReader(`{"x":1}`))
	rev2 := getStateRev()
	if rev2 <= rev1 {
		t.Fatalf("state_rev did not increment after append_event: %d -> %d", rev1, rev2)
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// First server instance: write data
	s1, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(1): %v", err)
	}
	ts1 := httptest.NewServer(s1)

	http.Post(ts1.URL+"/blob", "text/plain", strings.NewReader("persist me"))
	http.Post(ts1.URL+"/event", "application/json", strings.NewReader(`{"persisted":true}`))

	// Capture blob ID
	resp, err := http.Post(ts1.URL+"/blob", "text/plain", strings.NewReader("persist me"))
	if err != nil {
		t.Fatalf("POST /blob: %v", err)
	}
	var putResp PutBlobResponse
	json.NewDecoder(resp.Body).Decode(&putResp)
	resp.Body.Close()
	ts1.Close()

	// Second server instance: verify data survived
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(2): %v", err)
	}
	ts2 := httptest.NewServer(s2)
	defer ts2.Close()

	// Blob should exist
	getResp, err := http.Get(ts2.URL + "/blob/" + putResp.BlobID)
	if err != nil {
		t.Fatalf("GET /blob: %v", err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("blob not found after restart")
	}
	getResp.Body.Close()

	// Events should exist
	tailResp, err := http.Get(ts2.URL + "/events/tail?n=10")
	if err != nil {
		t.Fatalf("GET /events/tail: %v", err)
	}
	var tailResult TailEventsResponse
	json.NewDecoder(tailResp.Body).Decode(&tailResult)
	tailResp.Body.Close()
	if len(tailResult.Events) < 1 {
		t.Fatal("events lost after restart")
	}

	// state_rev should be reconstructed from events
	statusResp, err := http.Get(ts2.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	var status StatusResponse
	json.NewDecoder(statusResp.Body).Decode(&status)
	statusResp.Body.Close()
	if status.StateRev < 1 {
		t.Fatalf("state_rev not reconstructed: %d", status.StateRev)
	}
}
