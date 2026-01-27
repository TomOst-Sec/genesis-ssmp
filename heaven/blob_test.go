package heaven

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBlobRoundtrip(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	content := []byte("hello heaven")
	id, err := bs.Put(content)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if id == "" {
		t.Fatal("Put returned empty id")
	}

	got, err := bs.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("roundtrip mismatch: got %q, want %q", got, content)
	}
}

func TestBlobDedupe(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	content := []byte("duplicate me")
	id1, _ := bs.Put(content)
	id2, _ := bs.Put(content)
	if id1 != id2 {
		t.Fatalf("dedupe failed: %s != %s", id1, id2)
	}
}

func TestBlobNotFound(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	_, err = bs.Get("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for missing blob")
	}
}

func TestBlobDedupContentVerification(t *testing.T) {
	dir := t.TempDir()
	bs, err := NewBlobStore(dir)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	content := []byte("deduplicate this content")
	id1, _ := bs.Put(content)
	id2, _ := bs.Put(content)
	if id1 != id2 {
		t.Fatalf("dedupe IDs differ: %s != %s", id1, id2)
	}

	// Verify only one file on disk at blobs/sha256/
	entries, err := os.ReadDir(filepath.Join(dir, "blobs", "sha256"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if e.Name() == id1 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 file for blob %s, found %d", id1, count)
	}
}

func TestBlobLargeContent(t *testing.T) {
	bs, err := NewBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}

	// 1MB blob
	large := make([]byte, 1024*1024)
	for i := range large {
		large[i] = byte(i % 256)
	}

	id, err := bs.Put(large)
	if err != nil {
		t.Fatalf("Put 1MB blob: %v", err)
	}

	got, err := bs.Get(id)
	if err != nil {
		t.Fatalf("Get 1MB blob: %v", err)
	}
	if len(got) != len(large) {
		t.Fatalf("roundtrip size: got %d, want %d", len(got), len(large))
	}
	for i := range large {
		if got[i] != large[i] {
			t.Fatalf("byte mismatch at offset %d", i)
			break
		}
	}
}
