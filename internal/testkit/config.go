package testkit

import (
	"net/http/httptest"
	"testing"

	"github.com/genesis-ssmp/genesis/heaven"
)

// TestConfig aggregates all test infrastructure for one-line setup.
type TestConfig struct {
	Clock    *TestClock
	Rand     *TestRandSource
	Heaven   *HeavenEnv
	Evidence *EvidenceRecorder
}

// NewTestConfig creates a fully wired TestConfig with a running Heaven server.
func NewTestConfig(t *testing.T) *TestConfig {
	t.Helper()
	return &TestConfig{
		Clock:    NewTestClock(),
		Rand:     NewTestRandSource("test"),
		Heaven:   LaunchHeaven(t),
		Evidence: NewEvidenceRecorder(),
	}
}

// NewTestConfigWithoutHeaven creates a TestConfig without starting Heaven.
func NewTestConfigWithoutHeaven(t *testing.T) *TestConfig {
	t.Helper()
	return &TestConfig{
		Clock:    NewTestClock(),
		Rand:     NewTestRandSource("test"),
		Evidence: NewEvidenceRecorder(),
	}
}

// Restart shuts down the current Heaven server and starts a new one using the
// same data directory. This tests persistence across restarts.
func (h *HeavenEnv) Restart(t *testing.T) {
	t.Helper()
	h.Server.Close()
	dataDir := h.DataDir
	srv, err := heaven.NewServer(dataDir)
	if err != nil {
		t.Fatalf("testkit: heaven restart: %v", err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	h.Server = ts
}
