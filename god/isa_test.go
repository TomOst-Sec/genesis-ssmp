package god

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseISAMinimal(t *testing.T) {
	source := `ISA_VERSION 0
BASE_REV abc123
NEED symdef Foo
OP Add feature
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prog.Version != 0 {
		t.Errorf("version = %d, want 0", prog.Version)
	}
	if prog.BaseRev != "abc123" {
		t.Errorf("base_rev = %q, want %q", prog.BaseRev, "abc123")
	}
	if prog.Mode != "SWARM" {
		t.Errorf("mode = %q, want %q (default)", prog.Mode, "SWARM")
	}
	if len(prog.Needs) != 1 {
		t.Fatalf("needs = %d, want 1", len(prog.Needs))
	}
	if prog.Needs[0].Kind != "symdef" || prog.Needs[0].Symbol != "Foo" {
		t.Errorf("need[0] = %+v, want symdef Foo", prog.Needs[0])
	}
	if len(prog.Ops) != 1 || prog.Ops[0] != "Add feature" {
		t.Errorf("ops = %v, want [Add feature]", prog.Ops)
	}
}

func TestParseISAFull(t *testing.T) {
	fixture, _ := filepath.Abs("../fixtures/sample.isa")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}

	prog, err := ParseISA(string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prog.Version != 0 {
		t.Errorf("version = %d, want 0", prog.Version)
	}
	if prog.BaseRev != "abc123" {
		t.Errorf("base_rev = %q", prog.BaseRev)
	}
	if prog.Mode != "SOLO" {
		t.Errorf("mode = %q, want SOLO", prog.Mode)
	}
	if prog.Budget != 6000 {
		t.Errorf("budget = %d, want 6000", prog.Budget)
	}
	if prog.PromptRef != "sha256-deadbeef" {
		t.Errorf("prompt_ref = %q", prog.PromptRef)
	}
	if len(prog.Needs) != 4 {
		t.Errorf("needs = %d, want 4", len(prog.Needs))
	}
	// Verify NEED test
	testNeeds := 0
	for _, n := range prog.Needs {
		if n.Kind == "test" {
			testNeeds++
			if n.Pattern != "TestHandleCreate" {
				t.Errorf("test pattern = %q", n.Pattern)
			}
		}
	}
	if testNeeds != 1 {
		t.Errorf("test needs = %d, want 1", testNeeds)
	}
	if len(prog.Invariants) != 2 {
		t.Errorf("invariants = %d, want 2", len(prog.Invariants))
	}
	if len(prog.Ops) != 2 {
		t.Errorf("ops = %d, want 2", len(prog.Ops))
	}
	if len(prog.Runs) != 2 {
		t.Errorf("runs = %d, want 2", len(prog.Runs))
	}
	if len(prog.Asserts) != 1 {
		t.Errorf("asserts = %d, want 1", len(prog.Asserts))
	}
	if len(prog.IfFails) != 2 {
		t.Errorf("if_fails = %d, want 2", len(prog.IfFails))
	}
	if prog.IfFails[0].Action != "RETRY" || prog.IfFails[0].N != 2 {
		t.Errorf("if_fail[0] = %+v, want RETRY 2", prog.IfFails[0])
	}
	if prog.IfFails[1].Action != "ESCALATE" {
		t.Errorf("if_fail[1] = %+v, want ESCALATE", prog.IfFails[1])
	}
	if len(prog.Labels) != 1 || prog.Labels[0] != "checkpoint-1" {
		t.Errorf("labels = %v", prog.Labels)
	}
	if !prog.Halt {
		t.Error("halt = false, want true")
	}
}

func TestParseISAComments(t *testing.T) {
	source := `# Full line comment
ISA_VERSION 0
BASE_REV abc123
NEED symdef Foo # inline comment
OP Do something
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prog.Needs) != 1 {
		t.Errorf("needs = %d, want 1", len(prog.Needs))
	}
	if prog.Needs[0].Symbol != "Foo" {
		t.Errorf("need symbol = %q, want Foo (inline comment should be stripped)", prog.Needs[0].Symbol)
	}
}

func TestParseISAErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		errMsg string
	}{
		{
			name:   "missing ISA_VERSION",
			source: "BASE_REV abc\nNEED symdef Foo\nOP Do\n",
			errMsg: "missing required ISA_VERSION",
		},
		{
			name:   "missing BASE_REV",
			source: "ISA_VERSION 0\nNEED symdef Foo\nOP Do\n",
			errMsg: "missing required BASE_REV",
		},
		{
			name:   "unsupported version",
			source: "ISA_VERSION 99\nBASE_REV abc\n",
			errMsg: "unsupported ISA_VERSION",
		},
		{
			name:   "unknown directive",
			source: "ISA_VERSION 0\nBASE_REV abc\nFOOBAR xyz\n",
			errMsg: "unknown directive",
		},
		{
			name:   "invalid MODE",
			source: "ISA_VERSION 0\nBASE_REV abc\nMODE INVALID\n",
			errMsg: "MODE must be SOLO or SWARM",
		},
		{
			name:   "duplicate BASE_REV",
			source: "ISA_VERSION 0\nBASE_REV abc\nBASE_REV def\n",
			errMsg: "duplicate BASE_REV",
		},
		{
			name:   "unknown NEED kind",
			source: "ISA_VERSION 0\nBASE_REV abc\nNEED foobar Baz\n",
			errMsg: "unknown NEED kind",
		},
		{
			name:   "unknown RUN kind",
			source: "ISA_VERSION 0\nBASE_REV abc\nRUN deploy\n",
			errMsg: "unknown RUN kind",
		},
		{
			name:   "unknown IF_FAIL action",
			source: "ISA_VERSION 0\nBASE_REV abc\nIF_FAIL PANIC\n",
			errMsg: "unknown IF_FAIL action",
		},
		{
			name:   "BUDGET not positive",
			source: "ISA_VERSION 0\nBASE_REV abc\nBUDGET -5\n",
			errMsg: "BUDGET must be a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseISA(tt.source)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errMsg)
			}
			if got := err.Error(); !contains(got, tt.errMsg) {
				t.Errorf("error = %q, want to contain %q", got, tt.errMsg)
			}
		})
	}
}

func TestCompileISABasic(t *testing.T) {
	source := `ISA_VERSION 0
BASE_REV abc123
NEED symdef HandleCreate
NEED callers HandleCreate
OP Add ETag validation to HandleCreate
ASSERT tests ./handler/...
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := CompileISA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if result.Mission.MissionID == "" {
		t.Error("mission_id is empty")
	}
	if result.Mission.BaseRev != "abc123" {
		t.Errorf("base_rev = %q", result.Mission.BaseRev)
	}
	if result.Mission.TokenBudget != 8000 {
		t.Errorf("token_budget = %d, want 8000 (default)", result.Mission.TokenBudget)
	}
	if len(result.ShardRequests) != 2 {
		t.Errorf("shard_requests = %d, want 2", len(result.ShardRequests))
	}
	if result.ShardRequests[0].Command != "PF_SYMDEF" {
		t.Errorf("shard[0].command = %q, want PF_SYMDEF", result.ShardRequests[0].Command)
	}
	if result.ShardRequests[1].Command != "PF_CALLERS" {
		t.Errorf("shard[1].command = %q, want PF_CALLERS", result.ShardRequests[1].Command)
	}
	if result.Mode != "SWARM" {
		t.Errorf("mode = %q, want SWARM (default)", result.Mode)
	}
	if len(result.Mission.Scopes) != 1 {
		t.Errorf("scopes = %d, want 1 (deduped symbol:HandleCreate)", len(result.Mission.Scopes))
	}
}

func TestCompileISABudgetOverride(t *testing.T) {
	source := `ISA_VERSION 0
BASE_REV abc123
BUDGET 4000
NEED symdef Foo
OP Do something
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := CompileISA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if result.Mission.TokenBudget != 4000 {
		t.Errorf("token_budget = %d, want 4000", result.Mission.TokenBudget)
	}
}

func TestCompileISAModeSOLO(t *testing.T) {
	source := `ISA_VERSION 0
BASE_REV abc123
MODE SOLO
NEED symdef Bar
OP Fix the bug
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := CompileISA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	if result.Mode != "SOLO" {
		t.Errorf("mode = %q, want SOLO", result.Mode)
	}
}

func TestCompileISANeedTest(t *testing.T) {
	source := `ISA_VERSION 0
BASE_REV abc123
NEED test TestFoo
OP Run tests
`
	prog, err := ParseISA(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	result, err := CompileISA(prog)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Test NEEDs should produce PF_TESTS shard request
	found := false
	for _, sr := range result.ShardRequests {
		if sr.Command == "PF_TESTS" {
			found = true
			if sr.Args.Symbol != "TestFoo" {
				t.Errorf("PF_TESTS symbol = %q, want TestFoo", sr.Args.Symbol)
			}
		}
	}
	if !found {
		t.Errorf("no PF_TESTS shard request found, got %+v", result.ShardRequests)
	}

	// Test-only programs should have wildcard lease fallback
	if len(result.Mission.Scopes) != 1 || result.Mission.Scopes[0].ScopeValue != "*" {
		t.Errorf("scopes = %+v, want wildcard fallback", result.Mission.Scopes)
	}
}

// contains checks if s contains substr (used for error matching).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
