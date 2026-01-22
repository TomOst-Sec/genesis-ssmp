package testkit

import (
	"encoding/json"
	"fmt"
	"sync"
)

// MockAngelProvider is a scripted mock for the god.Provider interface.
// It returns per-mission-ID responses or falls back to sequential defaults.
type MockAngelProvider struct {
	mu        sync.Mutex
	overrides map[string][]byte // mission_id -> raw response
	defaults  [][]byte          // sequential fallback responses
	defaultN  int               // index into defaults
	err       error             // if set, all calls return this error
	calls     []MockProviderCall
}

// MockProviderCall records a single Send call.
type MockProviderCall struct {
	MissionID string
	PackJSON  []byte
}

// NewMockAngelProvider creates a MockAngelProvider.
func NewMockAngelProvider() *MockAngelProvider {
	return &MockAngelProvider{
		overrides: make(map[string][]byte),
	}
}

// OnMission sets a specific response for a given mission ID.
func (m *MockAngelProvider) OnMission(missionID string, response []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.overrides[missionID] = response
}

// Default adds a default response to the sequential queue.
func (m *MockAngelProvider) Default(response []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults = append(m.defaults, response)
}

// SetError causes all subsequent Send calls to return this error.
func (m *MockAngelProvider) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// Calls returns all recorded Send calls.
func (m *MockAngelProvider) Calls() []MockProviderCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]MockProviderCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// Send implements the provider interface pattern. It expects a struct with
// a Mission field containing a MissionID string field.
func (m *MockAngelProvider) Send(pack interface{ MissionID() string }) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	missionID := pack.MissionID()
	packJSON, _ := json.Marshal(pack)
	m.calls = append(m.calls, MockProviderCall{MissionID: missionID, PackJSON: packJSON})

	if resp, ok := m.overrides[missionID]; ok {
		return resp, nil
	}
	if m.defaultN < len(m.defaults) {
		resp := m.defaults[m.defaultN]
		m.defaultN++
		return resp, nil
	}
	return nil, fmt.Errorf("mock provider: no response configured for mission %s", missionID)
}
