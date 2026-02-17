package god

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyResult holds the outcome of applying an Edit IR to a repository.
type ApplyResult struct {
	FilesModified []string `json:"files_modified"`
	FilesCreated  []string `json:"files_created"`
	FilesDeleted  []string `json:"files_deleted"`
	OpsApplied    int      `json:"ops_applied"`
}

// ApplyEditIR applies an Edit IR to files on disk under repoRoot.
// Operations are applied in order. Each op that references an existing file
// verifies the anchor hash before modifying it.
func ApplyEditIR(repoRoot string, ir *EditIR) (*ApplyResult, error) {
	result := &ApplyResult{}

	for i, op := range ir.Ops {
		absPath := filepath.Join(repoRoot, op.Path)

		switch op.Op {
		case "replace_span":
			if err := applyReplaceSpan(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] replace_span: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "delete_span":
			if err := applyDeleteSpan(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] delete_span: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "insert_after_symbol":
			if err := applyInsertAfterSymbol(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] insert_after_symbol: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "add_file":
			if err := applyAddFile(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] add_file: %w", i, err)
			}
			result.FilesCreated = appendUnique(result.FilesCreated, op.Path)

		case "insert_before_symbol":
			if err := applyInsertBeforeSymbol(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] insert_before_symbol: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "delete_file":
			if err := applyDeleteFile(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] delete_file: %w", i, err)
			}
			result.FilesDeleted = appendUnique(result.FilesDeleted, op.Path)

		case "replace_line":
			if err := applyReplaceLine(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] replace_line: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "insert_lines":
			if err := applyInsertLines(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] insert_lines: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		case "template":
			if err := applyTemplate(absPath, op); err != nil {
				return nil, fmt.Errorf("ops[%d] template: %w", i, err)
			}
			result.FilesModified = appendUnique(result.FilesModified, op.Path)

		default:
			return nil, fmt.Errorf("ops[%d]: unknown op %q", i, op.Op)
		}

		result.OpsApplied++
	}

	return result, nil
}

// ComputeAnchorHash computes the anchor hash for a span in a file.
// It takes 3 lines before the span start and 3 lines after the span end,
// concatenates them, and returns the SHA256 hex digest.
// Lines are 1-indexed. startLine and endLine are inclusive.
func ComputeAnchorHash(lines []string, startLine, endLine int) string {
	var anchor strings.Builder

	// 3 lines before the span (indices startLine-4 to startLine-2, 0-indexed)
	for i := startLine - 4; i < startLine-1; i++ {
		if i >= 0 && i < len(lines) {
			anchor.WriteString(lines[i])
			anchor.WriteByte('\n')
		}
	}

	// 3 lines after the span (indices endLine to endLine+2, 0-indexed)
	for i := endLine; i < endLine+3; i++ {
		if i >= 0 && i < len(lines) {
			anchor.WriteString(lines[i])
			anchor.WriteByte('\n')
		}
	}

	h := sha256.Sum256([]byte(anchor.String()))
	return hex.EncodeToString(h[:])
}

func applyReplaceSpan(absPath string, op EditOp) error {
	if len(op.Lines) != 2 {
		return fmt.Errorf("lines must have exactly 2 elements [start, end]")
	}
	startLine, endLine := op.Lines[0], op.Lines[1]
	if startLine < 1 || endLine < startLine {
		return fmt.Errorf("invalid line range [%d, %d]", startLine, endLine)
	}

	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	if endLine > len(lines) {
		return fmt.Errorf("end line %d exceeds file length %d", endLine, len(lines))
	}

	// Verify anchor
	actual := ComputeAnchorHash(lines, startLine, endLine)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Replace: lines before span + new content + lines after span
	newContent := splitContent(op.Content)
	result := make([]string, 0, len(lines))
	result = append(result, lines[:startLine-1]...)
	result = append(result, newContent...)
	result = append(result, lines[endLine:]...)

	return writeFileLines(absPath, result)
}

func applyDeleteSpan(absPath string, op EditOp) error {
	if len(op.Lines) != 2 {
		return fmt.Errorf("lines must have exactly 2 elements [start, end]")
	}
	startLine, endLine := op.Lines[0], op.Lines[1]
	if startLine < 1 || endLine < startLine {
		return fmt.Errorf("invalid line range [%d, %d]", startLine, endLine)
	}

	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	if endLine > len(lines) {
		return fmt.Errorf("end line %d exceeds file length %d", endLine, len(lines))
	}

	// Verify anchor
	actual := ComputeAnchorHash(lines, startLine, endLine)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Delete: lines before span + lines after span
	result := make([]string, 0, len(lines))
	result = append(result, lines[:startLine-1]...)
	result = append(result, lines[endLine:]...)

	return writeFileLines(absPath, result)
}

func applyInsertAfterSymbol(absPath string, op EditOp) error {
	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	// Find the symbol line
	symbolLine := -1
	for i, line := range lines {
		if strings.Contains(line, op.Symbol) {
			symbolLine = i
			break
		}
	}
	if symbolLine < 0 {
		return fmt.Errorf("symbol %q not found in %s", op.Symbol, absPath)
	}

	// Verify anchor: use the symbol line as a single-line span
	anchorLineNum := symbolLine + 1 // 1-indexed
	actual := ComputeAnchorHash(lines, anchorLineNum, anchorLineNum)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Insert new content after the symbol line
	newContent := splitContent(op.Content)
	result := make([]string, 0, len(lines)+len(newContent))
	result = append(result, lines[:symbolLine+1]...)
	result = append(result, newContent...)
	result = append(result, lines[symbolLine+1:]...)

	return writeFileLines(absPath, result)
}

func applyAddFile(absPath string, op EditOp) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(absPath, []byte(op.Content), 0o644)
}

// readFileLines reads a file and splits it into lines.
func readFileLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)
	// Remove trailing newline to avoid empty final element
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n"), nil
}

// writeFileLines joins lines with newlines and writes to disk.
func writeFileLines(path string, lines []string) error {
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// splitContent splits replacement content into lines.
func splitContent(content string) []string {
	if content == "" {
		return nil
	}
	content = strings.TrimSuffix(content, "\n")
	return strings.Split(content, "\n")
}

func applyInsertBeforeSymbol(absPath string, op EditOp) error {
	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	// Find the symbol line
	symbolLine := -1
	for i, line := range lines {
		if strings.Contains(line, op.Symbol) {
			symbolLine = i
			break
		}
	}
	if symbolLine < 0 {
		return fmt.Errorf("symbol %q not found in %s", op.Symbol, absPath)
	}

	// Verify anchor: use the symbol line as a single-line span
	anchorLineNum := symbolLine + 1 // 1-indexed
	actual := ComputeAnchorHash(lines, anchorLineNum, anchorLineNum)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Insert new content before the symbol line
	newContent := splitContent(op.Content)
	result := make([]string, 0, len(lines)+len(newContent))
	result = append(result, lines[:symbolLine]...)
	result = append(result, newContent...)
	result = append(result, lines[symbolLine:]...)

	return writeFileLines(absPath, result)
}

func applyDeleteFile(absPath string, op EditOp) error {
	if err := os.Remove(absPath); err != nil {
		return fmt.Errorf("delete %s: %w", absPath, err)
	}
	return nil
}

func applyReplaceLine(absPath string, op EditOp) error {
	if len(op.Lines) != 1 {
		return fmt.Errorf("lines must have exactly 1 element [lineNum]")
	}
	lineNum := op.Lines[0]
	if lineNum < 1 {
		return fmt.Errorf("invalid line number %d", lineNum)
	}

	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	if lineNum > len(lines) {
		return fmt.Errorf("line %d exceeds file length %d", lineNum, len(lines))
	}

	// Verify anchor: treat as a single-line span
	actual := ComputeAnchorHash(lines, lineNum, lineNum)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Replace the single line with new content
	newContent := splitContent(op.Content)
	result := make([]string, 0, len(lines)+len(newContent)-1)
	result = append(result, lines[:lineNum-1]...)
	result = append(result, newContent...)
	result = append(result, lines[lineNum:]...)

	return writeFileLines(absPath, result)
}

func applyInsertLines(absPath string, op EditOp) error {
	if len(op.Lines) != 1 {
		return fmt.Errorf("lines must have exactly 1 element [lineNum]")
	}
	lineNum := op.Lines[0]
	if lineNum < 1 {
		return fmt.Errorf("invalid line number %d", lineNum)
	}

	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	if lineNum > len(lines)+1 {
		return fmt.Errorf("line %d exceeds file length %d", lineNum, len(lines))
	}

	// Verify anchor: use the target line as a single-line span
	// When inserting at the end (lineNum == len(lines)+1), use last line as anchor
	anchorLine := lineNum
	if anchorLine > len(lines) {
		anchorLine = len(lines)
	}
	actual := ComputeAnchorHash(lines, anchorLine, anchorLine)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Insert content before the specified line number
	newContent := splitContent(op.Content)
	result := make([]string, 0, len(lines)+len(newContent))
	result = append(result, lines[:lineNum-1]...)
	result = append(result, newContent...)
	result = append(result, lines[lineNum-1:]...)

	return writeFileLines(absPath, result)
}

func applyTemplate(absPath string, op EditOp) error {
	if op.Template == "" {
		return fmt.Errorf("template field is required")
	}
	if len(op.Instances) == 0 {
		return fmt.Errorf("instances field is required")
	}

	// Expand template for each instance
	var expanded strings.Builder
	for i, inst := range op.Instances {
		text := op.Template
		for key, val := range inst {
			text = strings.ReplaceAll(text, "{{"+key+"}}", val)
		}
		if i > 0 {
			expanded.WriteByte('\n')
		}
		expanded.WriteString(text)
	}

	// Treat the expanded content as an insert_after_symbol operation
	lines, err := readFileLines(absPath)
	if err != nil {
		return err
	}

	// Find the symbol line
	symbolLine := -1
	for i, line := range lines {
		if strings.Contains(line, op.Symbol) {
			symbolLine = i
			break
		}
	}
	if symbolLine < 0 {
		return fmt.Errorf("symbol %q not found in %s", op.Symbol, absPath)
	}

	// Verify anchor: use the symbol line as a single-line span
	anchorLineNum := symbolLine + 1 // 1-indexed
	actual := ComputeAnchorHash(lines, anchorLineNum, anchorLineNum)
	if actual != op.AnchorHash {
		return fmt.Errorf("anchor mismatch: expected %s, got %s", op.AnchorHash, actual)
	}

	// Insert expanded content after the symbol line
	newContent := splitContent(expanded.String())
	result := make([]string, 0, len(lines)+len(newContent))
	result = append(result, lines[:symbolLine+1]...)
	result = append(result, newContent...)
	result = append(result, lines[symbolLine+1:]...)

	return writeFileLines(absPath, result)
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}
