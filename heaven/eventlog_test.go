package heaven

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestEventAppendAndTail(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	events := []string{
		`{"type":"init","v":1}`,
		`{"type":"blob_put","blob_id":"abc"}`,
		`{"type":"lease_grant","lease_id":"l1"}`,
	}
	for _, e := range events {
		if _, err := el.Append(json.RawMessage(e)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := el.Tail(2)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Tail(2) returned %d events, want 2", len(got))
	}
	if string(got[0]) != events[1] {
		t.Fatalf("Tail[0] = %s, want %s", got[0], events[1])
	}
	if string(got[1]) != events[2] {
		t.Fatalf("Tail[1] = %s, want %s", got[1], events[2])
	}
}

func TestEventTailMoreThanExists(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	el.Append(json.RawMessage(`{"x":1}`))

	got, err := el.Tail(100)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Tail(100) returned %d events, want 1", len(got))
	}
}

func TestEventTailEmpty(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	got, err := el.Tail(10)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got != nil {
		t.Fatalf("Tail on empty log returned %d events, want nil", len(got))
	}
}

func TestEventRejectInvalidJSON(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	_, err = el.Append(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestEventLogConcurrentAppend(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	const goroutines = 20
	const eventsPerGoroutine = 10
	done := make(chan bool, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			for i := 0; i < eventsPerGoroutine; i++ {
				evt := fmt.Sprintf(`{"goroutine":%d,"seq":%d}`, id, i)
				if _, err := el.Append(json.RawMessage(evt)); err != nil {
					t.Errorf("goroutine %d append %d: %v", id, i, err)
				}
			}
			done <- true
		}(g)
	}

	for i := 0; i < goroutines; i++ {
		<-done
	}

	// All 200 events should be present
	n, err := el.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	expected := goroutines * eventsPerGoroutine
	if n != expected {
		t.Fatalf("Len = %d, want %d", n, expected)
	}

	// Verify all events are valid JSON
	all, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for i, raw := range all {
		if !json.Valid(raw) {
			t.Fatalf("event %d is not valid JSON: %s", i, raw)
		}
	}
}

func TestEventLogOrderPreservation(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	// Append sequentially
	for i := 0; i < 10; i++ {
		evt := fmt.Sprintf(`{"seq":%d}`, i)
		el.Append(json.RawMessage(evt))
	}

	all, err := el.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("All() returned %d events, want 10", len(all))
	}

	// Verify order is preserved
	for i, raw := range all {
		var evt struct {
			Seq int `json:"seq"`
		}
		if err := json.Unmarshal(raw, &evt); err != nil {
			t.Fatalf("unmarshal event %d: %v", i, err)
		}
		if evt.Seq != i {
			t.Fatalf("event %d has seq=%d, want %d", i, evt.Seq, i)
		}
	}
}

func TestEventLen(t *testing.T) {
	el, err := NewEventLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewEventLog: %v", err)
	}

	for i := 0; i < 5; i++ {
		el.Append(json.RawMessage(`{"i":` + json.Number(string(rune('0'+i))).String() + `}`))
	}

	n, err := el.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 5 {
		t.Fatalf("Len = %d, want 5", n)
	}
}
