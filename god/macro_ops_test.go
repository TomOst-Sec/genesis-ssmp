package god

import (
	"strings"
	"testing"
)

func TestExpandRenameSymbol(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:      MacroRenameSymbol,
		OldName:   "HandleCreate",
		NewName:   "HandleCreateV2",
		ScopePath: "handler.go",
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(ir.Ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ir.Ops))
	}
	op := ir.Ops[0]
	if op.Op != "replace_span" {
		t.Errorf("op = %q, want replace_span", op.Op)
	}
	if op.Path != "handler.go" {
		t.Errorf("path = %q, want handler.go", op.Path)
	}
	if op.Content != "HandleCreateV2" {
		t.Errorf("content = %q, want HandleCreateV2", op.Content)
	}
	if op.Symbol != "HandleCreate" {
		t.Errorf("symbol = %q, want HandleCreate", op.Symbol)
	}
	if !strings.HasPrefix(op.AnchorHash, "macro-") {
		t.Errorf("anchor_hash = %q, want macro- prefix", op.AnchorHash)
	}
}

func TestExpandRenameSymbolNoScope(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:    MacroRenameSymbol,
		OldName: "Foo",
		NewName: "Bar",
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if ir.Ops[0].Path != "*" {
		t.Errorf("path = %q, want * (wildcard default)", ir.Ops[0].Path)
	}
}

func TestExpandAddParam(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:      MacroAddParam,
		FuncName:  "HandleCreate",
		ParamName: "ctx",
		ParamType: "context.Context",
		Path:      "handler.go",
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(ir.Ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ir.Ops))
	}
	op := ir.Ops[0]
	if op.Op != "insert_after_symbol" {
		t.Errorf("op = %q, want insert_after_symbol", op.Op)
	}
	if op.Content != "ctx context.Context" {
		t.Errorf("content = %q", op.Content)
	}
	if op.Symbol != "HandleCreate" {
		t.Errorf("symbol = %q", op.Symbol)
	}
}

func TestExpandInsertImport(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:       MacroInsertImport,
		Path:       "handler.go",
		ImportSpec: `"context"`,
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	op := ir.Ops[0]
	if op.Op != "insert_after_symbol" {
		t.Errorf("op = %q", op.Op)
	}
	if op.Symbol != "import" {
		t.Errorf("symbol = %q, want import", op.Symbol)
	}
	if op.Content != `"context"` {
		t.Errorf("content = %q", op.Content)
	}
}

func TestExpandAddTestCase(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:     MacroAddTestCase,
		TestFunc: "TestHandleCreate",
		CaseName: "empty_title",
		CaseBody: `{name: "empty_title", input: "", wantErr: true}`,
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	op := ir.Ops[0]
	if op.Op != "insert_after_symbol" {
		t.Errorf("op = %q", op.Op)
	}
	if op.Symbol != "TestHandleCreate" {
		t.Errorf("symbol = %q", op.Symbol)
	}
}

func TestExpandAddFunctionStub(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:    MacroAddFunctionStub,
		Path:    "handler.go",
		FuncSig: "func HandleDelete(w http.ResponseWriter, r *http.Request) {}",
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	op := ir.Ops[0]
	if op.Op != "insert_after_symbol" {
		t.Errorf("op = %q", op.Op)
	}
	if op.Path != "handler.go" {
		t.Errorf("path = %q", op.Path)
	}
}

func TestExpandReplaceSpan(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{{
		Kind:      MacroReplaceSpan,
		Path:      "handler.go",
		StartLine: 10,
		EndLine:   15,
		Content:   "// replaced content\n",
	}}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	op := ir.Ops[0]
	if op.Op != "replace_span" {
		t.Errorf("op = %q", op.Op)
	}
	if len(op.Lines) != 2 || op.Lines[0] != 10 || op.Lines[1] != 15 {
		t.Errorf("lines = %v, want [10 15]", op.Lines)
	}
}

func TestExpandMultipleMacros(t *testing.T) {
	macros := &MacroOps{Ops: []MacroOp{
		{Kind: MacroInsertImport, Path: "main.go", ImportSpec: `"fmt"`},
		{Kind: MacroAddFunctionStub, Path: "main.go", FuncSig: "func Hello() {}"},
	}}

	ir, err := ExpandMacroOps(macros)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(ir.Ops) != 2 {
		t.Errorf("ops = %d, want 2", len(ir.Ops))
	}
}

func TestValidateMacroOpRequired(t *testing.T) {
	tests := []struct {
		name string
		op   MacroOp
		want string
	}{
		{"rename no old", MacroOp{Kind: MacroRenameSymbol, NewName: "B"}, "old_name required"},
		{"rename no new", MacroOp{Kind: MacroRenameSymbol, OldName: "A"}, "new_name required"},
		{"add_param no func", MacroOp{Kind: MacroAddParam, ParamName: "x", ParamType: "int"}, "func_name required"},
		{"add_param no name", MacroOp{Kind: MacroAddParam, FuncName: "F", ParamType: "int"}, "param_name required"},
		{"add_param no type", MacroOp{Kind: MacroAddParam, FuncName: "F", ParamName: "x"}, "param_type required"},
		{"import no path", MacroOp{Kind: MacroInsertImport, ImportSpec: "x"}, "path required"},
		{"import no spec", MacroOp{Kind: MacroInsertImport, Path: "x.go"}, "import_spec required"},
		{"test no func", MacroOp{Kind: MacroAddTestCase, CaseName: "x", CaseBody: "y"}, "test_func required"},
		{"test no case", MacroOp{Kind: MacroAddTestCase, TestFunc: "T", CaseBody: "y"}, "case_name required"},
		{"test no body", MacroOp{Kind: MacroAddTestCase, TestFunc: "T", CaseName: "x"}, "case_body required"},
		{"stub no path", MacroOp{Kind: MacroAddFunctionStub, FuncSig: "func F() {}"}, "path required"},
		{"stub no sig", MacroOp{Kind: MacroAddFunctionStub, Path: "x.go"}, "func_sig required"},
		{"span no path", MacroOp{Kind: MacroReplaceSpan, StartLine: 1, EndLine: 2}, "path required"},
		{"span bad start", MacroOp{Kind: MacroReplaceSpan, Path: "x.go", StartLine: 0, EndLine: 2}, "start_line must be > 0"},
		{"span bad end", MacroOp{Kind: MacroReplaceSpan, Path: "x.go", StartLine: 5, EndLine: 3}, "end_line must be >= start_line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMacroOp(tt.op, 0)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want to contain %q", err, tt.want)
			}
		})
	}
}

func TestValidateMacroOpUnknownKind(t *testing.T) {
	err := ValidateMacroOp(MacroOp{Kind: "UNKNOWN"}, 0)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("error = %q", err)
	}
}

func TestMacroAnchorDeterministic(t *testing.T) {
	a1 := macroAnchor("rename", "Foo", "Bar")
	a2 := macroAnchor("rename", "Foo", "Bar")
	if a1 != a2 {
		t.Errorf("non-deterministic: %q != %q", a1, a2)
	}

	a3 := macroAnchor("rename", "Foo", "Baz")
	if a1 == a3 {
		t.Error("different inputs produced same anchor")
	}
}
