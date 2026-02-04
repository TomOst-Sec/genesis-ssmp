package heaven

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PromptArtifact is a content-addressed prompt stored in Heaven.
type PromptArtifact struct {
	PromptID       string          `json:"prompt_id"`                   // sha256 of raw bytes
	RawBlobID      string          `json:"raw_blob_id"`                 // BlobStore key
	Sections       []PromptSection `json:"sections"`
	TotalBytes     int             `json:"total_bytes"`
	TotalTokens    int             `json:"total_tokens"`
	SectionCount   int             `json:"section_count"`
	CreatedAt      string          `json:"created_at"`
	ParentPromptID string          `json:"parent_prompt_id,omitempty"`
	Deltas         []SectionDelta  `json:"deltas,omitempty"`
}

// SectionDelta describes a change between parent and child prompt sections.
type SectionDelta struct {
	Op           string `json:"op"`    // "add", "remove", "replace"
	SectionIndex int    `json:"section_index"`
	OldSHA256    string `json:"old_sha256,omitempty"`
	NewSHA256    string `json:"new_sha256,omitempty"`
}

// PromptStore manages content-addressed prompt artifacts backed by a BlobStore.
type PromptStore struct {
	blobs   *BlobStore
	mu      sync.RWMutex
	prompts map[string]*PromptArtifact
}

// NewPromptStore creates a PromptStore backed by the given BlobStore.
func NewPromptStore(blobs *BlobStore) *PromptStore {
	return &PromptStore{
		blobs:   blobs,
		prompts: make(map[string]*PromptArtifact),
	}
}

// Store sections the raw prompt, stores the raw bytes in the BlobStore,
// and returns the PromptArtifact. Deduplicates by prompt_id.
func (ps *PromptStore) Store(raw []byte) (*PromptArtifact, error) {
	promptID := promptHash(raw)

	ps.mu.RLock()
	if existing, ok := ps.prompts[promptID]; ok {
		ps.mu.RUnlock()
		return existing, nil
	}
	ps.mu.RUnlock()

	// Store raw bytes in BlobStore
	rawBlobID, err := ps.blobs.Put(raw)
	if err != nil {
		return nil, fmt.Errorf("prompt store: put raw: %w", err)
	}

	// Section the prompt
	sectioner := NewPromptSectioner()
	sections := sectioner.Section(raw)

	// Estimate tokens: bytes/4 + 10 overhead per section
	totalTokens := 0
	for _, sec := range sections {
		totalTokens += len(sec.Content)/4 + 10
	}

	artifact := &PromptArtifact{
		PromptID:     promptID,
		RawBlobID:    rawBlobID,
		Sections:     sections,
		TotalBytes:   len(raw),
		TotalTokens:  totalTokens,
		SectionCount: len(sections),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	ps.mu.Lock()
	ps.prompts[promptID] = artifact
	ps.mu.Unlock()

	return artifact, nil
}

// Get returns the PromptArtifact for the given prompt_id.
func (ps *PromptStore) Get(promptID string) (*PromptArtifact, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	a, ok := ps.prompts[promptID]
	if !ok {
		return nil, fmt.Errorf("prompt not found: %s", promptID)
	}
	return a, nil
}

// GetSection returns a single section by index.
func (ps *PromptStore) GetSection(promptID string, index int) (*PromptSection, error) {
	a, err := ps.Get(promptID)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(a.Sections) {
		return nil, fmt.Errorf("section index %d out of bounds [0, %d)", index, len(a.Sections))
	}
	sec := a.Sections[index]
	return &sec, nil
}

// SearchSections returns sections whose content or title contains the query (case-insensitive).
func (ps *PromptStore) SearchSections(promptID string, query string) ([]PromptSection, error) {
	a, err := ps.Get(promptID)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(query)
	var matches []PromptSection
	for _, sec := range a.Sections {
		if strings.Contains(strings.ToLower(sec.Content), lower) ||
			strings.Contains(strings.ToLower(sec.Title), lower) {
			matches = append(matches, sec)
		}
	}
	return matches, nil
}

// Reconstruct returns the raw bytes of the prompt by fetching from BlobStore.
// Verifies sha256 matches prompt_id.
func (ps *PromptStore) Reconstruct(promptID string) ([]byte, error) {
	a, err := ps.Get(promptID)
	if err != nil {
		return nil, err
	}
	data, err := ps.blobs.Get(a.RawBlobID)
	if err != nil {
		return nil, fmt.Errorf("prompt reconstruct: %w", err)
	}
	// Verify hash
	actual := promptHash(data)
	if actual != promptID {
		return nil, fmt.Errorf("prompt reconstruct: hash mismatch: got %s, want %s", actual, promptID)
	}
	return data, nil
}

// StoreDelta stores a new prompt and computes deltas from a parent prompt.
func (ps *PromptStore) StoreDelta(raw []byte, parentID string) (*PromptArtifact, error) {
	parent, err := ps.Get(parentID)
	if err != nil {
		return nil, fmt.Errorf("prompt store delta: parent: %w", err)
	}

	artifact, err := ps.Store(raw)
	if err != nil {
		return nil, err
	}

	// Compute deltas by comparing section SHA256s
	deltas := computeDeltas(parent.Sections, artifact.Sections)
	artifact.ParentPromptID = parentID
	artifact.Deltas = deltas

	ps.mu.Lock()
	ps.prompts[artifact.PromptID] = artifact
	ps.mu.Unlock()

	return artifact, nil
}

// computeDeltas compares old and new sections by SHA256 to produce a delta list.
func computeDeltas(old, new []PromptSection) []SectionDelta {
	var deltas []SectionDelta

	maxLen := len(old)
	if len(new) > maxLen {
		maxLen = len(new)
	}

	for i := 0; i < maxLen; i++ {
		switch {
		case i >= len(old):
			// New section added
			deltas = append(deltas, SectionDelta{
				Op:           "add",
				SectionIndex: i,
				NewSHA256:    new[i].SHA256,
			})
		case i >= len(new):
			// Old section removed
			deltas = append(deltas, SectionDelta{
				Op:           "remove",
				SectionIndex: i,
				OldSHA256:    old[i].SHA256,
			})
		case old[i].SHA256 != new[i].SHA256:
			// Section replaced
			deltas = append(deltas, SectionDelta{
				Op:           "replace",
				SectionIndex: i,
				OldSHA256:    old[i].SHA256,
				NewSHA256:    new[i].SHA256,
			})
		}
	}

	return deltas
}

func promptHash(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
