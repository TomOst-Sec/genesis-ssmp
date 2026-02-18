package god

import (
	"fmt"
	"strconv"
	"strings"
)

// PatchV1Op represents a single operation in GENESIS_PATCH_V1 format.
type PatchV1Op struct {
	Op      string // "REPLACE", "ADD", "DELETE", "INSERT_AFTER"
	Path    string
	Lines   []int  // [start, end] for REPLACE/DELETE
	Symbol  string // for INSERT_AFTER
	Content string // raw content (no escaping)
}

// PatchV1Set is the parsed representation of a GENESIS_PATCH_V1 response.
type PatchV1Set struct {
	MissionID string
	Symbols   []string
	Files     []string
	Ops       []PatchV1Op
}

// ParsePatchV1 parses a GENESIS_PATCH_V1 text block into a PatchV1Set.
func ParsePatchV1(raw []byte) (*PatchV1Set, error) {
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("patch_v1: empty input")
	}

	ps := &PatchV1Set{}
	i := 0

	// Skip leading blank lines
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}

	// Expect GENESIS_PATCH_V1 header
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "GENESIS_PATCH_V1" {
		return nil, fmt.Errorf("patch_v1: missing GENESIS_PATCH_V1 header")
	}
	i++

	// Parse metadata lines (MISSION:, SYMBOLS:, FILES:)
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		if strings.HasPrefix(line, "### ") {
			break // start of ops
		}
		if after, found := strings.CutPrefix(line, "MISSION:"); found {
			ps.MissionID = strings.TrimSpace(after)
		} else if after, found := strings.CutPrefix(line, "SYMBOLS:"); found {
			ps.Symbols = splitCSV(after)
		} else if after, found := strings.CutPrefix(line, "FILES:"); found {
			ps.Files = splitCSV(after)
		}
		i++
	}

	if ps.MissionID == "" {
		return nil, fmt.Errorf("patch_v1: missing MISSION: header")
	}

	// Parse ops
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}

		if !strings.HasPrefix(line, "### ") {
			i++
			continue
		}

		op, advance, err := parsePatchV1Op(lines, i)
		if err != nil {
			return nil, fmt.Errorf("patch_v1 line %d: %w", i+1, err)
		}
		ps.Ops = append(ps.Ops, op)
		i += advance
	}

	return ps, nil
}

// parsePatchV1Op parses a single op block starting at lines[start].
// Returns the op and number of lines consumed.
func parsePatchV1Op(lines []string, start int) (PatchV1Op, int, error) {
	header := strings.TrimSpace(lines[start])
	parts := strings.Fields(header)
	// parts[0] = "###", parts[1] = OP, parts[2] = path, ...

	if len(parts) < 3 {
		return PatchV1Op{}, 0, fmt.Errorf("malformed op header: %q", header)
	}

	opType := parts[1]
	path := parts[2]

	var op PatchV1Op
	op.Op = opType
	op.Path = path

	switch opType {
	case "REPLACE":
		if len(parts) < 5 {
			return PatchV1Op{}, 0, fmt.Errorf("REPLACE requires path start end: %q", header)
		}
		startLine, err := strconv.Atoi(parts[3])
		if err != nil {
			return PatchV1Op{}, 0, fmt.Errorf("REPLACE invalid start line %q: %w", parts[3], err)
		}
		endLine, err := strconv.Atoi(parts[4])
		if err != nil {
			return PatchV1Op{}, 0, fmt.Errorf("REPLACE invalid end line %q: %w", parts[4], err)
		}
		op.Lines = []int{startLine, endLine}

	case "ADD":
		// ### ADD path — no extra fields

	case "DELETE":
		if len(parts) < 5 {
			return PatchV1Op{}, 0, fmt.Errorf("DELETE requires path start end: %q", header)
		}
		startLine, err := strconv.Atoi(parts[3])
		if err != nil {
			return PatchV1Op{}, 0, fmt.Errorf("DELETE invalid start line %q: %w", parts[3], err)
		}
		endLine, err := strconv.Atoi(parts[4])
		if err != nil {
			return PatchV1Op{}, 0, fmt.Errorf("DELETE invalid end line %q: %w", parts[4], err)
		}
		op.Lines = []int{startLine, endLine}

	case "INSERT_AFTER":
		if len(parts) < 4 {
			return PatchV1Op{}, 0, fmt.Errorf("INSERT_AFTER requires path symbol: %q", header)
		}
		op.Symbol = strings.Join(parts[3:], " ")

	default:
		return PatchV1Op{}, 0, fmt.Errorf("unknown op type %q", opType)
	}

	// Collect content lines until ### END
	i := start + 1
	var contentLines []string
	foundEnd := false
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "### END" {
			foundEnd = true
			i++
			break
		}
		contentLines = append(contentLines, lines[i])
		i++
	}

	if !foundEnd {
		return PatchV1Op{}, 0, fmt.Errorf("unterminated op block (missing ### END)")
	}

	// DELETE ops have no content
	if opType != "DELETE" && len(contentLines) > 0 {
		op.Content = strings.Join(contentLines, "\n")
	}

	return op, i - start, nil
}

// PatchV1ToAngelResponse converts a PatchV1Set to a standard AngelResponse.
func PatchV1ToAngelResponse(ps *PatchV1Set) (*AngelResponse, error) {
	if err := ValidatePatchV1(ps); err != nil {
		return nil, err
	}

	var ops []EditOp
	for _, pop := range ps.Ops {
		var eop EditOp
		switch pop.Op {
		case "REPLACE":
			eop = EditOp{
				Op:         "replace_span",
				Path:       pop.Path,
				AnchorHash: "placeholder",
				Lines:      pop.Lines,
				Content:    pop.Content,
			}
		case "ADD":
			eop = EditOp{
				Op:         "add_file",
				Path:       pop.Path,
				AnchorHash: "placeholder",
				Content:    pop.Content,
			}
		case "DELETE":
			eop = EditOp{
				Op:         "delete_span",
				Path:       pop.Path,
				AnchorHash: "placeholder",
				Lines:      pop.Lines,
			}
		case "INSERT_AFTER":
			eop = EditOp{
				Op:         "insert_after_symbol",
				Path:       pop.Path,
				AnchorHash: "placeholder",
				Symbol:     pop.Symbol,
				Content:    pop.Content,
			}
		default:
			return nil, fmt.Errorf("unknown patch_v1 op: %s", pop.Op)
		}
		ops = append(ops, eop)
	}

	return &AngelResponse{
		MissionID:  ps.MissionID,
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: ops},
		Manifest: Manifest{
			SymbolsTouched: ps.Symbols,
			FilesTouched:   ps.Files,
		},
	}, nil
}

// ValidatePatchV1 checks structural validity of a PatchV1Set.
func ValidatePatchV1(ps *PatchV1Set) error {
	if ps.MissionID == "" {
		return fmt.Errorf("patch_v1: missing mission_id")
	}
	if ps.Symbols == nil {
		return fmt.Errorf("patch_v1: missing symbols list")
	}
	if ps.Files == nil {
		return fmt.Errorf("patch_v1: missing files list")
	}
	if len(ps.Ops) == 0 {
		return fmt.Errorf("patch_v1: no ops")
	}

	for i, op := range ps.Ops {
		if op.Path == "" {
			return fmt.Errorf("patch_v1: op[%d] missing path", i)
		}
		switch op.Op {
		case "REPLACE":
			if len(op.Lines) != 2 {
				return fmt.Errorf("patch_v1: op[%d] REPLACE requires [start, end] lines", i)
			}
			if op.Lines[0] < 1 || op.Lines[1] < op.Lines[0] {
				return fmt.Errorf("patch_v1: op[%d] REPLACE invalid line range [%d, %d]", i, op.Lines[0], op.Lines[1])
			}
		case "ADD":
			// Empty content is valid (e.g. __init__.py)
		case "DELETE":
			if len(op.Lines) != 2 {
				return fmt.Errorf("patch_v1: op[%d] DELETE requires [start, end] lines", i)
			}
			if op.Lines[0] < 1 || op.Lines[1] < op.Lines[0] {
				return fmt.Errorf("patch_v1: op[%d] DELETE invalid line range [%d, %d]", i, op.Lines[0], op.Lines[1])
			}
		case "INSERT_AFTER":
			if op.Symbol == "" {
				return fmt.Errorf("patch_v1: op[%d] INSERT_AFTER requires symbol", i)
			}
		default:
			return fmt.Errorf("patch_v1: op[%d] unknown op %q", i, op.Op)
		}
	}

	return nil
}

// DecodePatchV1Content expands delta-encoded content for a REPLACE op.
// origSpan is the slice of original file lines covering [startLine, endLine].
//
// Directives within the encoded content:
//   - @K      — keep 1 original line at current position
//   - @K{N}   — keep N consecutive original lines (e.g. @K15)
//   - @S      — skip 1 original line (delete)
//   - @S{N}   — skip N consecutive original lines
//   - @@...   — escape: literal line starting with @
//   - anything else — literal new line
func DecodePatchV1Content(encoded string, origSpan []string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	lines := strings.Split(encoded, "\n")
	var output []string
	origPos := 0

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Normalize @K{N} → @KN and @S{N} → @SN (Angel sometimes uses brace syntax)
		if (strings.HasPrefix(trimmed, "@K{") || strings.HasPrefix(trimmed, "@S{")) && strings.HasSuffix(trimmed, "}") {
			trimmed = trimmed[:2] + trimmed[3:len(trimmed)-1]
		}

		switch {
		case trimmed == "@K":
			// Keep 1 original line
			if origPos >= len(origSpan) {
				return "", fmt.Errorf("line %d: @K at position %d past end of span (%d lines)", lineNum+1, origPos, len(origSpan))
			}
			output = append(output, origSpan[origPos])
			origPos++

		case strings.HasPrefix(trimmed, "@K") && len(trimmed) > 2 && trimmed[2] >= '0' && trimmed[2] <= '9':
			// Keep N original lines
			n, err := strconv.Atoi(trimmed[2:])
			if err != nil {
				return "", fmt.Errorf("line %d: invalid @K count %q: %w", lineNum+1, trimmed, err)
			}
			if n < 1 {
				return "", fmt.Errorf("line %d: @K count must be >= 1, got %d", lineNum+1, n)
			}
			if origPos+n > len(origSpan) {
				return "", fmt.Errorf("line %d: @K%d at position %d exceeds span (%d lines)", lineNum+1, n, origPos, len(origSpan))
			}
			output = append(output, origSpan[origPos:origPos+n]...)
			origPos += n

		case trimmed == "@S":
			// Skip 1 original line
			if origPos >= len(origSpan) {
				return "", fmt.Errorf("line %d: @S at position %d past end of span (%d lines)", lineNum+1, origPos, len(origSpan))
			}
			origPos++

		case strings.HasPrefix(trimmed, "@S") && len(trimmed) > 2 && trimmed[2] >= '0' && trimmed[2] <= '9':
			// Skip N original lines
			n, err := strconv.Atoi(trimmed[2:])
			if err != nil {
				return "", fmt.Errorf("line %d: invalid @S count %q: %w", lineNum+1, trimmed, err)
			}
			if n < 1 {
				return "", fmt.Errorf("line %d: @S count must be >= 1, got %d", lineNum+1, n)
			}
			if origPos+n > len(origSpan) {
				return "", fmt.Errorf("line %d: @S%d at position %d exceeds span (%d lines)", lineNum+1, n, origPos, len(origSpan))
			}
			origPos += n

		case strings.HasPrefix(line, "@@"):
			// Escape: strip first @ for literal
			output = append(output, line[1:])

		default:
			// Literal new line
			output = append(output, line)
		}
	}

	return strings.Join(output, "\n"), nil
}

// DecodePatchV1Set decodes all delta-encoded REPLACE ops in a PatchV1Set
// by expanding @K/@S markers using original files loaded from repoRoot.
// Ops are modified in-place: op.Content is replaced with fully-expanded content.
func DecodePatchV1Set(ps *PatchV1Set, repoRoot string) error {
	for i := range ps.Ops {
		op := &ps.Ops[i]
		if op.Op != "REPLACE" || len(op.Lines) != 2 {
			continue
		}
		if op.Content == "" {
			continue
		}

		// Check if content uses delta encoding (contains @K or @S directives)
		if !containsDeltaDirectives(op.Content) {
			continue // literal content, no decoding needed
		}

		absPath := repoRoot + "/" + op.Path
		fileLines, err := readFileLines(absPath)
		if err != nil {
			return fmt.Errorf("decode op[%d] %s: %w", i, op.Path, err)
		}

		startLine, endLine := op.Lines[0], op.Lines[1]
		if startLine < 1 || endLine > len(fileLines) || endLine < startLine {
			return fmt.Errorf("decode op[%d] %s: line range [%d, %d] out of bounds (file has %d lines)",
				i, op.Path, startLine, endLine, len(fileLines))
		}

		origSpan := fileLines[startLine-1 : endLine]
		decoded, err := DecodePatchV1Content(op.Content, origSpan)
		if err != nil {
			return fmt.Errorf("decode op[%d] %s lines [%d,%d]: %w", i, op.Path, startLine, endLine, err)
		}
		op.Content = decoded
	}
	return nil
}

// containsDeltaDirectives checks if content contains @K or @S directives.
func containsDeltaDirectives(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "@K" || trimmed == "@S" {
			return true
		}
		if (strings.HasPrefix(trimmed, "@K") || strings.HasPrefix(trimmed, "@S")) && len(trimmed) > 2 {
			ch := trimmed[2]
			if ch >= '0' && ch <= '9' {
				return true
			}
			// Brace syntax: @K{N} or @S{N}
			if ch == '{' {
				return true
			}
		}
	}
	return false
}

// splitCSV splits a comma-separated string into trimmed non-empty tokens.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if result == nil {
		result = []string{}
	}
	return result
}
