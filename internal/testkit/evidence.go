package testkit

import (
	"sync"
	"testing"
)

// EvidenceRecorder captures tagged payloads for test assertion.
type EvidenceRecorder struct {
	mu      sync.Mutex
	records map[string][]any
}

// NewEvidenceRecorder creates an empty EvidenceRecorder.
func NewEvidenceRecorder() *EvidenceRecorder {
	return &EvidenceRecorder{
		records: make(map[string][]any),
	}
}

// Record stores a payload under the given tag.
func (er *EvidenceRecorder) Record(tag string, data any) {
	er.mu.Lock()
	defer er.mu.Unlock()
	er.records[tag] = append(er.records[tag], data)
}

// ByTag returns all payloads recorded under a tag.
func (er *EvidenceRecorder) ByTag(tag string) []any {
	er.mu.Lock()
	defer er.mu.Unlock()
	cp := make([]any, len(er.records[tag]))
	copy(cp, er.records[tag])
	return cp
}

// AssertCount asserts that exactly n payloads were recorded for the tag.
func (er *EvidenceRecorder) AssertCount(t *testing.T, tag string, n int) {
	t.Helper()
	er.mu.Lock()
	got := len(er.records[tag])
	er.mu.Unlock()
	if got != n {
		t.Errorf("evidence[%q]: got %d records, want %d", tag, got, n)
	}
}
