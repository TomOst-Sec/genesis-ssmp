package god

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// MacroOpKind identifies a macro operation type.
type MacroOpKind string

const (
	MacroRenameSymbol    MacroOpKind = "RENAME_SYMBOL"
	MacroAddParam        MacroOpKind = "ADD_PARAM"
	MacroInsertImport    MacroOpKind = "INSERT_IMPORT"
	MacroAddTestCase     MacroOpKind = "ADD_TEST_CASE"
	MacroAddFunctionStub MacroOpKind = "ADD_FUNCTION_STUB"
	MacroReplaceSpan     MacroOpKind = "REPLACE_SPAN"
)

// MacroOp represents a compact output opcode from an Angel.
type MacroOp struct {
	Kind       MacroOpKind `json:"kind"`
	OldName    string      `json:"old_name,omitempty"`
	NewName    string      `json:"new_name,omitempty"`
	ScopePath  string      `json:"scope_path,omitempty"`
	FuncName   string      `json:"func_name,omitempty"`
	ParamName  string      `json:"param_name,omitempty"`
	ParamType  string      `json:"param_type,omitempty"`
	Position   int         `json:"position,omitempty"`
	Path       string      `json:"path,omitempty"`
	ImportSpec string      `json:"import_spec,omitempty"`
	TestFunc   string      `json:"test_func,omitempty"`
	CaseName   string      `json:"case_name,omitempty"`
	CaseBody   string      `json:"case_body,omitempty"`
	FuncSig    string      `json:"func_sig,omitempty"`
	StartLine  int         `json:"start_line,omitempty"`
	EndLine    int         `json:"end_line,omitempty"`
	Content    string      `json:"content,omitempty"`
}

// MacroOps is a collection of macro operations in an Angel response.
type MacroOps struct {
	Ops []MacroOp `json:"ops"`
}

// ExpandMacroOps converts a MacroOps into an EditIR by expanding each
// macro operation into one or more standard Edit IR operations.
func ExpandMacroOps(macros *MacroOps) (*EditIR, error) {
	var ops []EditOp
	for i, m := range macros.Ops {
		expanded, err := expandMacro(m)
		if err != nil {
			return nil, fmt.Errorf("macro_ops[%d] %s: %w", i, m.Kind, err)
		}
		ops = append(ops, expanded...)
	}
	return &EditIR{Ops: ops}, nil
}

func expandMacro(m MacroOp) ([]EditOp, error) {
	switch m.Kind {
	case MacroRenameSymbol:
		return expandRenameSymbol(m)
	case MacroAddParam:
		return expandAddParam(m)
	case MacroInsertImport:
		return expandInsertImport(m)
	case MacroAddTestCase:
		return expandAddTestCase(m)
	case MacroAddFunctionStub:
		return expandAddFunctionStub(m)
	case MacroReplaceSpan:
		return expandReplaceSpan(m)
	default:
		return nil, fmt.Errorf("unknown macro kind %q", m.Kind)
	}
}

func expandRenameSymbol(m MacroOp) ([]EditOp, error) {
	path := m.ScopePath
	if path == "" {
		path = "*"
	}
	return []EditOp{{
		Op:         "replace_span",
		Path:       path,
		AnchorHash: macroAnchor("rename", m.OldName, m.NewName),
		Content:    m.NewName,
		Symbol:     m.OldName,
	}}, nil
}

func expandAddParam(m MacroOp) ([]EditOp, error) {
	path := m.Path
	if path == "" {
		path = "*"
	}
	content := fmt.Sprintf("%s %s", m.ParamName, m.ParamType)
	return []EditOp{{
		Op:         "insert_after_symbol",
		Path:       path,
		AnchorHash: macroAnchor("add_param", m.FuncName, m.ParamName),
		Content:    content,
		Symbol:     m.FuncName,
	}}, nil
}

func expandInsertImport(m MacroOp) ([]EditOp, error) {
	return []EditOp{{
		Op:         "insert_after_symbol",
		Path:       m.Path,
		AnchorHash: macroAnchor("insert_import", m.Path, m.ImportSpec),
		Content:    m.ImportSpec,
		Symbol:     "import",
	}}, nil
}

func expandAddTestCase(m MacroOp) ([]EditOp, error) {
	path := m.Path
	if path == "" {
		path = "*"
	}
	return []EditOp{{
		Op:         "insert_after_symbol",
		Path:       path,
		AnchorHash: macroAnchor("add_test_case", m.TestFunc, m.CaseName),
		Content:    m.CaseBody,
		Symbol:     m.TestFunc,
	}}, nil
}

func expandAddFunctionStub(m MacroOp) ([]EditOp, error) {
	return []EditOp{{
		Op:         "insert_after_symbol",
		Path:       m.Path,
		AnchorHash: macroAnchor("add_func_stub", m.Path, m.FuncSig),
		Content:    m.FuncSig,
	}}, nil
}

func expandReplaceSpan(m MacroOp) ([]EditOp, error) {
	return []EditOp{{
		Op:         "replace_span",
		Path:       m.Path,
		AnchorHash: macroAnchor("replace_span", m.Path, fmt.Sprintf("%d-%d", m.StartLine, m.EndLine)),
		Lines:      []int{m.StartLine, m.EndLine},
		Content:    m.Content,
	}}, nil
}

// macroAnchor generates a deterministic placeholder anchor hash.
func macroAnchor(parts ...string) string {
	h := sha256.Sum256([]byte("macro:" + strings.Join(parts, ":")))
	return "macro-" + hex.EncodeToString(h[:16])
}

// ValidateMacroOp checks a single macro operation for required fields.
func ValidateMacroOp(m MacroOp, index int) error {
	switch m.Kind {
	case MacroRenameSymbol:
		if m.OldName == "" {
			return fmt.Errorf("macro_ops[%d]: old_name required", index)
		}
		if m.NewName == "" {
			return fmt.Errorf("macro_ops[%d]: new_name required", index)
		}
	case MacroAddParam:
		if m.FuncName == "" {
			return fmt.Errorf("macro_ops[%d]: func_name required", index)
		}
		if m.ParamName == "" {
			return fmt.Errorf("macro_ops[%d]: param_name required", index)
		}
		if m.ParamType == "" {
			return fmt.Errorf("macro_ops[%d]: param_type required", index)
		}
	case MacroInsertImport:
		if m.Path == "" {
			return fmt.Errorf("macro_ops[%d]: path required", index)
		}
		if m.ImportSpec == "" {
			return fmt.Errorf("macro_ops[%d]: import_spec required", index)
		}
	case MacroAddTestCase:
		if m.TestFunc == "" {
			return fmt.Errorf("macro_ops[%d]: test_func required", index)
		}
		if m.CaseName == "" {
			return fmt.Errorf("macro_ops[%d]: case_name required", index)
		}
		if m.CaseBody == "" {
			return fmt.Errorf("macro_ops[%d]: case_body required", index)
		}
	case MacroAddFunctionStub:
		if m.Path == "" {
			return fmt.Errorf("macro_ops[%d]: path required", index)
		}
		if m.FuncSig == "" {
			return fmt.Errorf("macro_ops[%d]: func_sig required", index)
		}
	case MacroReplaceSpan:
		if m.Path == "" {
			return fmt.Errorf("macro_ops[%d]: path required", index)
		}
		if m.StartLine <= 0 {
			return fmt.Errorf("macro_ops[%d]: start_line must be > 0", index)
		}
		if m.EndLine < m.StartLine {
			return fmt.Errorf("macro_ops[%d]: end_line must be >= start_line", index)
		}
	default:
		return fmt.Errorf("macro_ops[%d]: unknown kind %q", index, m.Kind)
	}
	return nil
}
