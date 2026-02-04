package heaven

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Sectioner tests
// ---------------------------------------------------------------------------

func TestPromptSectionerHeadings(t *testing.T) {
	raw := []byte("# Overview\nProject description.\n## Constraints\nMust be fast.\n### API\nGET /foo\n")
	ps := NewPromptSectioner()
	sections := ps.Section(raw)

	if len(sections) < 3 {
		t.Fatalf("expected >= 3 sections, got %d", len(sections))
	}

	// Verify titles
	titles := make([]string, len(sections))
	for i, s := range sections {
		titles[i] = s.Title
	}
	if sections[0].Title != "Overview" {
		t.Errorf("section 0 title = %q, want 'Overview'", sections[0].Title)
	}
	if sections[1].Title != "Constraints" {
		t.Errorf("section 1 title = %q, want 'Constraints'", sections[1].Title)
	}
	if sections[2].Title != "API" {
		t.Errorf("section 2 title = %q, want 'API'", sections[2].Title)
	}

	// Verify byte preservation
	var joined strings.Builder
	for _, s := range sections {
		joined.WriteString(s.Content)
	}
	if joined.String() != string(raw) {
		t.Errorf("joined sections do not match raw:\ngot:  %q\nwant: %q", joined.String(), string(raw))
	}

	// Verify byte counts
	totalBytes := 0
	for _, s := range sections {
		totalBytes += s.ByteLen
	}
	if totalBytes != len(raw) {
		t.Errorf("total ByteLen = %d, want %d", totalBytes, len(raw))
	}
}

func TestPromptSectionerCodeFences(t *testing.T) {
	raw := []byte("# Intro\nText.\n```\n# This is not a heading\ncode block\n```\n## Next\nMore text.\n")
	ps := NewPromptSectioner()
	sections := ps.Section(raw)

	// The "# This is not a heading" inside backticks should NOT create a split
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections (Intro + Next), got %d", len(sections))
	}
	if sections[0].Title != "Intro" {
		t.Errorf("section 0 title = %q, want 'Intro'", sections[0].Title)
	}
	if sections[1].Title != "Next" {
		t.Errorf("section 1 title = %q, want 'Next'", sections[1].Title)
	}

	// Verify code block is entirely in section 0
	if !strings.Contains(sections[0].Content, "# This is not a heading") {
		t.Error("code block heading should be in first section")
	}

	// Byte preservation
	var joined strings.Builder
	for _, s := range sections {
		joined.WriteString(s.Content)
	}
	if joined.String() != string(raw) {
		t.Error("joined sections do not match raw")
	}
}

func TestPromptSectionerSentinels(t *testing.T) {
	raw := []byte("# Project Overview\nBlah.\n## Constraints\nMust X.\n## API Contracts\nGET /foo.\n## Examples\nDemo.\n## Security\nAuth.\n")
	ps := NewPromptSectioner()
	sections := ps.Section(raw)

	expected := map[string]string{
		"Project Overview": "spec",
		"Constraints":      "constraints",
		"API Contracts":    "api",
		"Examples":         "examples",
		"Security":         "security",
	}

	for _, sec := range sections {
		want, ok := expected[sec.Title]
		if !ok {
			continue
		}
		if sec.SectionType != want {
			t.Errorf("section %q: type = %q, want %q", sec.Title, sec.SectionType, want)
		}
	}
}

func TestPromptSectionerLargeChunking(t *testing.T) {
	// Create a section larger than 4KB
	ps := NewPromptSectioner()
	ps.MaxSectionBytes = 100 // small for testing

	var buf strings.Builder
	buf.WriteString("# Big Section\n")
	for i := 0; i < 20; i++ {
		buf.WriteString(fmt.Sprintf("Paragraph %d with some text content.\n\n", i))
	}
	raw := []byte(buf.String())

	sections := ps.Section(raw)
	if len(sections) < 2 {
		t.Fatalf("expected section to be split, got %d sections", len(sections))
	}

	// All sections should be <= maxBytes (except possibly last if no good split point)
	for i, sec := range sections[:len(sections)-1] {
		if len(sec.Content) > ps.MaxSectionBytes+50 { // some tolerance for split point
			t.Errorf("section %d too large: %d bytes > %d", i, len(sec.Content), ps.MaxSectionBytes)
		}
	}

	// Byte preservation
	var joined strings.Builder
	for _, s := range sections {
		joined.WriteString(s.Content)
	}
	if joined.String() != string(raw) {
		t.Error("joined sections do not match raw after chunking")
	}
}

func TestPromptSectionerEmpty(t *testing.T) {
	ps := NewPromptSectioner()
	sections := ps.Section([]byte{})

	if len(sections) != 1 {
		t.Fatalf("empty input should return 1 section, got %d", len(sections))
	}
	if sections[0].Content != "" {
		t.Errorf("empty section content = %q", sections[0].Content)
	}
	if sections[0].ByteLen != 0 {
		t.Errorf("empty section ByteLen = %d", sections[0].ByteLen)
	}
}

func TestPromptSectionerNoHeadings(t *testing.T) {
	raw := []byte("This is just plain text without any markdown headings.\nAnother line.\n")
	ps := NewPromptSectioner()
	sections := ps.Section(raw)

	if len(sections) != 1 {
		t.Fatalf("plain text should return 1 section, got %d", len(sections))
	}
	if sections[0].SectionType != "other" {
		t.Errorf("section type = %q, want 'other'", sections[0].SectionType)
	}
	if sections[0].Content != string(raw) {
		t.Error("content should match raw input")
	}
}

// ---------------------------------------------------------------------------
// PromptStore tests
// ---------------------------------------------------------------------------

func TestPromptReconstructionEquality(t *testing.T) {
	dataDir := t.TempDir()
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPromptStore(blobs)

	raw := []byte("# Spec\nProject overview.\n## Constraints\nMust be fast.\n## API\nGET /endpoint\n")
	artifact, err := store.Store(raw)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	reconstructed, err := store.Reconstruct(artifact.PromptID)
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}

	if !bytes.Equal(reconstructed, raw) {
		t.Errorf("reconstructed bytes differ from raw:\ngot:  %q\nwant: %q", reconstructed, raw)
	}
}

func TestPromptStoreRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPromptStore(blobs)

	raw := []byte("# Overview\nA prompt.\n## Details\nMore info.\n")
	stored, err := store.Store(raw)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := store.Get(stored.PromptID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.PromptID != stored.PromptID {
		t.Errorf("prompt_id mismatch: %s vs %s", got.PromptID, stored.PromptID)
	}
	if got.TotalBytes != len(raw) {
		t.Errorf("total_bytes = %d, want %d", got.TotalBytes, len(raw))
	}
	if got.SectionCount != len(got.Sections) {
		t.Errorf("section_count = %d, sections len = %d", got.SectionCount, len(got.Sections))
	}
}

func TestPromptStoreDeduplicate(t *testing.T) {
	dataDir := t.TempDir()
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPromptStore(blobs)

	raw := []byte("# Test\nSame prompt stored twice.\n")
	a1, err := store.Store(raw)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := store.Store(raw)
	if err != nil {
		t.Fatal(err)
	}

	if a1.PromptID != a2.PromptID {
		t.Errorf("duplicate should return same prompt_id: %s vs %s", a1.PromptID, a2.PromptID)
	}
}

func TestPromptDeltaComputation(t *testing.T) {
	dataDir := t.TempDir()
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPromptStore(blobs)

	parent := []byte("# Spec\nOriginal spec.\n## API\nGET /foo\n")
	parentArtifact, err := store.Store(parent)
	if err != nil {
		t.Fatalf("store parent: %v", err)
	}

	// Child: change the API section
	child := []byte("# Spec\nOriginal spec.\n## API\nGET /bar\n")
	childArtifact, err := store.StoreDelta(child, parentArtifact.PromptID)
	if err != nil {
		t.Fatalf("store delta: %v", err)
	}

	if childArtifact.ParentPromptID != parentArtifact.PromptID {
		t.Errorf("parent_prompt_id = %q, want %q", childArtifact.ParentPromptID, parentArtifact.PromptID)
	}
	if len(childArtifact.Deltas) == 0 {
		t.Fatal("expected deltas for changed prompt")
	}

	// At least one delta should be a "replace" for the changed API section
	hasReplace := false
	for _, d := range childArtifact.Deltas {
		if d.Op == "replace" {
			hasReplace = true
		}
	}
	if !hasReplace {
		t.Error("expected at least one 'replace' delta")
	}
}

func TestPromptSearchSections(t *testing.T) {
	dataDir := t.TempDir()
	blobs, err := NewBlobStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPromptStore(blobs)

	raw := []byte("# Overview\nProject uses REST API.\n## Security\nUse OAuth2 tokens.\n## Glossary\nREST = Representational State Transfer.\n")
	artifact, err := store.Store(raw)
	if err != nil {
		t.Fatal(err)
	}

	// Search for "OAuth"
	matches, err := store.SearchSections(artifact.PromptID, "OAuth")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'OAuth', got %d", len(matches))
	}
	if matches[0].Title != "Security" {
		t.Errorf("match title = %q, want 'Security'", matches[0].Title)
	}

	// Search for "REST" (case-insensitive) — should match Overview and Glossary
	matches, err = store.SearchSections(artifact.PromptID, "rest")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected >= 2 matches for 'rest', got %d", len(matches))
	}
}

// ---------------------------------------------------------------------------
// HTTP endpoint test
// ---------------------------------------------------------------------------

func TestPromptStoreHTTPEndpoints(t *testing.T) {
	dataDir := t.TempDir()
	repoDir := setupFixtureRepo(t)

	s, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	BuildIndex(context.Background(), s.irIndex, repoDir)
	ts := httptest.NewServer(s)
	defer ts.Close()

	raw := "# Spec\nBuild a calculator.\n## Constraints\nMust handle division by zero.\n## API\nGET /calc/{op}\n"

	// POST /prompt/store
	resp, err := http.Post(ts.URL+"/prompt/store", "text/plain", strings.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /prompt/store: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /prompt/store status = %d", resp.StatusCode)
	}

	var artifact PromptArtifact
	if err := json.NewDecoder(resp.Body).Decode(&artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	if artifact.PromptID == "" {
		t.Fatal("prompt_id should not be empty")
	}
	if artifact.SectionCount < 3 {
		t.Errorf("section_count = %d, want >= 3", artifact.SectionCount)
	}

	// GET /prompt/{id}
	resp2, err := http.Get(ts.URL + "/prompt/" + artifact.PromptID)
	if err != nil {
		t.Fatalf("GET /prompt/{id}: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET /prompt/{id} status = %d", resp2.StatusCode)
	}
	var got PromptArtifact
	json.NewDecoder(resp2.Body).Decode(&got)
	if got.PromptID != artifact.PromptID {
		t.Errorf("GET returned different prompt_id")
	}

	// GET /prompt/{id}/section/0
	resp3, err := http.Get(ts.URL + "/prompt/" + artifact.PromptID + "/section/0")
	if err != nil {
		t.Fatalf("GET section: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("GET section status = %d", resp3.StatusCode)
	}
	var sec PromptSection
	json.NewDecoder(resp3.Body).Decode(&sec)
	if sec.Index != 0 {
		t.Errorf("section index = %d, want 0", sec.Index)
	}

	// GET /prompt/{id}/reconstruct
	resp4, err := http.Get(ts.URL + "/prompt/" + artifact.PromptID + "/reconstruct")
	if err != nil {
		t.Fatalf("GET reconstruct: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("GET reconstruct status = %d", resp4.StatusCode)
	}
	var reconResp struct {
		PromptID string `json:"prompt_id"`
		Content  string `json:"content"`
	}
	json.NewDecoder(resp4.Body).Decode(&reconResp)
	if reconResp.Content != raw {
		t.Errorf("reconstruct content mismatch:\ngot:  %q\nwant: %q", reconResp.Content, raw)
	}
}
