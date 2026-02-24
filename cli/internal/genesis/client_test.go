package genesis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewClient("")
	c.BaseURL = server.URL
	return c
}

func TestStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode(HeavenStatus{
			StateRev: 42,
			Leases:   []LeaseInfo{{ID: "l1", OwnerID: "o1"}},
			Clocks:   map[string]int64{"main.go": 3},
		})
	})
	c := testClient(t, mux)

	status, err := c.Status()
	require.NoError(t, err)
	assert.Equal(t, int64(42), status.StateRev)
	assert.Len(t, status.Leases, 1)
	assert.Equal(t, int64(3), status.Clocks["main.go"])
}

func TestTailEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/events/tail", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.URL.Query().Get("n"))
		json.NewEncoder(w).Encode([]json.RawMessage{
			json.RawMessage(`{"type":"build"}`),
			json.RawMessage(`{"type":"test"}`),
		})
	})
	c := testClient(t, mux)

	events, err := c.TailEvents(5)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}

func TestIRBuild(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ir/build", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "/repo", body["repo_path"])
		json.NewEncoder(w).Encode(map[string]int{"symbols": 150})
	})
	c := testClient(t, mux)

	symbols, err := c.IRBuild("/repo")
	require.NoError(t, err)
	assert.Equal(t, 150, symbols)
}

func TestIRSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ir/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]SymbolResult{
			{Name: "Foo", Kind: "func", FilePath: "foo.go", Line: 10},
		})
	})
	c := testClient(t, mux)

	results, err := c.IRSearch("Foo", 5)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Foo", results[0].Name)
}

func TestIRSymdef(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ir/symdef", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]SymbolResult{
			{Name: "Bar", Kind: "type", FilePath: "bar.go", Line: 5},
		})
	})
	c := testClient(t, mux)

	results, err := c.IRSymdef("Bar")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "Bar", results[0].Name)
}

func TestIRCallers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ir/callers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]RefResult{
			{Caller: "main", FilePath: "main.go", Line: 20},
		})
	})
	c := testClient(t, mux)

	results, err := c.IRCallers("Foo", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "main", results[0].Caller)
}

func TestLeaseAcquire(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/lease/acquire", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(LeaseAcquireResult{
			LeaseID: "lease-123",
			Granted: true,
		})
	})
	c := testClient(t, mux)

	result, err := c.LeaseAcquire("owner1", "mission1", []Scope{{Path: "foo.go", Mode: "write"}})
	require.NoError(t, err)
	assert.True(t, result.Granted)
	assert.Equal(t, "lease-123", result.LeaseID)
}

func TestPutBlob(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/blob", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		json.NewEncoder(w).Encode(map[string]string{"hash": "abc123"})
	})
	c := testClient(t, mux)

	hash, err := c.PutBlob([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "abc123", hash)
}

func TestFileClockGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fileclock/get", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int64{"main.go": 5})
	})
	c := testClient(t, mux)

	clocks, err := c.FileClockGet([]string{"main.go"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), clocks["main.go"])
}

func TestFileClockInc(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fileclock/inc", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int64{"main.go": 6})
	})
	c := testClient(t, mux)

	clocks, err := c.FileClockInc([]string{"main.go"})
	require.NoError(t, err)
	assert.Equal(t, int64(6), clocks["main.go"])
}

func TestClientConcurrentRequests(t *testing.T) {
	var callCount atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(HeavenStatus{
			StateRev: callCount.Load(),
			Leases:   []LeaseInfo{},
			Clocks:   map[string]int64{},
		})
	})
	c := testClient(t, mux)

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Status()
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent request failed: %v", err)
	}

	assert.Equal(t, int64(goroutines), callCount.Load())
}

func TestClientRetryOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})
	c := testClient(t, mux)

	_, err := c.Status()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestStatusError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	c := testClient(t, mux)

	_, err := c.Status()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}
