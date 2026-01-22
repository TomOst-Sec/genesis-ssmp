package testkit

import (
	"net/http/httptest"
	"testing"

	"github.com/genesis-ssmp/genesis/heaven"
)

// HeavenEnv holds a running Heaven test server and its components.
type HeavenEnv struct {
	Server  *httptest.Server
	DataDir string
}

// LaunchHeaven starts a real Heaven server in a temp directory and registers cleanup.
func LaunchHeaven(t *testing.T) *HeavenEnv {
	t.Helper()
	dataDir := t.TempDir()
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("testkit: heaven server init: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &HeavenEnv{
		Server:  ts,
		DataDir: dataDir,
	}
}

// URL returns the test server's URL.
func (h *HeavenEnv) URL() string {
	return h.Server.URL
}
