package heaven

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// langConfig maps file extensions to tree-sitter languages and their symbol node types.
type langConfig struct {
	lang     *sitter.Language
	defTypes map[string]string // node type -> symbol kind
}

var langConfigs = map[string]langConfig{
	".go": {
		lang: golang.GetLanguage(),
		defTypes: map[string]string{
			"function_declaration":  "function",
			"method_declaration":    "method",
			"type_declaration":      "type",
			"type_spec":             "type",
			"var_declaration":       "variable",
			"const_declaration":     "constant",
			"short_var_declaration": "variable",
		},
	},
	".py": {
		lang: python.GetLanguage(),
		defTypes: map[string]string{
			"function_definition": "function",
			"class_definition":    "class",
		},
	},
	".ts": {
		lang: typescript.GetLanguage(),
		defTypes: map[string]string{
			"function_declaration":   "function",
			"class_declaration":      "class",
			"lexical_declaration":    "variable",
			"variable_declaration":   "variable",
			"method_definition":      "method",
			"interface_declaration":  "interface",
			"type_alias_declaration": "type",
			"enum_declaration":       "enum",
		},
	},
}

// langForFile returns the langConfig for a file based on its extension.
func langForFile(path string) (langConfig, bool) {
	ext := filepath.Ext(path)
	lc, ok := langConfigs[ext]
	return lc, ok
}

// BuildIndex walks repoPath and indexes all supported files into the IRIndex.
// Returns the number of files indexed.
func BuildIndex(ctx context.Context, idx *IRIndex, repoPath string) (int, error) {
	var indexed int
	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "node_modules" || base == "vendor" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		if _, ok := langForFile(path); !ok {
			return nil
		}

		mtime := info.ModTime().Unix()
		needsReindex, err := idx.FileNeedsReindex(path, mtime)
		if err != nil {
			return fmt.Errorf("check reindex %s: %w", path, err)
		}
		if !needsReindex {
			return nil
		}

		if err := indexFile(ctx, idx, path, mtime); err != nil {
			return fmt.Errorf("index %s: %w", path, err)
		}
		indexed++
		return nil
	})
	return indexed, err
}

func indexFile(ctx context.Context, idx *IRIndex, path string, mtime int64) error {
	lc, ok := langForFile(path)
	if !ok {
		return nil
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	root, err := sitter.ParseCtx(ctx, src, lc.lang)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	hash, err := FileHash(path)
	if err != nil {
		return err
	}

	tx, err := idx.BeginFileTx(path)
	if err != nil {
		return err
	}

	// Pass 1: extract symbol definitions
	symbolIDs := make(map[string]int64) // name -> symbol id for ref linking
	walkTree(root, func(node *sitter.Node) {
		kind, isDef := lc.defTypes[node.Type()]
		if !isDef {
			return
		}
		name := extractSymbolName(node, src)
		if name == "" {
			return
		}
		id, err := idx.InsertSymbol(tx, Symbol{
			Name:      name,
			Kind:      kind,
			Path:      path,
			StartByte: int(node.StartByte()),
			EndByte:   int(node.EndByte()),
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
		})
		if err == nil {
			symbolIDs[name] = id
		}
	})

	// Pass 2: extract identifier references (skip definition names)
	walkTree(root, func(node *sitter.Node) {
		if node.Type() != "identifier" && node.Type() != "type_identifier" {
			return
		}
		// Skip identifiers that are the "name" field of a definition
		if isDefinitionName(node, src) {
			return
		}
		name := node.Content(src)
		symID, ok := symbolIDs[name]
		if !ok {
			// Try to find symbol in existing index
			syms, err := idx.Symdef(name)
			if err != nil || len(syms) == 0 {
				return
			}
			symID = syms[0].ID
		}
		idx.InsertRef(tx, Ref{
			SymbolID:  symID,
			Path:      path,
			StartByte: int(node.StartByte()),
			EndByte:   int(node.EndByte()),
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			RefKind:   classifyRef(node),
		})
	})

	if err := idx.UpdateFileRecord(tx, IndexedFile{
		Path:  path,
		Hash:  hash,
		Mtime: mtime,
	}); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// extractSymbolName gets the name identifier from a definition node.
func extractSymbolName(node *sitter.Node, src []byte) string {
	// Try "name" field first (Go, Python, TS function/class declarations)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		fieldName := node.FieldNameForChild(i)
		if fieldName == "name" {
			return child.Content(src)
		}
	}
	// For type_spec, try the first identifier child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "identifier" || child.Type() == "type_identifier" {
			return child.Content(src)
		}
	}
	return ""
}

// isDefinitionName checks if an identifier node is the name part of a definition.
func isDefinitionName(node *sitter.Node, src []byte) bool {
	parent := node.Parent()
	if parent == nil {
		return false
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		child := parent.Child(i)
		if child.StartByte() == node.StartByte() && child.EndByte() == node.EndByte() {
			fieldName := parent.FieldNameForChild(i)
			if fieldName == "name" {
				return true
			}
		}
	}
	return false
}

// classifyRef determines the kind of reference based on the parent node.
func classifyRef(node *sitter.Node) string {
	parent := node.Parent()
	if parent == nil {
		return "reference"
	}
	switch parent.Type() {
	case "call_expression", "call":
		return "call"
	case "import_declaration", "import_statement", "import_from_statement":
		return "import"
	case "assignment", "assignment_expression", "short_var_declaration":
		return "assignment"
	default:
		return "reference"
	}
}

// walkTree walks all nodes in the tree, calling fn for each.
func walkTree(node *sitter.Node, fn func(*sitter.Node)) {
	fn(node)
	for i := 0; i < int(node.ChildCount()); i++ {
		walkTree(node.Child(i), fn)
	}
}

// InsertRefTx is a convenience for inserting refs within an existing transaction.
func (idx *IRIndex) InsertRefTx(tx *sql.Tx, r Ref) error {
	return idx.InsertRef(tx, r)
}
