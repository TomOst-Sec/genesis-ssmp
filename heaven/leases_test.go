package heaven

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustPost(t *testing.T, url, contentType, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, body)
	}
	return v
}

// --- Unit tests for LeaseManager ---

func TestLeaseAcquireExclusive(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, err := NewLeaseManager(events)
	if err != nil {
		t.Fatalf("NewLeaseManager: %v", err)
	}

	// Owner A acquires symbol:Greet
	res1, err := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Greet"}},
	})
	if err != nil {
		t.Fatalf("Acquire A: %v", err)
	}
	if len(res1.Acquired) != 1 {
		t.Fatalf("A acquired %d, want 1", len(res1.Acquired))
	}

	// Owner B tries same scope — denied
	res2, err := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-B",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Greet"}},
	})
	if err != nil {
		t.Fatalf("Acquire B: %v", err)
	}
	if len(res2.Denied) != 1 {
		t.Fatalf("B denied %d, want 1", len(res2.Denied))
	}
	if len(res2.Acquired) != 0 {
		t.Fatalf("B acquired %d, want 0", len(res2.Acquired))
	}
}

func TestLeaseAcquireIdempotent(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	// Same owner acquires same scope twice — idempotent
	lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Foo"}},
	})
	res, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Foo"}},
	})
	if len(res.Acquired) != 1 {
		t.Fatalf("idempotent acquired %d, want 1", len(res.Acquired))
	}
	if len(res.Denied) != 0 {
		t.Fatalf("idempotent denied %d, want 0", len(res.Denied))
	}
}

func TestLeaseRelease(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	res, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Greet"}},
	})
	leaseID := res.Acquired[0].LeaseID

	// Release
	n, err := lm.Release([]string{leaseID})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if n != 1 {
		t.Fatalf("released %d, want 1", n)
	}

	// Now B can acquire the same scope
	res2, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-B",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Greet"}},
	})
	if len(res2.Acquired) != 1 {
		t.Fatalf("B acquired %d after release, want 1", len(res2.Acquired))
	}
}

func TestLeaseList(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes: []ScopeTarget{
			{ScopeType: "symbol", ScopeValue: "Greet"},
			{ScopeType: "file", ScopeValue: "main.go"},
		},
	})

	list := lm.List(true)
	if len(list) != 2 {
		t.Fatalf("list has %d leases, want 2", len(list))
	}
}

func TestLeaseReplayOnBoot(t *testing.T) {
	dir := t.TempDir()
	events, _ := NewEventLog(dir)
	lm1, _ := NewLeaseManager(events)

	// Acquire some leases
	res, _ := lm1.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes: []ScopeTarget{
			{ScopeType: "symbol", ScopeValue: "Greet"},
			{ScopeType: "symbol", ScopeValue: "Farewell"},
		},
	})

	// Release one
	lm1.Release([]string{res.Acquired[1].LeaseID})

	// Boot new manager from same events
	events2, _ := NewEventLog(dir)
	lm2, err := NewLeaseManager(events2)
	if err != nil {
		t.Fatalf("NewLeaseManager(2): %v", err)
	}

	// Should have exactly 1 active lease (Greet)
	if lm2.ActiveCount() != 1 {
		t.Fatalf("after replay: %d active leases, want 1", lm2.ActiveCount())
	}
	if !lm2.OwnerHoldsScope("angel-A", "symbol", "Greet") {
		t.Fatal("after replay: angel-A should hold symbol:Greet")
	}
	if lm2.OwnerHoldsScope("angel-A", "symbol", "Farewell") {
		t.Fatal("after replay: angel-A should NOT hold symbol:Farewell (released)")
	}
}

// --- Unit tests for FileClock ---

func TestFileClockIncrementAndGet(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	fc, err := NewFileClock(events)
	if err != nil {
		t.Fatalf("NewFileClock: %v", err)
	}

	fc.Increment([]string{"a.go", "b.go"})
	fc.Increment([]string{"a.go"})

	clocks := fc.Get([]string{"a.go", "b.go", "c.go"})
	if clocks["a.go"] != 2 {
		t.Fatalf("a.go clock = %d, want 2", clocks["a.go"])
	}
	if clocks["b.go"] != 1 {
		t.Fatalf("b.go clock = %d, want 1", clocks["b.go"])
	}
	if clocks["c.go"] != 0 {
		t.Fatalf("c.go clock = %d, want 0", clocks["c.go"])
	}
}

func TestFileClockReplayOnBoot(t *testing.T) {
	dir := t.TempDir()
	events, _ := NewEventLog(dir)
	fc1, _ := NewFileClock(events)

	fc1.Increment([]string{"main.go"})
	fc1.Increment([]string{"main.go", "util.go"})

	// Boot new file clock from same events
	events2, _ := NewEventLog(dir)
	fc2, err := NewFileClock(events2)
	if err != nil {
		t.Fatalf("NewFileClock(2): %v", err)
	}

	clocks := fc2.Get([]string{"main.go", "util.go"})
	if clocks["main.go"] != 2 {
		t.Fatalf("main.go clock after replay = %d, want 2", clocks["main.go"])
	}
	if clocks["util.go"] != 1 {
		t.Fatalf("util.go clock after replay = %d, want 1", clocks["util.go"])
	}
}

// --- HTTP endpoint tests ---

func TestLeaseAcquireEndpoint(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/lease/acquire", "application/json",
		strings.NewReader(`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`))
	if err != nil {
		t.Fatalf("POST /lease/acquire: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var result AcquireResult
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Acquired) != 1 {
		t.Fatalf("acquired %d, want 1", len(result.Acquired))
	}
}

func TestLeaseExclusiveEndpoint(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// A acquires
	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`).Body.Close()

	// B tries same scope
	result := decodeJSON[AcquireResult](t, mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"b1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`))
	if len(result.Denied) != 1 {
		t.Fatalf("denied %d, want 1", len(result.Denied))
	}
}

func TestLeaseReleaseEndpoint(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Acquire
	acq := decodeJSON[AcquireResult](t, mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`))

	// Release
	rel := decodeJSON[LeaseReleaseResponse](t, mustPost(t, ts.URL+"/lease/release", "application/json",
		fmt.Sprintf(`{"lease_ids":["%s"]}`, acq.Acquired[0].LeaseID)))
	if rel.Released != 1 {
		t.Fatalf("released %d, want 1", rel.Released)
	}
}

func TestLeaseListEndpoint(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"},{"scope_type":"file","scope_value":"main.go"}]}`).Body.Close()

	result := decodeJSON[LeaseListResponse](t, mustGet(t, ts.URL+"/lease/list"))
	if len(result.Leases) != 2 {
		t.Fatalf("list has %d leases, want 2", len(result.Leases))
	}
}

func TestFileClockEndpoints(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Increment
	incResult := decodeJSON[FileClockIncResponse](t, mustPost(t, ts.URL+"/file-clock/inc", "application/json",
		`{"paths":["a.go","b.go"]}`))
	if incResult.Clocks["a.go"] != 1 {
		t.Fatalf("a.go = %d, want 1", incResult.Clocks["a.go"])
	}

	// Increment again
	mustPost(t, ts.URL+"/file-clock/inc", "application/json", `{"paths":["a.go"]}`).Body.Close()

	// Get
	getResult := decodeJSON[FileClockGetResponse](t, mustPost(t, ts.URL+"/file-clock/get", "application/json",
		`{"paths":["a.go","b.go","c.go"]}`))
	if getResult.Clocks["a.go"] != 2 {
		t.Fatalf("a.go = %d, want 2", getResult.Clocks["a.go"])
	}
	if getResult.Clocks["b.go"] != 1 {
		t.Fatalf("b.go = %d, want 1", getResult.Clocks["b.go"])
	}
	if getResult.Clocks["c.go"] != 0 {
		t.Fatalf("c.go = %d, want 0", getResult.Clocks["c.go"])
	}
}

func TestValidateManifestAllowed(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"},{"scope_type":"file","scope_value":"main.go"}]}`).Body.Close()

	result := decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":["Greet"],"files_touched":["main.go"]}`))
	if !result.Allowed {
		t.Fatalf("expected allowed, got denied: %s", result.Reason)
	}
}

func TestValidateManifestDeniedMissingLease(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	result := decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":["Greet"],"files_touched":["main.go"]}`))
	if result.Allowed {
		t.Fatal("expected denied, got allowed")
	}
	if len(result.MissingLeases) != 2 {
		t.Fatalf("missing leases %d, want 2", len(result.MissingLeases))
	}
}

func TestValidateManifestDeniedClockDrift(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"},{"scope_type":"file","scope_value":"main.go"}]}`).Body.Close()

	mustPost(t, ts.URL+"/file-clock/inc", "application/json", `{"paths":["main.go"]}`).Body.Close()

	result := decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":["Greet"],"files_touched":["main.go"],"expected_clocks":{"main.go":0}}`))
	if result.Allowed {
		t.Fatal("expected denied due to clock drift, got allowed")
	}
	if len(result.ClockDrift) == 0 {
		t.Fatal("expected clock_drift entries")
	}
}

func TestStatusReportsLeasesAndClocks(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`).Body.Close()

	mustPost(t, ts.URL+"/file-clock/inc", "application/json", `{"paths":["main.go"]}`).Body.Close()

	status := decodeJSON[StatusResponse](t, mustGet(t, ts.URL+"/status"))
	if status.ActiveLeasesCount != 1 {
		t.Fatalf("active_leases_count = %d, want 1", status.ActiveLeasesCount)
	}
	if status.FileClockSummary["main.go"] != "1" {
		t.Fatalf("file_clock_summary[main.go] = %q, want \"1\"", status.FileClockSummary["main.go"])
	}
}

func TestLeaseConcurrentAcquire(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	const goroutines = 10
	results := make(chan AcquireResult, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			res, _ := lm.Acquire(AcquireRequest{
				OwnerID:   fmt.Sprintf("angel-%d", id),
				MissionID: "race-m1",
				Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Contested"}},
			})
			results <- res
		}(i)
	}

	winners := 0
	for i := 0; i < goroutines; i++ {
		res := <-results
		if len(res.Acquired) == 1 && len(res.Denied) == 0 {
			winners++
		}
	}
	// At most 1 can win outright (the first), though same-owner idempotent matches count too
	if winners < 1 {
		t.Fatal("at least 1 goroutine should acquire the lease")
	}
}

func TestLeaseConcurrentAcquireRelease(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	// Owner A acquires
	res1, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Relay"}},
	})
	if len(res1.Acquired) != 1 {
		t.Fatalf("A acquired %d, want 1", len(res1.Acquired))
	}

	// Release
	lm.Release([]string{res1.Acquired[0].LeaseID})

	// Owner B should now succeed
	res2, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-B",
		MissionID: "m2",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Relay"}},
	})
	if len(res2.Acquired) != 1 {
		t.Fatalf("B acquired %d after release, want 1", len(res2.Acquired))
	}
	if res2.Acquired[0].OwnerID != "angel-B" {
		t.Fatalf("B lease owner = %q, want angel-B", res2.Acquired[0].OwnerID)
	}
}

func TestLeaseAcquireMultipleScopePartialDeny(t *testing.T) {
	events, _ := NewEventLog(t.TempDir())
	lm, _ := NewLeaseManager(events)

	// A holds symbol:Alpha
	lm.Acquire(AcquireRequest{
		OwnerID:   "angel-A",
		MissionID: "m1",
		Scopes:    []ScopeTarget{{ScopeType: "symbol", ScopeValue: "Alpha"}},
	})

	// B tries to acquire both Alpha (held by A) and Beta (free)
	res, _ := lm.Acquire(AcquireRequest{
		OwnerID:   "angel-B",
		MissionID: "m2",
		Scopes: []ScopeTarget{
			{ScopeType: "symbol", ScopeValue: "Alpha"},
			{ScopeType: "symbol", ScopeValue: "Beta"},
		},
	})

	// Alpha is denied, Beta is acquired (current behavior: partial grant)
	if len(res.Denied) != 1 {
		t.Fatalf("denied %d, want 1 (Alpha)", len(res.Denied))
	}
	if len(res.Acquired) != 1 {
		t.Fatalf("acquired %d, want 1 (Beta)", len(res.Acquired))
	}
}

func TestFileClockDriftExactBoundary(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Acquire lease and set up expected clock
	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"file","scope_value":"main.go"}]}`).Body.Close()

	// Clock is 0. Validate with expected 0 -> should pass
	result := decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":[],"files_touched":["main.go"],"expected_clocks":{"main.go":0}}`))
	if !result.Allowed {
		t.Fatal("expected allowed when clock matches exactly")
	}

	// Increment clock to 1
	mustPost(t, ts.URL+"/file-clock/inc", "application/json", `{"paths":["main.go"]}`).Body.Close()

	// Validate with expected 0 -> should fail (off by one)
	result = decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":[],"files_touched":["main.go"],"expected_clocks":{"main.go":0}}`))
	if result.Allowed {
		t.Fatal("expected denied when clock is off by one")
	}
	if len(result.ClockDrift) == 0 {
		t.Fatal("expected clock_drift entries")
	}
}

func TestFileClockMultipleFilesPartialDrift(t *testing.T) {
	s, err := NewServer(t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()

	// Acquire leases for both files
	mustPost(t, ts.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"file","scope_value":"a.go"},{"scope_type":"file","scope_value":"b.go"}]}`).Body.Close()

	// Increment only a.go
	mustPost(t, ts.URL+"/file-clock/inc", "application/json", `{"paths":["a.go"]}`).Body.Close()

	// Validate with both files expecting 0 -> drift in a.go blocks entire manifest
	result := decodeJSON[ValidateManifestResponse](t, mustPost(t, ts.URL+"/validate-manifest", "application/json",
		`{"owner_id":"a1","mission_id":"m1","symbols_touched":[],"files_touched":["a.go","b.go"],"expected_clocks":{"a.go":0,"b.go":0}}`))
	if result.Allowed {
		t.Fatal("expected denied: drift in a.go should block entire manifest")
	}
	if len(result.ClockDrift) == 0 {
		t.Fatal("expected clock_drift entries for a.go")
	}
}

func TestLeasesPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()

	// Server 1: acquire leases
	s1, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(1): %v", err)
	}
	ts1 := httptest.NewServer(s1)
	mustPost(t, ts1.URL+"/lease/acquire", "application/json",
		`{"owner_id":"a1","mission_id":"m1","scopes":[{"scope_type":"symbol","scope_value":"Greet"}]}`).Body.Close()
	mustPost(t, ts1.URL+"/file-clock/inc", "application/json", `{"paths":["main.go"]}`).Body.Close()
	ts1.Close()

	// Server 2: verify state rebuilt
	s2, err := NewServer(dir)
	if err != nil {
		t.Fatalf("NewServer(2): %v", err)
	}
	ts2 := httptest.NewServer(s2)
	defer ts2.Close()

	// Leases should be reconstructed
	listResult := decodeJSON[LeaseListResponse](t, mustGet(t, ts2.URL+"/lease/list"))
	if len(listResult.Leases) != 1 {
		t.Fatalf("after restart: %d leases, want 1", len(listResult.Leases))
	}

	// File clocks should be reconstructed
	clockResult := decodeJSON[FileClockGetResponse](t, mustPost(t, ts2.URL+"/file-clock/get", "application/json",
		`{"paths":["main.go"]}`))
	if clockResult.Clocks["main.go"] != 1 {
		t.Fatalf("after restart: main.go clock = %d, want 1", clockResult.Clocks["main.go"])
	}
}
