package heaven

import (
	"encoding/json"
	"fmt"
	"sync"
)

// FileClock tracks a monotonic version counter per file path.
// Incremented when a patch touches a file. Rebuilt from events on boot.
type FileClock struct {
	mu     sync.RWMutex
	clocks map[string]int64 // path -> clock value
	events *EventLog
}

// NewFileClock creates a FileClock and replays events to rebuild state.
func NewFileClock(events *EventLog) (*FileClock, error) {
	fc := &FileClock{
		clocks: make(map[string]int64),
		events: events,
	}
	if err := fc.replayEvents(); err != nil {
		return nil, fmt.Errorf("file clock init: %w", err)
	}
	return fc, nil
}

func (fc *FileClock) replayEvents() error {
	all, err := fc.events.All()
	if err != nil {
		return err
	}
	for _, raw := range all {
		var evt struct {
			Type  string   `json:"type"`
			Paths []string `json:"paths"`
		}
		if json.Unmarshal(raw, &evt) != nil {
			continue
		}
		if evt.Type == "file_clock_inc" {
			for _, p := range evt.Paths {
				fc.clocks[p]++
			}
		}
	}
	return nil
}

// Increment bumps the clock for each path and logs the event.
func (fc *FileClock) Increment(paths []string) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	for _, p := range paths {
		fc.clocks[p]++
	}

	evt, _ := json.Marshal(map[string]any{
		"type":  "file_clock_inc",
		"paths": paths,
	})
	_, err := fc.events.Append(json.RawMessage(evt))
	return err
}

// Get returns the current clock values for the requested paths.
func (fc *FileClock) Get(paths []string) map[string]int64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	result := make(map[string]int64, len(paths))
	for _, p := range paths {
		result[p] = fc.clocks[p] // 0 if not present
	}
	return result
}

// Summary returns a copy of all tracked file clocks.
func (fc *FileClock) Summary() map[string]int64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	result := make(map[string]int64, len(fc.clocks))
	for k, v := range fc.clocks {
		result[k] = v
	}
	return result
}
