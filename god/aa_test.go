package god

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Parser tests
// ---------------------------------------------------------------------------

func TestParseAAMinimal(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
DO Refactor Greet
RETURN edit_ir
`
	prog, err := ParseAA(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog.BaseRev != "abc123" {
		t.Errorf("BaseRev = %q, want %q", prog.BaseRev, "abc123")
	}
	if len(prog.Leases) != 1 || prog.Leases[0].ScopeType != "symbol" || prog.Leases[0].ScopeValue != "Greet" {
		t.Errorf("Leases = %+v, want [{symbol Greet}]", prog.Leases)
	}
	if len(prog.Dos) != 1 || prog.Dos[0] != "Refactor Greet" {
		t.Errorf("Dos = %v, want [Refactor Greet]", prog.Dos)
	}
	if prog.Return != "edit_ir" {
		t.Errorf("Return = %q, want %q", prog.Return, "edit_ir")
	}
	if len(prog.Needs) != 0 {
		t.Errorf("Needs = %v, want empty", prog.Needs)
	}
	if len(prog.Asserts) != 0 {
		t.Errorf("Asserts = %v, want empty", prog.Asserts)
	}
}

func TestParseAAFull(t *testing.T) {
	src := `
# Full example
BASE_REV abc123
LEASE symbol:Greet file:main.go
NEED symdef Greet
NEED callers Greet 10
NEED slice main.go 10 20
DO Refactor the Greet function
DO Update all callers
ASSERT tests TestGreet
RETURN edit_ir
`
	prog, err := ParseAA(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog.BaseRev != "abc123" {
		t.Errorf("BaseRev = %q", prog.BaseRev)
	}
	if len(prog.Leases) != 2 {
		t.Fatalf("Leases count = %d, want 2", len(prog.Leases))
	}
	if prog.Leases[0].ScopeType != "symbol" || prog.Leases[1].ScopeType != "file" {
		t.Errorf("Lease types = %s, %s", prog.Leases[0].ScopeType, prog.Leases[1].ScopeType)
	}
	if len(prog.Needs) != 3 {
		t.Fatalf("Needs count = %d, want 3", len(prog.Needs))
	}
	if prog.Needs[0].Kind != "symdef" || prog.Needs[0].Symbol != "Greet" {
		t.Errorf("Need[0] = %+v", prog.Needs[0])
	}
	if prog.Needs[1].Kind != "callers" || prog.Needs[1].N != 10 {
		t.Errorf("Need[1] = %+v", prog.Needs[1])
	}
	if prog.Needs[2].Kind != "slice" || prog.Needs[2].Path != "main.go" || prog.Needs[2].Start != 10 || prog.Needs[2].N != 20 {
		t.Errorf("Need[2] = %+v", prog.Needs[2])
	}
	if len(prog.Dos) != 2 {
		t.Fatalf("Dos count = %d, want 2", len(prog.Dos))
	}
	if len(prog.Asserts) != 1 || prog.Asserts[0].Kind != "tests" || prog.Asserts[0].Selector != "TestGreet" {
		t.Errorf("Asserts = %+v", prog.Asserts)
	}
	if prog.Return != "edit_ir" {
		t.Errorf("Return = %q", prog.Return)
	}
}

func TestParseAACommentsAndBlanks(t *testing.T) {
	src := `
# This is a comment

BASE_REV abc123

# Another comment
LEASE symbol:Greet

DO Do the thing  # inline comment

RETURN edit_ir
`
	prog, err := ParseAA(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog.Dos[0] != "Do the thing" {
		t.Errorf("DO = %q, want %q (inline comment should be stripped)", prog.Dos[0], "Do the thing")
	}
}

func TestParseAAMissingBaseRev(t *testing.T) {
	src := `
LEASE symbol:Greet
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for missing BASE_REV")
	}
	pe, ok := err.(*AAParseError)
	if !ok {
		t.Fatalf("expected *AAParseError, got %T", err)
	}
	if !strings.Contains(pe.Message, "BASE_REV") {
		t.Errorf("error message %q should mention BASE_REV", pe.Message)
	}
}

func TestParseAAMissingLease(t *testing.T) {
	src := `
BASE_REV abc123
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for missing LEASE")
	}
	if !strings.Contains(err.Error(), "LEASE") {
		t.Errorf("error %q should mention LEASE", err.Error())
	}
}

func TestParseAAMissingDO(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for missing DO")
	}
	if !strings.Contains(err.Error(), "DO") {
		t.Errorf("error %q should mention DO", err.Error())
	}
}

func TestParseAAMissingReturn(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
DO Do it
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for missing RETURN")
	}
	if !strings.Contains(err.Error(), "RETURN") {
		t.Errorf("error %q should mention RETURN", err.Error())
	}
}

func TestParseAADuplicateBaseRev(t *testing.T) {
	src := `
BASE_REV abc123
BASE_REV def456
LEASE symbol:Greet
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for duplicate BASE_REV")
	}
	if !strings.Contains(err.Error(), "duplicate BASE_REV") {
		t.Errorf("error %q should mention duplicate", err.Error())
	}
}

func TestParseAAInvalidLeaseFormat(t *testing.T) {
	src := `
BASE_REV abc123
LEASE nocolon
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for invalid LEASE format")
	}
	if !strings.Contains(err.Error(), "invalid LEASE format") {
		t.Errorf("error %q should mention invalid format", err.Error())
	}
}

func TestParseAAInvalidNeedKind(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
NEED unknown Foo
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for invalid NEED kind")
	}
	if !strings.Contains(err.Error(), "unknown NEED kind") {
		t.Errorf("error %q should mention unknown kind", err.Error())
	}
}

func TestParseAANeedSymdefArgs(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
NEED symdef
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for symdef with no args")
	}
}

func TestParseAANeedSliceArgs(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"too few", "NEED slice main.go 10"},
		{"bad start", "NEED slice main.go abc 20"},
		{"bad count", "NEED slice main.go 10 -5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "BASE_REV abc\nLEASE symbol:X\n" + tc.line + "\nDO Do it\nRETURN edit_ir"
			_, err := ParseAA(src)
			if err == nil {
				t.Fatalf("expected error for %q", tc.line)
			}
		})
	}
}

func TestParseAAUnknownDirective(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
BOGUS something
DO Do it
RETURN edit_ir
`
	_, err := ParseAA(src)
	if err == nil {
		t.Fatal("expected error for unknown directive")
	}
	if !strings.Contains(err.Error(), "unknown directive") {
		t.Errorf("error %q should mention unknown directive", err.Error())
	}
}

func TestParseAACallersWithoutTopK(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet
NEED callers Greet
DO Do it
RETURN edit_ir
`
	prog, err := ParseAA(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog.Needs[0].N != 0 {
		t.Errorf("callers without top_k should have N=0, got %d", prog.Needs[0].N)
	}
}

// ---------------------------------------------------------------------------
// Compiler tests
// ---------------------------------------------------------------------------

func TestCompileAAMinimal(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc123",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "Greet"}},
		Dos:     []string{"Refactor Greet"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.Mission
	if m.MissionID == "" {
		t.Error("MissionID should not be empty")
	}
	if len(m.MissionID) != 32 {
		t.Errorf("MissionID length = %d, want 32 hex chars", len(m.MissionID))
	}
	if m.Goal != "Refactor Greet" {
		t.Errorf("Goal = %q, want %q", m.Goal, "Refactor Greet")
	}
	if m.BaseRev != "abc123" {
		t.Errorf("BaseRev = %q", m.BaseRev)
	}
	if len(m.Tasks) != 1 || m.Tasks[0] != "Refactor Greet" {
		t.Errorf("Tasks = %v", m.Tasks)
	}
	if m.TokenBudget != 8000 {
		t.Errorf("TokenBudget = %d, want 8000", m.TokenBudget)
	}
	if len(m.Scopes) != 1 {
		t.Errorf("Scopes = %v", m.Scopes)
	}
	if len(m.LeaseIDs) != 0 {
		t.Errorf("LeaseIDs should be empty, got %v", m.LeaseIDs)
	}
	if len(result.ShardRequests) != 0 {
		t.Errorf("ShardRequests should be empty, got %v", result.ShardRequests)
	}
}

func TestCompileAAGoalJoinsMultipleDOs(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "X"}},
		Dos:     []string{"First task", "Second task", "Third task"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "First task; Second task; Third task"
	if result.Mission.Goal != want {
		t.Errorf("Goal = %q, want %q", result.Mission.Goal, want)
	}
	if len(result.Mission.Tasks) != 3 {
		t.Errorf("Tasks count = %d, want 3", len(result.Mission.Tasks))
	}
}

func TestCompileAAShardRequestSymdef(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "X"}},
		Needs:   []AANeed{{Kind: "symdef", Symbol: "Greet"}},
		Dos:     []string{"Do it"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ShardRequests) != 1 {
		t.Fatalf("ShardRequests count = %d, want 1", len(result.ShardRequests))
	}
	sr := result.ShardRequests[0]
	if sr.Command != "PF_SYMDEF" {
		t.Errorf("Command = %q, want PF_SYMDEF", sr.Command)
	}
	if sr.Args.Symbol != "Greet" {
		t.Errorf("Args.Symbol = %q, want Greet", sr.Args.Symbol)
	}
	if sr.Args.MissionID != result.Mission.MissionID {
		t.Error("shard request mission_id should match mission")
	}
}

func TestCompileAAShardRequestCallers(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "X"}},
		Needs:   []AANeed{{Kind: "callers", Symbol: "Greet", N: 10}},
		Dos:     []string{"Do it"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sr := result.ShardRequests[0]
	if sr.Command != "PF_CALLERS" {
		t.Errorf("Command = %q, want PF_CALLERS", sr.Command)
	}
	if sr.Args.Symbol != "Greet" {
		t.Errorf("Args.Symbol = %q", sr.Args.Symbol)
	}
	if sr.Args.TopK != 10 {
		t.Errorf("Args.TopK = %d, want 10", sr.Args.TopK)
	}
}

func TestCompileAAShardRequestSlice(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "X"}},
		Needs:   []AANeed{{Kind: "slice", Path: "main.go", Start: 10, N: 20}},
		Dos:     []string{"Do it"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sr := result.ShardRequests[0]
	if sr.Command != "PF_SLICE" {
		t.Errorf("Command = %q, want PF_SLICE", sr.Command)
	}
	if sr.Args.Path != "main.go" {
		t.Errorf("Args.Path = %q", sr.Args.Path)
	}
	if sr.Args.StartLine != 10 {
		t.Errorf("Args.StartLine = %d", sr.Args.StartLine)
	}
	if sr.Args.N != 20 {
		t.Errorf("Args.N = %d", sr.Args.N)
	}
}

func TestCompileAADefaultTokenBudget(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases:  []Scope{{ScopeType: "symbol", ScopeValue: "X"}},
		Dos:     []string{"Do it"},
		Return:  "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mission.TokenBudget != 8000 {
		t.Errorf("TokenBudget = %d, want 8000", result.Mission.TokenBudget)
	}
}

func TestCompileAAScopesFromLeases(t *testing.T) {
	prog := &AAProgram{
		BaseRev: "abc",
		Leases: []Scope{
			{ScopeType: "symbol", ScopeValue: "Greet"},
			{ScopeType: "file", ScopeValue: "main.go"},
		},
		Dos:    []string{"Do it"},
		Return: "edit_ir",
	}
	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Mission.Scopes) != 2 {
		t.Fatalf("Scopes count = %d, want 2", len(result.Mission.Scopes))
	}
	if result.Mission.Scopes[0].ScopeType != "symbol" || result.Mission.Scopes[0].ScopeValue != "Greet" {
		t.Errorf("Scope[0] = %+v", result.Mission.Scopes[0])
	}
	if result.Mission.Scopes[1].ScopeType != "file" || result.Mission.Scopes[1].ScopeValue != "main.go" {
		t.Errorf("Scope[1] = %+v", result.Mission.Scopes[1])
	}
}

// ---------------------------------------------------------------------------
// Validator tests
// ---------------------------------------------------------------------------

func TestValidateMissionValid(t *testing.T) {
	m := Mission{
		MissionID:   "abc123",
		BaseRev:     "def456",
		Goal:        "Do something",
		Tasks:       []string{"task1"},
		TokenBudget: 8000,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := ValidateMission(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMissionEmptyFields(t *testing.T) {
	tests := []struct {
		name    string
		mission Mission
		errStr  string
	}{
		{
			name:    "empty mission_id",
			mission: Mission{BaseRev: "x", Goal: "g", Tasks: []string{"t"}, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			errStr:  "mission_id",
		},
		{
			name:    "empty base_rev",
			mission: Mission{MissionID: "x", Goal: "g", Tasks: []string{"t"}, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			errStr:  "base_rev",
		},
		{
			name:    "empty goal",
			mission: Mission{MissionID: "x", BaseRev: "b", Tasks: []string{"t"}, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			errStr:  "goal",
		},
		{
			name:    "nil tasks",
			mission: Mission{MissionID: "x", BaseRev: "b", Goal: "g", Tasks: nil, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			errStr:  "tasks",
		},
		{
			name:    "empty tasks",
			mission: Mission{MissionID: "x", BaseRev: "b", Goal: "g", Tasks: []string{}, CreatedAt: time.Now().UTC().Format(time.RFC3339)},
			errStr:  "tasks",
		},
		{
			name:    "empty created_at",
			mission: Mission{MissionID: "x", BaseRev: "b", Goal: "g", Tasks: []string{"t"}},
			errStr:  "created_at",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMission(tc.mission)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.errStr) {
				t.Errorf("error %q should mention %q", err.Error(), tc.errStr)
			}
		})
	}
}

func TestValidateMissionNegativeBudget(t *testing.T) {
	m := Mission{
		MissionID:   "x",
		BaseRev:     "b",
		Goal:        "g",
		Tasks:       []string{"t"},
		TokenBudget: -1,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	err := ValidateMission(m)
	if err == nil {
		t.Fatal("expected error for negative budget")
	}
	if !strings.Contains(err.Error(), "token_budget") {
		t.Errorf("error %q should mention token_budget", err.Error())
	}
}

func TestValidateMissionBadTimestamp(t *testing.T) {
	m := Mission{
		MissionID:   "x",
		BaseRev:     "b",
		Goal:        "g",
		Tasks:       []string{"t"},
		TokenBudget: 100,
		CreatedAt:   "not-a-timestamp",
	}
	err := ValidateMission(m)
	if err == nil {
		t.Fatal("expected error for bad timestamp")
	}
	if !strings.Contains(err.Error(), "RFC3339") {
		t.Errorf("error %q should mention RFC3339", err.Error())
	}
}

func TestValidateMissionZeroBudget(t *testing.T) {
	m := Mission{
		MissionID:   "x",
		BaseRev:     "b",
		Goal:        "g",
		Tasks:       []string{"t"},
		TokenBudget: 0,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := ValidateMission(m); err != nil {
		t.Fatalf("zero budget should be valid, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Fixture round-trip tests: Parse → Compile → Validate
// ---------------------------------------------------------------------------

func fixturesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "fixtures")
}

func TestRoundTripRefactorAA(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixturesDir(), "refactor.aa"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	prog, err := ParseAA(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog.BaseRev != "abc123" {
		t.Errorf("BaseRev = %q", prog.BaseRev)
	}
	if len(prog.Needs) != 3 {
		t.Errorf("Needs count = %d, want 3", len(prog.Needs))
	}

	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(result.ShardRequests) != 3 {
		t.Errorf("ShardRequests count = %d, want 3", len(result.ShardRequests))
	}

	if err := ValidateMission(result.Mission); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestRoundTripBugfixAA(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(fixturesDir(), "bugfix.aa"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	prog, err := ParseAA(string(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if prog.BaseRev != "def456" {
		t.Errorf("BaseRev = %q", prog.BaseRev)
	}
	if len(prog.Asserts) != 1 {
		t.Errorf("Asserts count = %d, want 1", len(prog.Asserts))
	}
	if prog.Asserts[0].Kind != "tests" || prog.Asserts[0].Selector != "TestParseConfig" {
		t.Errorf("Assert = %+v", prog.Asserts[0])
	}

	result, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(result.ShardRequests) != 2 {
		t.Errorf("ShardRequests count = %d, want 2", len(result.ShardRequests))
	}

	if err := ValidateMission(result.Mission); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
