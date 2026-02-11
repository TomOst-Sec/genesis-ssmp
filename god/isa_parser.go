package god

import (
	"fmt"
	"strconv"
	"strings"
)

// ISAVersion is the current instruction set version.
const ISAVersion = 0

// ISAProgram is the AST produced by parsing Genesis ISA v0 source text.
type ISAProgram struct {
	Version    int
	BaseRev    string
	PromptRef  string
	Mode       string // "SOLO" or "SWARM"
	Budget     int    // token budget, 0 = use default
	Invariants []string
	Needs      []ISANeed
	Ops        []string
	Runs       []ISARun
	Asserts    []ISAAssert
	IfFails    []ISAIfFail
	Labels     []string
	Halt       bool
}

// ISANeed represents a prefetch requirement in the ISA program.
type ISANeed struct {
	Kind    string // "symdef", "callers", "slice", "test"
	Symbol  string // symdef, callers
	Path    string // slice
	Start   int    // slice
	N       int    // slice line count, callers top_k
	Pattern string // test
}

// ISARun represents a RUN directive.
type ISARun struct {
	Kind    string // "test", "lint", "typecheck"
	Pattern string // for test kind
}

// ISAAssert represents an ASSERT condition.
type ISAAssert struct {
	Condition string
}

// ISAIfFail represents an IF_FAIL error-handling directive.
type ISAIfFail struct {
	Action string // "RETRY", "ESCALATE", "HALT"
	N      int    // retry count (for RETRY only)
}

// ISAParseError reports a parse error at a specific line.
type ISAParseError struct {
	Line    int
	Message string
}

func (e *ISAParseError) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// ParseISA parses Genesis ISA v0 source text into an ISAProgram AST.
func ParseISA(source string) (*ISAProgram, error) {
	prog := &ISAProgram{}
	lines := strings.Split(source, "\n")
	versionCount := 0
	baseRevCount := 0
	promptRefCount := 0
	modeCount := 0
	budgetCount := 0

	for i, line := range lines {
		lineNum := i + 1

		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		// Strip inline comments (space before # required)
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		parts := strings.SplitN(line, " ", 2)
		keyword := parts[0]
		rest := ""
		if len(parts) > 1 {
			rest = strings.TrimSpace(parts[1])
		}

		switch keyword {
		case "ISA_VERSION":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "ISA_VERSION requires a value"}
			}
			versionCount++
			if versionCount > 1 {
				return nil, &ISAParseError{Line: lineNum, Message: "duplicate ISA_VERSION"}
			}
			v, err := strconv.Atoi(rest)
			if err != nil {
				return nil, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("ISA_VERSION must be an integer, got %q", rest)}
			}
			if v != ISAVersion {
				return nil, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("unsupported ISA_VERSION %d (expected %d)", v, ISAVersion)}
			}
			prog.Version = v

		case "BASE_REV":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "BASE_REV requires a value"}
			}
			baseRevCount++
			if baseRevCount > 1 {
				return nil, &ISAParseError{Line: lineNum, Message: "duplicate BASE_REV"}
			}
			prog.BaseRev = rest

		case "PROMPT_REF":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "PROMPT_REF requires a value"}
			}
			promptRefCount++
			if promptRefCount > 1 {
				return nil, &ISAParseError{Line: lineNum, Message: "duplicate PROMPT_REF"}
			}
			prog.PromptRef = rest

		case "MODE":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "MODE requires a value"}
			}
			modeCount++
			if modeCount > 1 {
				return nil, &ISAParseError{Line: lineNum, Message: "duplicate MODE"}
			}
			if rest != "SOLO" && rest != "SWARM" {
				return nil, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("MODE must be SOLO or SWARM, got %q", rest)}
			}
			prog.Mode = rest

		case "BUDGET":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "BUDGET requires a value"}
			}
			budgetCount++
			if budgetCount > 1 {
				return nil, &ISAParseError{Line: lineNum, Message: "duplicate BUDGET"}
			}
			b, err := strconv.Atoi(rest)
			if err != nil || b <= 0 {
				return nil, &ISAParseError{Line: lineNum, Message: "BUDGET must be a positive integer"}
			}
			prog.Budget = b

		case "INVARIANT":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "INVARIANT requires a value"}
			}
			// Strip surrounding quotes if present
			inv := rest
			if len(inv) >= 2 && inv[0] == '"' && inv[len(inv)-1] == '"' {
				inv = inv[1 : len(inv)-1]
			}
			prog.Invariants = append(prog.Invariants, inv)

		case "NEED":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "NEED requires arguments"}
			}
			need, err := parseISANeed(rest, lineNum)
			if err != nil {
				return nil, err
			}
			prog.Needs = append(prog.Needs, need)

		case "OP":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "OP requires a value"}
			}
			prog.Ops = append(prog.Ops, rest)

		case "RUN":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "RUN requires arguments"}
			}
			run, err := parseISARun(rest, lineNum)
			if err != nil {
				return nil, err
			}
			prog.Runs = append(prog.Runs, run)

		case "ASSERT":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "ASSERT requires a condition"}
			}
			prog.Asserts = append(prog.Asserts, ISAAssert{Condition: rest})

		case "IF_FAIL":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "IF_FAIL requires an action"}
			}
			ifFail, err := parseISAIfFail(rest, lineNum)
			if err != nil {
				return nil, err
			}
			prog.IfFails = append(prog.IfFails, ifFail)

		case "LABEL":
			if rest == "" {
				return nil, &ISAParseError{Line: lineNum, Message: "LABEL requires a name"}
			}
			prog.Labels = append(prog.Labels, rest)

		case "HALT":
			prog.Halt = true

		default:
			return nil, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("unknown directive %q", keyword)}
		}
	}

	// Validate required fields
	if versionCount == 0 {
		return nil, &ISAParseError{Line: 0, Message: "missing required ISA_VERSION"}
	}
	if prog.BaseRev == "" {
		return nil, &ISAParseError{Line: 0, Message: "missing required BASE_REV"}
	}

	// Default mode
	if prog.Mode == "" {
		prog.Mode = "SWARM"
	}

	return prog, nil
}

// parseISANeed parses the arguments of a NEED directive.
func parseISANeed(rest string, lineNum int) (ISANeed, error) {
	fields := strings.Fields(rest)
	kind := fields[0]

	switch kind {
	case "symdef":
		if len(fields) != 2 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED symdef requires exactly 1 argument: symbol"}
		}
		return ISANeed{Kind: "symdef", Symbol: fields[1]}, nil

	case "callers":
		if len(fields) < 2 || len(fields) > 3 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED callers requires 1-2 arguments: symbol [top_k]"}
		}
		need := ISANeed{Kind: "callers", Symbol: fields[1]}
		if len(fields) == 3 {
			n, err := strconv.Atoi(fields[2])
			if err != nil || n <= 0 {
				return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED callers top_k must be a positive integer"}
			}
			need.N = n
		}
		return need, nil

	case "slice":
		if len(fields) != 4 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED slice requires exactly 3 arguments: path start_line line_count"}
		}
		start, err := strconv.Atoi(fields[2])
		if err != nil || start < 0 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED slice start_line must be a non-negative integer"}
		}
		n, err := strconv.Atoi(fields[3])
		if err != nil || n <= 0 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED slice line_count must be a positive integer"}
		}
		return ISANeed{Kind: "slice", Path: fields[1], Start: start, N: n}, nil

	case "test":
		if len(fields) != 2 {
			return ISANeed{}, &ISAParseError{Line: lineNum, Message: "NEED test requires exactly 1 argument: pattern"}
		}
		return ISANeed{Kind: "test", Pattern: fields[1]}, nil

	default:
		return ISANeed{}, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("unknown NEED kind %q (expected symdef, callers, slice, or test)", kind)}
	}
}

// parseISARun parses the arguments of a RUN directive.
func parseISARun(rest string, lineNum int) (ISARun, error) {
	fields := strings.Fields(rest)
	kind := fields[0]

	switch kind {
	case "test":
		if len(fields) != 2 {
			return ISARun{}, &ISAParseError{Line: lineNum, Message: "RUN test requires exactly 1 argument: pattern"}
		}
		return ISARun{Kind: "test", Pattern: fields[1]}, nil

	case "lint":
		return ISARun{Kind: "lint"}, nil

	case "typecheck":
		return ISARun{Kind: "typecheck"}, nil

	default:
		return ISARun{}, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("unknown RUN kind %q (expected test, lint, or typecheck)", kind)}
	}
}

// parseISAIfFail parses the arguments of an IF_FAIL directive.
func parseISAIfFail(rest string, lineNum int) (ISAIfFail, error) {
	fields := strings.Fields(rest)
	action := fields[0]

	switch action {
	case "RETRY":
		if len(fields) != 2 {
			return ISAIfFail{}, &ISAParseError{Line: lineNum, Message: "IF_FAIL RETRY requires exactly 1 argument: count"}
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n <= 0 {
			return ISAIfFail{}, &ISAParseError{Line: lineNum, Message: "IF_FAIL RETRY count must be a positive integer"}
		}
		return ISAIfFail{Action: "RETRY", N: n}, nil

	case "ESCALATE":
		return ISAIfFail{Action: "ESCALATE"}, nil

	case "HALT":
		return ISAIfFail{Action: "HALT"}, nil

	default:
		return ISAIfFail{}, &ISAParseError{Line: lineNum, Message: fmt.Sprintf("unknown IF_FAIL action %q (expected RETRY, ESCALATE, or HALT)", action)}
	}
}
