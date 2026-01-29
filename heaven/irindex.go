package heaven

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// IRIndex is a SQLite-backed symbol index for code intelligence.
type IRIndex struct {
	db *sql.DB
}

// Symbol represents a symbol definition in the index.
type Symbol struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// Ref represents a reference to a symbol.
type Ref struct {
	ID        int64  `json:"id"`
	SymbolID  int64  `json:"symbol_id"`
	Path      string `json:"path"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	RefKind   string `json:"ref_kind"`
}

// IndexedFile tracks a file's fingerprint for incremental indexing.
type IndexedFile struct {
	Path  string `json:"path"`
	Hash  string `json:"hash"`
	Mtime int64  `json:"mtime"`
}

// NewIRIndex opens or creates an IR index at <dataDir>/ir.db.
func NewIRIndex(dataDir string) (*IRIndex, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("ir index init: %w", err)
	}

	dbPath := filepath.Join(dataDir, "ir.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("ir index open: %w", err)
	}

	idx := &IRIndex{db: db}
	if err := idx.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

func (idx *IRIndex) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		path  TEXT PRIMARY KEY,
		hash  TEXT NOT NULL,
		mtime INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS symbols (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		kind       TEXT NOT NULL,
		path       TEXT NOT NULL,
		start_byte INTEGER NOT NULL,
		end_byte   INTEGER NOT NULL,
		start_line INTEGER NOT NULL,
		end_line   INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS refs (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol_id  INTEGER NOT NULL,
		path       TEXT NOT NULL,
		start_byte INTEGER NOT NULL,
		end_byte   INTEGER NOT NULL,
		start_line INTEGER NOT NULL,
		end_line   INTEGER NOT NULL,
		ref_kind   TEXT NOT NULL,
		FOREIGN KEY (symbol_id) REFERENCES symbols(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
	CREATE INDEX IF NOT EXISTS idx_symbols_path ON symbols(path);
	CREATE INDEX IF NOT EXISTS idx_refs_symbol_id ON refs(symbol_id);
	CREATE INDEX IF NOT EXISTS idx_refs_path ON refs(path);
	`
	_, err := idx.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("ir index migrate: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (idx *IRIndex) Close() error {
	return idx.db.Close()
}

// FileNeedsReindex checks if a file needs reindexing based on hash/mtime.
func (idx *IRIndex) FileNeedsReindex(path string, mtime int64) (bool, error) {
	var storedHash string
	var storedMtime int64
	err := idx.db.QueryRow("SELECT hash, mtime FROM files WHERE path = ?", path).Scan(&storedHash, &storedMtime)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if storedMtime != mtime {
		return true, nil
	}
	return false, nil
}

// FileHash computes the SHA256 hash of a file's contents.
func FileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// BeginFileTx starts a transaction for reindexing a single file.
// It removes all existing symbols/refs for that file path.
func (idx *IRIndex) BeginFileTx(path string) (*sql.Tx, error) {
	tx, err := idx.db.Begin()
	if err != nil {
		return nil, err
	}
	// Delete old refs for symbols in this file.
	if _, err := tx.Exec("DELETE FROM refs WHERE symbol_id IN (SELECT id FROM symbols WHERE path = ?)", path); err != nil {
		tx.Rollback()
		return nil, err
	}
	// Delete old symbols for this file.
	if _, err := tx.Exec("DELETE FROM symbols WHERE path = ?", path); err != nil {
		tx.Rollback()
		return nil, err
	}
	// Delete old refs that point to this file (cross-file refs).
	if _, err := tx.Exec("DELETE FROM refs WHERE path = ?", path); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// InsertSymbol inserts a symbol within a transaction.
func (idx *IRIndex) InsertSymbol(tx *sql.Tx, s Symbol) (int64, error) {
	res, err := tx.Exec(
		"INSERT INTO symbols (name, kind, path, start_byte, end_byte, start_line, end_line) VALUES (?, ?, ?, ?, ?, ?, ?)",
		s.Name, s.Kind, s.Path, s.StartByte, s.EndByte, s.StartLine, s.EndLine,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertRef inserts a reference within a transaction.
func (idx *IRIndex) InsertRef(tx *sql.Tx, r Ref) error {
	_, err := tx.Exec(
		"INSERT INTO refs (symbol_id, path, start_byte, end_byte, start_line, end_line, ref_kind) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.SymbolID, r.Path, r.StartByte, r.EndByte, r.StartLine, r.EndLine, r.RefKind,
	)
	return err
}

// UpdateFileRecord updates the file fingerprint after successful indexing.
func (idx *IRIndex) UpdateFileRecord(tx *sql.Tx, f IndexedFile) error {
	_, err := tx.Exec(
		"INSERT OR REPLACE INTO files (path, hash, mtime) VALUES (?, ?, ?)",
		f.Path, f.Hash, f.Mtime,
	)
	return err
}

// Symdef returns the symbol definition(s) matching the given name.
// Exact match first; returns all matches.
func (idx *IRIndex) Symdef(name string) ([]Symbol, error) {
	rows, err := idx.db.Query(
		"SELECT id, name, kind, path, start_byte, end_byte, start_line, end_line FROM symbols WHERE name = ?",
		name,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var syms []Symbol
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Path, &s.StartByte, &s.EndByte, &s.StartLine, &s.EndLine); err != nil {
			return nil, err
		}
		syms = append(syms, s)
	}
	return syms, rows.Err()
}

// SymdefSlice represents a symbol definition at a requested depth.
type SymdefSlice struct {
	Symbol    Symbol `json:"symbol"`
	Content   string `json:"content"`
	Depth     string `json:"depth"`
	FullLines int    `json:"full_lines"`
}

// SymdefWithDepth returns symbol definitions with content extracted at the
// requested depth level: "signature" (sig + doc only), "summary" (sig + line count).
func (idx *IRIndex) SymdefWithDepth(name string, depth string) ([]SymdefSlice, error) {
	syms, err := idx.Symdef(name)
	if err != nil {
		return nil, err
	}

	var results []SymdefSlice
	for _, sym := range syms {
		fullLines := sym.EndLine - sym.StartLine + 1
		src, err := os.ReadFile(sym.Path)
		if err != nil {
			// If we can't read the file, return the symbol metadata with empty content
			results = append(results, SymdefSlice{
				Symbol:    sym,
				Depth:     depth,
				FullLines: fullLines,
			})
			continue
		}

		var content string
		switch depth {
		case "signature":
			content = extractSignature(src, sym)
		case "summary":
			content = extractSignature(src, sym) + "\n// ... body: " +
				fmt.Sprintf("%d", fullLines) + " lines"
		default:
			if sym.StartByte < len(src) && sym.EndByte <= len(src) {
				content = string(src[sym.StartByte:sym.EndByte])
			}
		}

		results = append(results, SymdefSlice{
			Symbol:    sym,
			Content:   content,
			Depth:     depth,
			FullLines: fullLines,
		})
	}
	return results, nil
}

// extractSignature extracts just the function/method signature from source.
// Scans from StartByte forward until finding the opening brace '{' (Go/TS)
// or ':' after ')' (Python). Returns the signature line(s).
func extractSignature(src []byte, sym Symbol) string {
	if sym.StartByte >= len(src) || sym.EndByte > len(src) {
		return ""
	}
	body := src[sym.StartByte:sym.EndByte]

	// Find opening brace
	for i, b := range body {
		if b == '{' {
			sig := strings.TrimRight(string(body[:i]), " \t\n")
			return sig
		}
	}

	// Python: find colon after closing paren
	parenDepth := 0
	for i, b := range body {
		if b == '(' {
			parenDepth++
		} else if b == ')' {
			parenDepth--
			if parenDepth == 0 {
				// Find the next colon
				for j := i + 1; j < len(body); j++ {
					if body[j] == ':' {
						return strings.TrimRight(string(body[:j+1]), " \t\n")
					}
				}
			}
		}
	}

	// Fallback: first line only
	lines := strings.SplitN(string(body), "\n", 2)
	return lines[0]
}

// Callers returns up to topK reference sites for the given symbol name.
func (idx *IRIndex) Callers(name string, topK int) ([]Ref, error) {
	rows, err := idx.db.Query(`
		SELECT r.id, r.symbol_id, r.path, r.start_byte, r.end_byte, r.start_line, r.end_line, r.ref_kind
		FROM refs r
		JOIN symbols s ON r.symbol_id = s.id
		WHERE s.name = ?
		LIMIT ?
	`, name, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []Ref
	for rows.Next() {
		var r Ref
		if err := rows.Scan(&r.ID, &r.SymbolID, &r.Path, &r.StartByte, &r.EndByte, &r.StartLine, &r.EndLine, &r.RefKind); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// Search performs a lexical search across symbol names. Returns up to topK results.
func (idx *IRIndex) Search(query string, topK int) ([]Symbol, error) {
	pattern := "%" + query + "%"
	rows, err := idx.db.Query(
		"SELECT id, name, kind, path, start_byte, end_byte, start_line, end_line FROM symbols WHERE name LIKE ? LIMIT ?",
		pattern, topK,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var syms []Symbol
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.Path, &s.StartByte, &s.EndByte, &s.StartLine, &s.EndLine); err != nil {
			return nil, err
		}
		syms = append(syms, s)
	}
	return syms, rows.Err()
}

// Slice reads n lines starting from startLine (1-indexed) from the given file.
func Slice(path string, startLine, nLines int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("slice: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if startLine < 1 {
		startLine = 1
	}
	start := startLine - 1
	if start >= len(lines) {
		return "", nil
	}
	end := start + nLines
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n"), nil
}

// Stats returns index statistics.
type IndexStats struct {
	Files   int `json:"files"`
	Symbols int `json:"symbols"`
	Refs    int `json:"refs"`
}

func (idx *IRIndex) Stats() (IndexStats, error) {
	var stats IndexStats
	err := idx.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&stats.Files)
	if err != nil {
		return stats, err
	}
	err = idx.db.QueryRow("SELECT COUNT(*) FROM symbols").Scan(&stats.Symbols)
	if err != nil {
		return stats, err
	}
	err = idx.db.QueryRow("SELECT COUNT(*) FROM refs").Scan(&stats.Refs)
	return stats, err
}
