package testkit

import (
	"fmt"
	"sync"
)

// TestRandSource produces deterministic sequential hex IDs for testing.
type TestRandSource struct {
	mu      sync.Mutex
	counter int
	prefix  string
}

// NewTestRandSource creates a TestRandSource with the given prefix.
func NewTestRandSource(prefix string) *TestRandSource {
	return &TestRandSource{prefix: prefix}
}

// GenID returns the next deterministic ID: "<prefix>-0000000000000001", etc.
func (r *TestRandSource) GenID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter++
	return fmt.Sprintf("%s-%016x", r.prefix, r.counter)
}

// Reset resets the counter to 0.
func (r *TestRandSource) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter = 0
}

// Counter returns the current counter value.
func (r *TestRandSource) Counter() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counter
}
