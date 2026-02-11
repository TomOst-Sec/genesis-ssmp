package god

import "fmt"

// ISACompileResult extends CompileResult with ISA-specific fields.
type ISACompileResult struct {
	CompileResult
	Mode       string      `json:"mode"`
	Invariants []string    `json:"invariants,omitempty"`
	Runs       []ISARun    `json:"runs,omitempty"`
	IfFails    []ISAIfFail `json:"if_fails,omitempty"`
	Ops        []string    `json:"ops,omitempty"`
	Labels     []string    `json:"labels,omitempty"`
	PromptRef  string      `json:"prompt_ref,omitempty"`
	Halt       bool        `json:"halt,omitempty"`
}

// CompileISA compiles an ISAProgram into an ISACompileResult.
// It lowers the ISA program to an AAProgram and delegates to CompileAA,
// then enriches the result with ISA-specific fields.
func CompileISA(prog *ISAProgram) (*ISACompileResult, error) {
	aaProg, err := lowerISAToAA(prog)
	if err != nil {
		return nil, fmt.Errorf("isa lower: %w", err)
	}

	cr, err := CompileAA(aaProg)
	if err != nil {
		return nil, fmt.Errorf("isa compile: %w", err)
	}

	// Override budget if specified
	if prog.Budget > 0 {
		cr.Mission.TokenBudget = prog.Budget
	}

	// Add test NEED as shard request
	for _, need := range prog.Needs {
		if need.Kind == "test" {
			cr.ShardRequests = append(cr.ShardRequests, ShardRequest{
				Command: "PF_TESTS",
				Args: ShardRequestArgs{
					MissionID: cr.Mission.MissionID,
					Symbol:    need.Pattern,
				},
			})
		}
	}

	return &ISACompileResult{
		CompileResult: *cr,
		Mode:          prog.Mode,
		Invariants:    prog.Invariants,
		Runs:          prog.Runs,
		IfFails:       prog.IfFails,
		Ops:           prog.Ops,
		Labels:        prog.Labels,
		PromptRef:     prog.PromptRef,
		Halt:          prog.Halt,
	}, nil
}

// lowerISAToAA converts an ISAProgram to an AAProgram for compilation
// through the existing AA pipeline.
func lowerISAToAA(isa *ISAProgram) (*AAProgram, error) {
	prog := &AAProgram{
		BaseRev: isa.BaseRev,
		Return:  "edit_ir",
	}

	// Derive leases from NEEDs
	seen := map[string]bool{}
	for _, need := range isa.Needs {
		var scope Scope
		switch need.Kind {
		case "symdef", "callers":
			scope = Scope{ScopeType: "symbol", ScopeValue: need.Symbol}
		case "slice":
			scope = Scope{ScopeType: "file", ScopeValue: need.Path}
		case "test":
			continue // tests don't require leases
		}
		key := scope.ScopeType + ":" + scope.ScopeValue
		if !seen[key] {
			prog.Leases = append(prog.Leases, scope)
			seen[key] = true
		}
	}

	// Wildcard fallback if no leases derived
	if len(prog.Leases) == 0 {
		prog.Leases = []Scope{{ScopeType: "file", ScopeValue: "*"}}
	}

	// Convert ISANeeds to AANeeds (excluding test, handled post-compile)
	for _, need := range isa.Needs {
		switch need.Kind {
		case "symdef", "callers", "slice":
			prog.Needs = append(prog.Needs, AANeed{
				Kind:   need.Kind,
				Symbol: need.Symbol,
				Path:   need.Path,
				Start:  need.Start,
				N:      need.N,
			})
		}
	}

	// OPs become DOs
	if len(isa.Ops) > 0 {
		prog.Dos = isa.Ops
	} else if len(isa.Invariants) > 0 {
		prog.Dos = isa.Invariants
	} else {
		prog.Dos = []string{"execute ISA program"}
	}

	// Map ISA asserts to AA asserts
	for _, a := range isa.Asserts {
		prog.Asserts = append(prog.Asserts, AAAssert{Kind: "tests", Selector: a.Condition})
	}

	return prog, nil
}
