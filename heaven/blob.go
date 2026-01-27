package heaven

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// BlobStore is a content-addressed blob store using SHA256 hashes.
// Blobs are stored at <root>/blobs/sha256/<hash>.
type BlobStore struct {
	root string
}

// NewBlobStore creates a BlobStore rooted at the given directory.
// It creates the directory structure if it doesn't exist.
func NewBlobStore(root string) (*BlobStore, error) {
	dir := filepath.Join(root, "blobs", "sha256")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("blob store init: %w", err)
	}
	return &BlobStore{root: root}, nil
}

// Put stores content and returns its SHA256 blob ID.
// If the blob already exists (dedupe), it returns the ID without rewriting.
func (bs *BlobStore) Put(content []byte) (string, error) {
	h := sha256.Sum256(content)
	id := hex.EncodeToString(h[:])
	path := bs.path(id)

	if _, err := os.Stat(path); err == nil {
		return id, nil // dedupe
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("blob put: %w", err)
	}
	return id, nil
}

// Get retrieves a blob by its SHA256 ID.
func (bs *BlobStore) Get(id string) ([]byte, error) {
	data, err := os.ReadFile(bs.path(id))
	if err != nil {
		return nil, fmt.Errorf("blob get %s: %w", id, err)
	}
	return data, nil
}

// Exists reports whether a blob with the given ID exists.
func (bs *BlobStore) Exists(id string) bool {
	_, err := os.Stat(bs.path(id))
	return err == nil
}

func (bs *BlobStore) path(id string) string {
	return filepath.Join(bs.root, "blobs", "sha256", id)
}
