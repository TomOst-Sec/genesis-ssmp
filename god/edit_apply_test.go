package god

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sampleFileContent is a 10-line Go file for testing.
const sampleFileContent = `package main

import "fmt"

func Greet(name string) string {
	return "Hello, " + name
}

func main() {
	fmt.Println(Greet("World"))
}
`

// anchorFor computes the anchor hash for a span in sampleFileContent.
func anchorFor(t *testing.T, content string, startLine, endLine int) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	return ComputeAnchorHash(lines, startLine, endLine)
}

// ---------------------------------------------------------------------------
// ComputeAnchorHash tests
// ---------------------------------------------------------------------------

func TestComputeAnchorHash(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}

	// Span lines 4-6 (1-indexed): context is lines 1-3 before + lines 7-9 after
	h1 := ComputeAnchorHash(lines, 4, 6)
	if h1 == "" {
		t.Fatal("anchor hash should not be empty")
	}
	if len(h1) != 64 {
		t.Errorf("anchor hash length = %d, want 64 (SHA256 hex)", len(h1))
	}

	// Same inputs produce same hash
	h2 := ComputeAnchorHash(lines, 4, 6)
	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}

	// Different span produces different hash
	h3 := ComputeAnchorHash(lines, 5, 7)
	if h1 == h3 {
		t.Error("different spans should produce different hashes")
	}
}

func TestComputeAnchorHashEdgeCases(t *testing.T) {
	lines := []string{"a", "b"}

	// Span at start of file (no lines before)
	h := ComputeAnchorHash(lines, 1, 1)
	if h == "" {
		t.Fatal("should handle span at file start")
	}

	// Span at end of file (no lines after)
	h2 := ComputeAnchorHash(lines, 2, 2)
	if h2 == "" {
		t.Fatal("should handle span at file end")
	}
}

// ---------------------------------------------------------------------------
// replace_span tests
// ---------------------------------------------------------------------------

func TestReplaceSpanWorks(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "main.go",
				AnchorHash: anchor,
				Lines:      []int{5, 7},
				Content:    "func Greet(p Person) string {\n\treturn \"Hello, \" + p.Name\n}",
			},
		},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d, want 1", result.OpsApplied)
	}
	if len(result.FilesModified) != 1 || result.FilesModified[0] != "main.go" {
		t.Errorf("FilesModified = %v", result.FilesModified)
	}

	content := readFile(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(content, "func Greet(p Person) string") {
		t.Error("replacement content not found in file")
	}
	if strings.Contains(content, "func Greet(name string) string") {
		t.Error("original content should have been replaced")
	}
}

func TestReplaceSpanAnchorMismatch(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "main.go",
				AnchorHash: "0000000000000000000000000000000000000000000000000000000000000000",
				Lines:      []int{5, 7},
				Content:    "replaced",
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected anchor mismatch error")
	}
	if !strings.Contains(err.Error(), "anchor mismatch") {
		t.Errorf("error should mention anchor mismatch, got: %v", err)
	}

	// File should be unchanged
	content := readFile(t, filepath.Join(dir, "main.go"))
	if content != sampleFileContent {
		t.Error("file should not have been modified on anchor mismatch")
	}
}

func TestReplaceSpanInvalidLineRange(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	tests := []struct {
		name  string
		lines []int
	}{
		{"end before start", []int{7, 5}},
		{"zero start", []int{0, 5}},
		{"too few elements", []int{5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ir := &EditIR{
				Ops: []EditOp{{Op: "replace_span", Path: "main.go", AnchorHash: "x", Lines: tc.lines}},
			}
			_, err := ApplyEditIR(dir, ir)
			if err == nil {
				t.Fatal("expected error for invalid line range")
			}
		})
	}
}

func TestReplaceSpanEndExceedsFile(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{
			{Op: "replace_span", Path: "main.go", AnchorHash: "x", Lines: []int{1, 999}, Content: "x"},
		},
	}
	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected error for line exceeding file")
	}
	if !strings.Contains(err.Error(), "exceeds file length") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// delete_span tests
// ---------------------------------------------------------------------------

func TestDeleteSpanWorks(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "delete_span",
				Path:       "main.go",
				AnchorHash: anchor,
				Lines:      []int{5, 7},
			},
		},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d", result.OpsApplied)
	}

	content := readFile(t, filepath.Join(dir, "main.go"))
	if strings.Contains(content, "func Greet") {
		t.Error("deleted span should be removed")
	}
	// Other content should remain
	if !strings.Contains(content, "func main()") {
		t.Error("non-deleted content should remain")
	}
}

func TestDeleteSpanAnchorMismatch(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "delete_span",
				Path:       "main.go",
				AnchorHash: "bad_hash",
				Lines:      []int{5, 7},
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected anchor mismatch error")
	}
}

// ---------------------------------------------------------------------------
// add_file tests
// ---------------------------------------------------------------------------

func TestAddFileWorks(t *testing.T) {
	dir := t.TempDir()

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "add_file",
				Path:       "pkg/util.go",
				AnchorHash: "empty",
				Content:    "package pkg\n\nfunc Helper() {}\n",
			},
		},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d", result.OpsApplied)
	}
	if len(result.FilesCreated) != 1 || result.FilesCreated[0] != "pkg/util.go" {
		t.Errorf("FilesCreated = %v", result.FilesCreated)
	}

	content := readFile(t, filepath.Join(dir, "pkg", "util.go"))
	if !strings.Contains(content, "func Helper()") {
		t.Error("file should contain the expected content")
	}
}

func TestAddFileCreatesDirectories(t *testing.T) {
	dir := t.TempDir()

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "add_file",
				Path:       "deep/nested/dir/file.go",
				AnchorHash: "empty",
				Content:    "package dir\n",
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "deep", "nested", "dir", "file.go")); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// insert_after_symbol tests
// ---------------------------------------------------------------------------

func TestInsertAfterSymbolWorks(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	// Compute anchor for the line containing "func Greet"
	lines := strings.Split(strings.TrimSuffix(sampleFileContent, "\n"), "\n")
	symbolLine := -1
	for i, l := range lines {
		if strings.Contains(l, "func Greet") {
			symbolLine = i + 1 // 1-indexed
			break
		}
	}
	if symbolLine < 0 {
		t.Fatal("could not find Greet symbol line")
	}

	anchor := ComputeAnchorHash(lines, symbolLine, symbolLine)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "insert_after_symbol",
				Path:       "main.go",
				AnchorHash: anchor,
				Symbol:     "func Greet",
				Content:    "// Greet says hello to a person",
			},
		},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.OpsApplied != 1 {
		t.Errorf("OpsApplied = %d", result.OpsApplied)
	}

	content := readFile(t, filepath.Join(dir, "main.go"))
	if !strings.Contains(content, "// Greet says hello to a person") {
		t.Error("inserted content not found")
	}

	// The inserted line should come after the Greet function declaration
	idx1 := strings.Index(content, "func Greet(name string)")
	idx2 := strings.Index(content, "// Greet says hello to a person")
	if idx2 < idx1 {
		t.Error("inserted content should appear after the symbol")
	}
}

func TestInsertAfterSymbolNotFound(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "insert_after_symbol",
				Path:       "main.go",
				AnchorHash: "x",
				Symbol:     "func NonExistent",
				Content:    "// should not appear",
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected error for missing symbol")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestInsertAfterSymbolAnchorMismatch(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "insert_after_symbol",
				Path:       "main.go",
				AnchorHash: "bad_anchor",
				Symbol:     "func Greet",
				Content:    "// nope",
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected anchor mismatch error")
	}
	if !strings.Contains(err.Error(), "anchor mismatch") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Unknown op
// ---------------------------------------------------------------------------

func TestUnknownOp(t *testing.T) {
	dir := t.TempDir()
	ir := &EditIR{
		Ops: []EditOp{
			{Op: "unknown_op", Path: "x.go", AnchorHash: "x"},
		},
	}
	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected error for unknown op")
	}
	if !strings.Contains(err.Error(), "unknown op") {
		t.Errorf("error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Multiple ops
// ---------------------------------------------------------------------------

func TestMultipleOpsApply(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	anchor := anchorFor(t, sampleFileContent, 5, 7)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "main.go",
				AnchorHash: anchor,
				Lines:      []int{5, 7},
				Content:    "func Greet(p Person) string {\n\treturn \"Hello, \" + p.Name\n}",
			},
			{
				Op:         "add_file",
				Path:       "types.go",
				AnchorHash: "empty",
				Content:    "package main\n\ntype Person struct {\n\tName string\n}\n",
			},
		},
	}

	result, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.OpsApplied != 2 {
		t.Errorf("OpsApplied = %d, want 2", result.OpsApplied)
	}
	if len(result.FilesModified) != 1 {
		t.Errorf("FilesModified = %v", result.FilesModified)
	}
	if len(result.FilesCreated) != 1 {
		t.Errorf("FilesCreated = %v", result.FilesCreated)
	}
}

// ---------------------------------------------------------------------------
// File not found
// ---------------------------------------------------------------------------

func TestReplaceSpanFileNotFound(t *testing.T) {
	dir := t.TempDir()
	ir := &EditIR{
		Ops: []EditOp{
			{Op: "replace_span", Path: "missing.go", AnchorHash: "x", Lines: []int{1, 1}, Content: "x"},
		},
	}
	_, err := ApplyEditIR(dir, ir)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// Diff renderer tests
// ---------------------------------------------------------------------------

func TestGenerateDiffSimpleReplace(t *testing.T) {
	oldContent := "line1\nline2\nline3\nline4\nline5\n"
	newContent := "line1\nline2\nNEW LINE\nline4\nline5\n"

	diff := GenerateDiff("test.go", oldContent, newContent)

	if !strings.Contains(diff, "--- a/test.go") {
		t.Error("diff should contain old file header")
	}
	if !strings.Contains(diff, "+++ b/test.go") {
		t.Error("diff should contain new file header")
	}
	if !strings.Contains(diff, "-line3") {
		t.Error("diff should contain removed line")
	}
	if !strings.Contains(diff, "+NEW LINE") {
		t.Error("diff should contain added line")
	}
	if !strings.Contains(diff, "@@") {
		t.Error("diff should contain hunk header")
	}
}

func TestGenerateDiffNoChanges(t *testing.T) {
	content := "line1\nline2\nline3\n"
	diff := GenerateDiff("test.go", content, content)
	if diff != "" {
		t.Errorf("identical content should produce empty diff, got:\n%s", diff)
	}
}

func TestGenerateDiffAddLines(t *testing.T) {
	oldContent := "line1\nline2\n"
	newContent := "line1\nline2\nline3\nline4\n"

	diff := GenerateDiff("test.go", oldContent, newContent)

	if !strings.Contains(diff, "+line3") {
		t.Error("diff should contain added line3")
	}
	if !strings.Contains(diff, "+line4") {
		t.Error("diff should contain added line4")
	}
}

func TestGenerateDiffDeleteLines(t *testing.T) {
	oldContent := "line1\nline2\nline3\nline4\n"
	newContent := "line1\nline4\n"

	diff := GenerateDiff("test.go", oldContent, newContent)

	if !strings.Contains(diff, "-line2") {
		t.Error("diff should contain removed line2")
	}
	if !strings.Contains(diff, "-line3") {
		t.Error("diff should contain removed line3")
	}
}

func TestGenerateDiffContextLines(t *testing.T) {
	// Build a file with enough lines that context is bounded to 3
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, "line"+strings.Repeat("x", i))
	}
	oldContent := strings.Join(lines, "\n") + "\n"

	// Change line 10 (0-indexed: 9)
	newLines := make([]string, len(lines))
	copy(newLines, lines)
	newLines[9] = "CHANGED"
	newContent := strings.Join(newLines, "\n") + "\n"

	diff := GenerateDiff("test.go", oldContent, newContent)

	// Should have context lines but not the entire file
	diffLines := strings.Split(diff, "\n")
	contextCount := 0
	for _, l := range diffLines {
		if strings.HasPrefix(l, " ") {
			contextCount++
		}
	}
	// Max 3 before + 3 after = 6 context lines
	if contextCount > 6 {
		t.Errorf("too many context lines: %d (max 6)", contextCount)
	}
}

func TestGenerateDiffEmptyOld(t *testing.T) {
	diff := GenerateDiff("new.go", "", "line1\nline2\n")
	if !strings.Contains(diff, "+line1") {
		t.Error("diff should show all lines as added")
	}
}

func TestGenerateDiffEmptyNew(t *testing.T) {
	diff := GenerateDiff("old.go", "line1\nline2\n", "")
	if !strings.Contains(diff, "-line1") {
		t.Error("diff should show all lines as removed")
	}
}

// ---------------------------------------------------------------------------
// Integration: apply + diff
// ---------------------------------------------------------------------------

func TestApplyAndDiff(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "main.go", sampleFileContent)

	oldContent := sampleFileContent
	anchor := anchorFor(t, sampleFileContent, 5, 7)

	ir := &EditIR{
		Ops: []EditOp{
			{
				Op:         "replace_span",
				Path:       "main.go",
				AnchorHash: anchor,
				Lines:      []int{5, 7},
				Content:    "func Greet(p Person) string {\n\treturn \"Hello, \" + p.Name\n}",
			},
		},
	}

	_, err := ApplyEditIR(dir, ir)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	newContent := readFile(t, filepath.Join(dir, "main.go"))
	diff := GenerateDiff("main.go", oldContent, newContent)

	if diff == "" {
		t.Fatal("diff should not be empty after apply")
	}
	if !strings.Contains(diff, "-func Greet(name string)") {
		t.Error("diff should show old function signature removed")
	}
	if !strings.Contains(diff, "+func Greet(p Person)") {
		t.Error("diff should show new function signature added")
	}
}
