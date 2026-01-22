package testkit

import (
	"os"
	"path/filepath"
	"testing"
)

// RepoFixture represents a temporary repository fixture for testing.
type RepoFixture struct {
	Dir   string
	Files map[string]string // relative path -> content
}

// NewRepoFixture creates a temp directory with the given files.
func NewRepoFixture(t *testing.T, files map[string]string) *RepoFixture {
	t.Helper()
	dir := t.TempDir()

	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("testkit: mkdir %s: %v", filepath.Dir(absPath), err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("testkit: write %s: %v", relPath, err)
		}
	}

	return &RepoFixture{Dir: dir, Files: files}
}

// Snapshot reads all files back from disk and returns their current contents.
func (rf *RepoFixture) Snapshot(t *testing.T) map[string]string {
	t.Helper()
	result := make(map[string]string, len(rf.Files))
	for relPath := range rf.Files {
		absPath := filepath.Join(rf.Dir, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			t.Fatalf("testkit: read snapshot %s: %v", relPath, err)
		}
		result[relPath] = string(data)
	}
	return result
}

// AddFile adds a file to the fixture directory at runtime.
func (rf *RepoFixture) AddFile(t *testing.T, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(rf.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("testkit: mkdir %s: %v", filepath.Dir(absPath), err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("testkit: write %s: %v", relPath, err)
	}
	rf.Files[relPath] = content
}
