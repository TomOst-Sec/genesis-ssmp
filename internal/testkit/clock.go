package testkit

import (
	"sync"
	"time"
)

// TestClock is an injectable clock for deterministic testing.
type TestClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewTestClock creates a TestClock set to 2025-01-01T00:00:00Z.
func NewTestClock() *TestClock {
	return &TestClock{
		now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// Now returns the current frozen time.
func (c *TestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *TestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the clock to an exact time.
func (c *TestClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
