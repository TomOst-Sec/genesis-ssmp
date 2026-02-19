package god

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePatchV1_Basic(t *testing.T) {
	input := `GENESIS_PATCH_V1
MISSION: m-001
SYMBOLS: parse_regex, gen
FILES: src/tinygrep/cli.py
### REPLACE src/tinygrep/cli.py 17 17
        elif c == '*':
            return ('star',)
### END
### REPLACE src/tinygrep/cli.py 216 244
def gen_star(node, nfa):
    start = nfa.new_state()
    accept = nfa.new_state()
    return (start, accept)
### END
`
	ps, err := ParsePatchV1([]byte(input))
	if err != nil {
		t.Fatalf("ParsePatchV1: %v", err)
	}
	if ps.MissionID != "m-001" {
		t.Errorf("MissionID = %q, want m-001", ps.MissionID)
	}
	if len(ps.Symbols) != 2 {
		t.Errorf("Symbols = %v, want 2 items", ps.Symbols)
	}
	if len(ps.Files) != 1 {
		t.Errorf("Files = %v, want 1 item", ps.Files)
	}
	if len(ps.Ops) != 2 {
		t.Fatalf("Ops count = %d, want 2", len(ps.Ops))
	}

	op0 := ps.Ops[0]
	if op0.Op != "REPLACE" || op0.Path != "src/tinygrep/cli.py" {
		t.Errorf("op0: Op=%q Path=%q", op0.Op, op0.Path)
	}
	if op0.Lines[0] != 17 || op0.Lines[1] != 17 {
		t.Errorf("op0 lines = %v, want [17 17]", op0.Lines)
	}
	if !strings.Contains(op0.Content, "star") {
		t.Errorf("op0 content missing 'star': %q", op0.Content)
	}

	op1 := ps.Ops[1]
	if op1.Lines[0] != 216 || op1.Lines[1] != 244 {
		t.Errorf("op1 lines = %v, want [216 244]", op1.Lines)
	}
}

func TestParsePatchV1_AllOps(t *testing.T) {
	input := `GENESIS_PATCH_V1
MISSION: m-002
SYMBOLS: foo, bar
FILES: a.py, b.py, c.py
### REPLACE a.py 10 12
replaced content
### END
### ADD b.py
new file content
line 2
### END
### DELETE c.py 50 55
### END
### INSERT_AFTER a.py def foo
    inserted after foo
### END
`
	ps, err := ParsePatchV1([]byte(input))
	if err != nil {
		t.Fatalf("ParsePatchV1: %v", err)
	}
	if len(ps.Ops) != 4 {
		t.Fatalf("Ops count = %d, want 4", len(ps.Ops))
	}

	// REPLACE
	if ps.Ops[0].Op != "REPLACE" || ps.Ops[0].Lines[0] != 10 || ps.Ops[0].Lines[1] != 12 {
		t.Errorf("op0 REPLACE: %+v", ps.Ops[0])
	}
	// ADD
	if ps.Ops[1].Op != "ADD" || ps.Ops[1].Content == "" {
		t.Errorf("op1 ADD: %+v", ps.Ops[1])
	}
	// DELETE
	if ps.Ops[2].Op != "DELETE" || ps.Ops[2].Lines[0] != 50 || ps.Ops[2].Lines[1] != 55 {
		t.Errorf("op2 DELETE: %+v", ps.Ops[2])
	}
	if ps.Ops[2].Content != "" {
		t.Errorf("DELETE should have no content, got %q", ps.Ops[2].Content)
	}
	// INSERT_AFTER
	if ps.Ops[3].Op != "INSERT_AFTER" || ps.Ops[3].Symbol != "def foo" {
		t.Errorf("op3 INSERT_AFTER: %+v", ps.Ops[3])
	}
}

func TestParsePatchV1_MultilineContent(t *testing.T) {
	input := `GENESIS_PATCH_V1
MISSION: m-003
SYMBOLS: test
FILES: test.py
### ADD test.py
# This is a comment with # characters
def test():
    """docstring with "quotes" inside"""
    x = {"key": "value"}

    # blank line above
    return x
### END
`
	ps, err := ParsePatchV1([]byte(input))
	if err != nil {
		t.Fatalf("ParsePatchV1: %v", err)
	}
	if len(ps.Ops) != 1 {
		t.Fatalf("Ops = %d, want 1", len(ps.Ops))
	}

	content := ps.Ops[0].Content
	if !strings.Contains(content, `"quotes"`) {
		t.Errorf("content missing quotes: %q", content)
	}
	if !strings.Contains(content, "# blank line above") {
		t.Errorf("content missing hash comment: %q", content)
	}
	// Verify blank line is preserved
	if !strings.Contains(content, "\n\n") {
		t.Errorf("content missing blank line: %q", content)
	}
}

func TestParsePatchV1_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // substring of error
	}{
		{
			name:  "missing header",
			input: "MISSION: m-001\n### REPLACE a.py 1 1\nfoo\n### END\n",
			want:  "missing GENESIS_PATCH_V1 header",
		},
		{
			name:  "missing mission",
			input: "GENESIS_PATCH_V1\nSYMBOLS: foo\nFILES: a.py\n### REPLACE a.py 1 1\nfoo\n### END\n",
			want:  "missing MISSION:",
		},
		{
			name:  "bad op format",
			input: "GENESIS_PATCH_V1\nMISSION: m-001\nSYMBOLS: a\nFILES: a.py\n### REPLACE\n### END\n",
			want:  "malformed op header",
		},
		{
			name:  "unterminated content",
			input: "GENESIS_PATCH_V1\nMISSION: m-001\nSYMBOLS: a\nFILES: a.py\n### ADD a.py\nsome content\n",
			want:  "unterminated op block",
		},
		{
			name:  "REPLACE missing lines",
			input: "GENESIS_PATCH_V1\nMISSION: m-001\nSYMBOLS: a\nFILES: a.py\n### REPLACE a.py 10\nfoo\n### END\n",
			want:  "REPLACE requires path start end",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePatchV1([]byte(tc.input))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPatchV1ToAngelResponse(t *testing.T) {
	ps := &PatchV1Set{
		MissionID: "m-001",
		Symbols:   []string{"parse_regex", "gen_star"},
		Files:     []string{"src/cli.py", "tests/test_star.py"},
		Ops: []PatchV1Op{
			{Op: "REPLACE", Path: "src/cli.py", Lines: []int{17, 17}, Content: "new line 17"},
			{Op: "ADD", Path: "tests/test_star.py", Content: "import pytest\n\ndef test_star():\n    pass"},
			{Op: "DELETE", Path: "src/cli.py", Lines: []int{50, 55}},
			{Op: "INSERT_AFTER", Path: "src/cli.py", Symbol: "def gen", Content: "    # star handler"},
		},
	}

	resp, err := PatchV1ToAngelResponse(ps)
	if err != nil {
		t.Fatalf("PatchV1ToAngelResponse: %v", err)
	}

	if resp.MissionID != "m-001" {
		t.Errorf("MissionID = %q", resp.MissionID)
	}
	if resp.OutputType != "edit_ir" {
		t.Errorf("OutputType = %q", resp.OutputType)
	}
	if resp.EditIR == nil || len(resp.EditIR.Ops) != 4 {
		t.Fatalf("EditIR ops = %v", resp.EditIR)
	}

	// Verify op mappings
	if resp.EditIR.Ops[0].Op != "replace_span" {
		t.Errorf("op0 = %q, want replace_span", resp.EditIR.Ops[0].Op)
	}
	if resp.EditIR.Ops[1].Op != "add_file" {
		t.Errorf("op1 = %q, want add_file", resp.EditIR.Ops[1].Op)
	}
	if resp.EditIR.Ops[2].Op != "delete_span" {
		t.Errorf("op2 = %q, want delete_span", resp.EditIR.Ops[2].Op)
	}
	if resp.EditIR.Ops[3].Op != "insert_after_symbol" {
		t.Errorf("op3 = %q, want insert_after_symbol", resp.EditIR.Ops[3].Op)
	}

	// Verify all anchor hashes are placeholder
	for i, op := range resp.EditIR.Ops {
		if op.AnchorHash != "placeholder" {
			t.Errorf("op[%d] anchor_hash = %q, want placeholder", i, op.AnchorHash)
		}
	}

	// Verify manifest
	if len(resp.Manifest.SymbolsTouched) != 2 {
		t.Errorf("symbols_touched = %v", resp.Manifest.SymbolsTouched)
	}
	if len(resp.Manifest.FilesTouched) != 2 {
		t.Errorf("files_touched = %v", resp.Manifest.FilesTouched)
	}
}

func TestPatchV1RoundTrip(t *testing.T) {
	input := `GENESIS_PATCH_V1
MISSION: m-roundtrip
SYMBOLS: alpha, beta
FILES: src/main.py, tests/test_main.py
### REPLACE src/main.py 10 15
def alpha():
    return 42
### END
### ADD tests/test_main.py
def test_alpha():
    assert alpha() == 42
### END
`
	ps, err := ParsePatchV1([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := ValidatePatchV1(ps); err != nil {
		t.Fatalf("validate: %v", err)
	}
	resp, err := PatchV1ToAngelResponse(ps)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	// Validate with standard validator
	if err := validateAngelResponse(resp, "m-roundtrip"); err != nil {
		t.Fatalf("validateAngelResponse: %v", err)
	}
}

func TestValidatePatchV1_Errors(t *testing.T) {
	tests := []struct {
		name string
		ps   *PatchV1Set
		want string
	}{
		{
			name: "missing mission_id",
			ps:   &PatchV1Set{Symbols: []string{}, Files: []string{}, Ops: []PatchV1Op{{Op: "ADD", Path: "a.py", Content: "x"}}},
			want: "missing mission_id",
		},
		{
			name: "missing symbols",
			ps:   &PatchV1Set{MissionID: "m", Files: []string{}, Ops: []PatchV1Op{{Op: "ADD", Path: "a.py", Content: "x"}}},
			want: "missing symbols",
		},
		{
			name: "missing files",
			ps:   &PatchV1Set{MissionID: "m", Symbols: []string{}, Ops: []PatchV1Op{{Op: "ADD", Path: "a.py", Content: "x"}}},
			want: "missing files",
		},
		{
			name: "no ops",
			ps:   &PatchV1Set{MissionID: "m", Symbols: []string{}, Files: []string{}},
			want: "no ops",
		},
		{
			name: "REPLACE bad range",
			ps:   &PatchV1Set{MissionID: "m", Symbols: []string{}, Files: []string{}, Ops: []PatchV1Op{{Op: "REPLACE", Path: "a.py", Lines: []int{5, 3}, Content: "x"}}},
			want: "invalid line range",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePatchV1(tc.ps)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

// --- Delta decoder tests ---

func TestDecodePatchV1Content_KeepAll(t *testing.T) {
	orig := []string{"line1", "line2", "line3", "line4"}
	decoded, err := DecodePatchV1Content("@K4", orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	want := "line1\nline2\nline3\nline4"
	if decoded != want {
		t.Errorf("got %q, want %q", decoded, want)
	}
}

func TestDecodePatchV1Content_KeepOne(t *testing.T) {
	orig := []string{"alpha", "beta", "gamma"}
	decoded, err := DecodePatchV1Content("@K\n@K\n@K", orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	want := "alpha\nbeta\ngamma"
	if decoded != want {
		t.Errorf("got %q, want %q", decoded, want)
	}
}

func TestDecodePatchV1Content_InsertMiddle(t *testing.T) {
	orig := []string{
		"def gen(node, nfa):",
		"    op = node[0]",
		"    if op == 'literal':",
		"        return gen_literal(node, nfa)",
		"    elif op == 'plus':",
		"        return gen_plus(node, nfa)",
		"    return start, accept",
	}
	// Keep first 6, insert 2 new lines, keep last 1
	encoded := "@K6\n    elif op == 'star':\n        return gen_star(node, nfa)\n@K"
	decoded, err := DecodePatchV1Content(encoded, orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}

	lines := strings.Split(decoded, "\n")
	if len(lines) != 9 { // 6 kept + 2 new + 1 kept
		t.Fatalf("got %d lines, want 9:\n%s", len(lines), decoded)
	}
	if lines[6] != "    elif op == 'star':" {
		t.Errorf("line 6 = %q", lines[6])
	}
	if lines[8] != "    return start, accept" {
		t.Errorf("line 8 = %q, want original last line", lines[8])
	}
}

func TestDecodePatchV1Content_SkipLines(t *testing.T) {
	orig := []string{"keep1", "delete_me", "delete_me_too", "keep2"}
	encoded := "@K\n@S2\n@K"
	decoded, err := DecodePatchV1Content(encoded, orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	want := "keep1\nkeep2"
	if decoded != want {
		t.Errorf("got %q, want %q", decoded, want)
	}
}

func TestDecodePatchV1Content_MixedOps(t *testing.T) {
	orig := []string{
		"line_a",     // 0: keep
		"line_b_old", // 1: skip + replace
		"line_c",     // 2: keep
		"line_d",     // 3: skip (delete)
		"line_e",     // 4: keep
	}
	// Keep first, skip+replace second, keep third, skip fourth, keep fifth
	encoded := "@K\n@S\nline_b_new\n@K\n@S\n@K"
	decoded, err := DecodePatchV1Content(encoded, orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	want := "line_a\nline_b_new\nline_c\nline_e"
	if decoded != want {
		t.Errorf("got %q, want %q", decoded, want)
	}
}

func TestDecodePatchV1Content_EscapeAt(t *testing.T) {
	orig := []string{"first", "second"}
	// @@ escape: literal line starting with @
	encoded := "@K\n@@decorator\n@@K_not_a_directive\n@K"
	decoded, err := DecodePatchV1Content(encoded, orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	want := "first\n@decorator\n@K_not_a_directive\nsecond"
	if decoded != want {
		t.Errorf("got %q, want %q", decoded, want)
	}
}

func TestDecodePatchV1Content_Errors(t *testing.T) {
	orig := []string{"only_one_line"}

	tests := []struct {
		name    string
		encoded string
		want    string
	}{
		{"K past end", "@K\n@K", "past end of span"},
		{"K{N} past end", "@K5", "exceeds span"},
		{"S past end", "@S\n@S", "past end of span"},
		{"S{N} past end", "@S5", "exceeds span"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodePatchV1Content(tc.encoded, orig)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q missing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDecodePatchV1Content_LiteralOnly(t *testing.T) {
	// No @K/@S — all literal (like the old verbatim mode)
	orig := []string{"old1", "old2"}
	encoded := "new1\nnew2\nnew3"
	decoded, err := DecodePatchV1Content(encoded, orig)
	if err != nil {
		t.Fatalf("DecodePatchV1Content: %v", err)
	}
	if decoded != encoded {
		t.Errorf("got %q, want %q", decoded, encoded)
	}
}

func TestDecodePatchV1Set(t *testing.T) {
	// Create a temp file to decode against
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.py")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := &PatchV1Set{
		MissionID: "m-test",
		Symbols:   []string{"test"},
		Files:     []string{"test.py"},
		Ops: []PatchV1Op{
			{
				Op:      "REPLACE",
				Path:    "test.py",
				Lines:   []int{2, 4},
				Content: "@K\n@S\nnew_line3\n@K", // keep line2, skip line3, insert new, keep line4
			},
			{
				Op:      "ADD",
				Path:    "new.py",
				Content: "brand new file", // ADD is never decoded
			},
		},
	}

	if err := DecodePatchV1Set(ps, dir); err != nil {
		t.Fatalf("DecodePatchV1Set: %v", err)
	}

	// REPLACE content should be decoded
	want := "line2\nnew_line3\nline4"
	if ps.Ops[0].Content != want {
		t.Errorf("REPLACE content = %q, want %q", ps.Ops[0].Content, want)
	}

	// ADD content should be unchanged
	if ps.Ops[1].Content != "brand new file" {
		t.Errorf("ADD content changed: %q", ps.Ops[1].Content)
	}
}

func TestDecodePatchV1Set_NoDirectives(t *testing.T) {
	// REPLACE with literal content (no @K/@S) should pass through unchanged
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.py")
	os.WriteFile(filePath, []byte("old\n"), 0o644)

	ps := &PatchV1Set{
		MissionID: "m",
		Symbols:   []string{},
		Files:     []string{"test.py"},
		Ops: []PatchV1Op{
			{Op: "REPLACE", Path: "test.py", Lines: []int{1, 1}, Content: "literally new"},
		},
	}

	if err := DecodePatchV1Set(ps, dir); err != nil {
		t.Fatalf("DecodePatchV1Set: %v", err)
	}
	if ps.Ops[0].Content != "literally new" {
		t.Errorf("content changed: %q", ps.Ops[0].Content)
	}
}
