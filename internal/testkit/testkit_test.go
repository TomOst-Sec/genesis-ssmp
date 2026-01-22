package testkit

import (
	"testing"
	"time"
)

func TestTestClockNow(t *testing.T) {
	c := NewTestClock()
	expected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := c.Now(); !got.Equal(expected) {
		t.Fatalf("Now() = %v, want %v", got, expected)
	}
}

func TestTestClockAdvance(t *testing.T) {
	c := NewTestClock()
	c.Advance(5 * time.Second)
	expected := time.Date(2025, 1, 1, 0, 0, 5, 0, time.UTC)
	if got := c.Now(); !got.Equal(expected) {
		t.Fatalf("Now() after Advance = %v, want %v", got, expected)
	}
}

func TestTestClockSet(t *testing.T) {
	c := NewTestClock()
	target := time.Date(2030, 6, 15, 12, 0, 0, 0, time.UTC)
	c.Set(target)
	if got := c.Now(); !got.Equal(target) {
		t.Fatalf("Now() after Set = %v, want %v", got, target)
	}
}

func TestTestRandSourceSequential(t *testing.T) {
	r := NewTestRandSource("test")
	id1 := r.GenID()
	id2 := r.GenID()
	if id1 == id2 {
		t.Fatalf("GenID should produce unique IDs: %s == %s", id1, id2)
	}
	if id1 != "test-0000000000000001" {
		t.Fatalf("first ID = %q, want test-0000000000000001", id1)
	}
	if id2 != "test-0000000000000002" {
		t.Fatalf("second ID = %q, want test-0000000000000002", id2)
	}
}

func TestTestRandSourceReset(t *testing.T) {
	r := NewTestRandSource("r")
	r.GenID()
	r.GenID()
	r.Reset()
	id := r.GenID()
	if id != "r-0000000000000001" {
		t.Fatalf("after Reset, GenID = %q, want r-0000000000000001", id)
	}
}

func TestRepoFixtureCreateAndSnapshot(t *testing.T) {
	files := map[string]string{
		"main.go":      "package main\n",
		"lib/util.go":  "package lib\n",
	}
	rf := NewRepoFixture(t, files)
	snap := rf.Snapshot(t)
	if snap["main.go"] != "package main\n" {
		t.Fatalf("main.go = %q", snap["main.go"])
	}
	if snap["lib/util.go"] != "package lib\n" {
		t.Fatalf("lib/util.go = %q", snap["lib/util.go"])
	}
}

func TestRepoFixtureAddFile(t *testing.T) {
	rf := NewRepoFixture(t, map[string]string{"a.txt": "hello"})
	rf.AddFile(t, "b.txt", "world")
	snap := rf.Snapshot(t)
	if snap["b.txt"] != "world" {
		t.Fatalf("b.txt = %q", snap["b.txt"])
	}
}

func TestEvidenceRecorder(t *testing.T) {
	er := NewEvidenceRecorder()
	er.Record("pf", "shard1")
	er.Record("pf", "shard2")
	er.Record("metric", 42)

	er.AssertCount(t, "pf", 2)
	er.AssertCount(t, "metric", 1)
	er.AssertCount(t, "missing", 0)

	pfs := er.ByTag("pf")
	if len(pfs) != 2 {
		t.Fatalf("ByTag(pf) = %d, want 2", len(pfs))
	}
}

func TestTokenCount(t *testing.T) {
	data := make([]byte, 400)
	tokens, bytes := TokenCount(data)
	if bytes != 400 {
		t.Fatalf("bytes = %d, want 400", bytes)
	}
	// 400/4 + 10 = 110
	if tokens != 110 {
		t.Fatalf("tokens = %d, want 110", tokens)
	}
}

func TestLaunchHeaven(t *testing.T) {
	env := LaunchHeaven(t)
	if env.URL() == "" {
		t.Fatal("LaunchHeaven returned empty URL")
	}
	if env.DataDir == "" {
		t.Fatal("LaunchHeaven returned empty DataDir")
	}
}
