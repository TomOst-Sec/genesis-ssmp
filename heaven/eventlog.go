package heaven

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EventLog is an append-only JSONL event log.
type EventLog struct {
	mu   sync.Mutex
	path string
}

// NewEventLog creates an EventLog at <root>/events.log.
func NewEventLog(root string) (*EventLog, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("event log init: %w", err)
	}
	return &EventLog{path: filepath.Join(root, "events.log")}, nil
}

// Append writes a JSON event to the log and returns the byte offset.
func (el *EventLog) Append(event json.RawMessage) (int64, error) {
	if !json.Valid(event) {
		return 0, fmt.Errorf("event log append: invalid JSON")
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	f, err := os.OpenFile(el.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("event log append: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("event log append: %w", err)
	}
	offset := info.Size()

	line := append(event, '\n')
	if _, err := f.Write(line); err != nil {
		return 0, fmt.Errorf("event log append: %w", err)
	}
	return offset, nil
}

// Tail returns the last n events from the log.
func (el *EventLog) Tail(n int) ([]json.RawMessage, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	f, err := os.Open(el.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("event log tail: %w", err)
	}
	defer f.Close()

	var all []json.RawMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		all = append(all, json.RawMessage(cp))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("event log tail: %w", err)
	}

	if n >= len(all) {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// All returns every event in the log, in order.
func (el *EventLog) All() ([]json.RawMessage, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	f, err := os.Open(el.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("event log all: %w", err)
	}
	defer f.Close()

	var all []json.RawMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		all = append(all, json.RawMessage(cp))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("event log all: %w", err)
	}
	return all, nil
}

// Len returns the total number of events in the log.
func (el *EventLog) Len() (int, error) {
	el.mu.Lock()
	defer el.mu.Unlock()

	f, err := os.Open(el.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("event log len: %w", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count, scanner.Err()
}
