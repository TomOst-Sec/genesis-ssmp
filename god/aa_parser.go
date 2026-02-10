package god

import (
	"fmt"
	"strconv"
	"strings"
)

// AAProgram is the AST produced by parsing Angel Assembly source text.
type AAProgram struct {
	BaseRev string
	Leases  []Scope
	Needs   []AANeed
	Dos     []string
	Asserts []AAAssert
	Return  string
}

// AANeed represents a prefetch requirement in the AA program.
type AANeed struct {
	Kind   string // "symdef", "callers", "slice"
	Symbol string // symdef, callers
	Path   string // slice
	Start  int    // slice
	N      int    // slice line count, callers top_k
}

// AAAssert represents a verification assertion in the AA program.
type AAAssert struct {
	Kind     string // "tests"
	Selector string // test selector
}

// AAParseError reports a parse error at a specific line.
type AAParseError struct {
	Line    int
	Message string
}

func (e *AAParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// ParseAA parses Angel Assembly source text into an AAProgram AST.
func ParseAA(source string) (*AAProgram, error) {
	prog := &AAProgram{}
	lines := strings.Split(source, "\n")
	baseRevCount := 0
	returnCount := 0

	for i, line := range lines {
		lineNum := i + 1

		// Trim whitespace
		line = strings.TrimSpace(line)

		// Skip blank lines
		if line == "" {
			continue
		}

		// Skip full-line comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments (space before # required)
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// Split into keyword + rest
		parts := strings.SplitN(line, " ", 2)
		keyword := parts[0]
		rest := ""
		if len(parts) > 1 {
			rest = strings.TrimSpace(parts[1])
		}

		switch keyword {
		case "BASE_REV":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "BASE_REV requires a value"}
			}
			baseRevCount++
			if baseRevCount > 1 {
				return nil, &AAParseError{Line: lineNum, Message: "duplicate BASE_REV"}
			}
			prog.BaseRev = rest

		case "LEASE":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "LEASE requires at least one scope_type:scope_value pair"}
			}
			pairs := strings.Fields(rest)
			for _, pair := range pairs {
				colonIdx := strings.Index(pair, ":")
				if colonIdx < 1 || colonIdx >= len(pair)-1 {
					return nil, &AAParseError{Line: lineNum, Message: fmt.Sprintf("invalid LEASE format %q, expected scope_type:scope_value", pair)}
				}
				prog.Leases = append(prog.Leases, Scope{
					ScopeType:  pair[:colonIdx],
					ScopeValue: pair[colonIdx+1:],
				})
			}

		case "NEED":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "NEED requires arguments"}
			}
			need, err := parseNeed(rest, lineNum)
			if err != nil {
				return nil, err
			}
			prog.Needs = append(prog.Needs, need)

		case "DO":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "DO requires a description"}
			}
			prog.Dos = append(prog.Dos, rest)

		case "ASSERT":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "ASSERT requires arguments"}
			}
			assert, err := parseAssert(rest, lineNum)
			if err != nil {
				return nil, err
			}
			prog.Asserts = append(prog.Asserts, assert)

		case "RETURN":
			if rest == "" {
				return nil, &AAParseError{Line: lineNum, Message: "RETURN requires a value"}
			}
			returnCount++
			if returnCount > 1 {
				return nil, &AAParseError{Line: lineNum, Message: "duplicate RETURN"}
			}
			prog.Return = rest

		default:
			return nil, &AAParseError{Line: lineNum, Message: fmt.Sprintf("unknown directive %q", keyword)}
		}
	}

	// Validate required fields
	if prog.BaseRev == "" {
		return nil, &AAParseError{Line: 0, Message: "missing required BASE_REV"}
	}
	if len(prog.Leases) == 0 {
		return nil, &AAParseError{Line: 0, Message: "missing required LEASE (at least one needed)"}
	}
	if len(prog.Dos) == 0 {
		return nil, &AAParseError{Line: 0, Message: "missing required DO (at least one needed)"}
	}
	if prog.Return == "" {
		return nil, &AAParseError{Line: 0, Message: "missing required RETURN"}
	}

	return prog, nil
}

// parseNeed parses the arguments of a NEED directive.
func parseNeed(rest string, lineNum int) (AANeed, error) {
	fields := strings.Fields(rest)
	kind := fields[0]

	switch kind {
	case "symdef":
		if len(fields) != 2 {
			return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED symdef requires exactly 1 argument: symbol"}
		}
		return AANeed{Kind: "symdef", Symbol: fields[1]}, nil

	case "callers":
		if len(fields) < 2 || len(fields) > 3 {
			return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED callers requires 1-2 arguments: symbol [top_k]"}
		}
		need := AANeed{Kind: "callers", Symbol: fields[1]}
		if len(fields) == 3 {
			n, err := strconv.Atoi(fields[2])
			if err != nil || n <= 0 {
				return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED callers top_k must be a positive integer"}
			}
			need.N = n
		}
		return need, nil

	case "slice":
		if len(fields) != 4 {
			return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED slice requires exactly 3 arguments: path start_line line_count"}
		}
		start, err := strconv.Atoi(fields[2])
		if err != nil || start < 0 {
			return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED slice start_line must be a non-negative integer"}
		}
		n, err := strconv.Atoi(fields[3])
		if err != nil || n <= 0 {
			return AANeed{}, &AAParseError{Line: lineNum, Message: "NEED slice line_count must be a positive integer"}
		}
		return AANeed{Kind: "slice", Path: fields[1], Start: start, N: n}, nil

	default:
		return AANeed{}, &AAParseError{Line: lineNum, Message: fmt.Sprintf("unknown NEED kind %q (expected symdef, callers, or slice)", kind)}
	}
}

// parseAssert parses the arguments of an ASSERT directive.
func parseAssert(rest string, lineNum int) (AAAssert, error) {
	fields := strings.Fields(rest)
	kind := fields[0]

	switch kind {
	case "tests":
		if len(fields) != 2 {
			return AAAssert{}, &AAParseError{Line: lineNum, Message: "ASSERT tests requires exactly 1 argument: selector"}
		}
		return AAAssert{Kind: "tests", Selector: fields[1]}, nil

	default:
		return AAAssert{}, &AAParseError{Line: lineNum, Message: fmt.Sprintf("unknown ASSERT kind %q (expected tests)", kind)}
	}
}
