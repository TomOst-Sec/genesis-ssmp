package god

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExecutionMode controls whether God runs in swarm (multi-mission DAG) or
// solo (single mission, paging loop) mode.
type ExecutionMode string

const (
	ModeSwarm ExecutionMode = "swarm"
	ModeSolo  ExecutionMode = "solo"
)

// SoloConfig holds configuration for solo execution mode.
type SoloConfig struct {
	TokenBudget   int  // max tokens per mission pack (default: 8000)
	MaxPFCalls    int  // max PF paging calls per session (default: 10)
	MaxTurns      int  // max LLM turns (default: 3)
	StrictEditIR  bool // reject non-Edit-IR output (default: true)
}

// DefaultSoloConfig returns sensible defaults for solo mode.
func DefaultSoloConfig() SoloConfig {
	return SoloConfig{
		TokenBudget:  8000,
		MaxPFCalls:   10,
		MaxTurns:     3,
		StrictEditIR: true,
	}
}

// SoloMission is a single comprehensive mission for solo mode.
// Unlike swarm missions, it covers the entire task in one shot.
type SoloMission struct {
	Mission       Mission          `json:"mission"`
	OwnerID       string           `json:"owner_id"`       // lease owner for integration
	WorkingSet    []string         `json:"working_set"`    // recently touched symbol names
	PFPlaybook    string           `json:"pf_playbook"`    // instructions for PF usage
	TestTargets   []string         `json:"test_targets"`   // recommended tests to run
	Constraints   []string         `json:"constraints"`    // hard constraints
	PromptRef     *PromptRef       `json:"prompt_ref,omitempty"`
}

// SoloResult captures the outcome of a solo execution.
type SoloResult struct {
	Success              bool             `json:"success"`
	Mission              Mission          `json:"mission"`
	TokensIn             int              `json:"tokens_in"`
	TokensOut            int              `json:"tokens_out"`
	CacheReadTokens      int              `json:"cache_read_tokens"`
	CacheCreationTokens  int              `json:"cache_creation_tokens"`
	CostUSD              float64          `json:"cost_usd"`
	Calls                int              `json:"calls"`
	PFCalls              int              `json:"pf_calls"`
	Turns                int              `json:"turns"`
	CLIDurationMS        int64            `json:"cli_duration_ms"`
	FilesModified        []string         `json:"files_modified,omitempty"`
	FilesCreated         []string         `json:"files_created,omitempty"`
	Phases               []PhaseResult    `json:"phases,omitempty"`
	Error                string           `json:"error,omitempty"`
}

// SoloPlanner creates a single comprehensive mission for solo mode.
type SoloPlanner struct {
	heaven *HeavenClient
	config SoloConfig
}

// NewSoloPlanner creates a SoloPlanner.
func NewSoloPlanner(heaven *HeavenClient, config SoloConfig) *SoloPlanner {
	return &SoloPlanner{heaven: heaven, config: config}
}

// Plan creates a single SoloMission from a task description.
// Unlike the swarm planner which creates a DAG, this emits exactly one mission.
func (sp *SoloPlanner) Plan(taskDesc, repoPath string) (*SoloMission, error) {
	// Step 1: Build IR index
	if _, err := sp.heaven.IRBuild(repoPath); err != nil {
		return nil, fmt.Errorf("solo plan: ir build: %w", err)
	}

	// Step 2: Gather all relevant symbols
	keywords := extractKeywords(taskDesc)
	symbols := sp.gatherAllSymbols(keywords)

	// Step 2b: If no keywords matched (vague prompt like "Improve this project"),
	// do a broad IR scan to discover what's in the codebase.
	if len(symbols) == 0 {
		symbols = sp.broadScan()
	}

	// Step 3: Build scopes from symbols (deduped)
	scopes, symbolNames := sp.buildScopes(symbols)

	// Step 4: Acquire leases (skip if no scopes — vague task with empty repo)
	missionID := genID()
	planID := genID()
	var leaseIDs []string
	if len(scopes) > 0 {
		leaseResult, err := sp.heaven.LeaseAcquire("god-"+planID, missionID, scopes)
		if err != nil {
			return nil, fmt.Errorf("solo plan: lease acquire: %w", err)
		}
		leaseIDs = make([]string, len(leaseResult.Acquired))
		for i, l := range leaseResult.Acquired {
			leaseIDs[i] = l.LeaseID
		}
	}

	// Step 5: Identify test targets
	testTargets := sp.findTestTargets(symbols)

	// Step 6: Build PF playbook
	playbook := buildPFPlaybook(sp.config.MaxPFCalls)

	// Step 7: Build constraints
	constraints := []string{
		"Output MUST be Edit IR JSON only (no full file dumps, no diffs)",
		"Each edit op MUST include anchor_hash for verification",
		fmt.Sprintf("Token budget: %d tokens — request only needed context via PF", sp.config.TokenBudget),
		"Do not guess file contents — use PF_SLICE to read before editing",
	}

	now := nowFunc().UTC().Format(time.RFC3339)
	mission := Mission{
		MissionID:   missionID,
		Goal:        taskDesc,
		BaseRev:     "HEAD",
		Scopes:      scopes,
		LeaseIDs:    leaseIDs,
		Tasks:       []string{taskDesc},
		TokenBudget: sp.config.TokenBudget,
		CreatedAt:   now,
	}

	sp.heaven.AppendEvent(map[string]any{
		"type":       "solo_mission_created",
		"mission_id": missionID,
		"plan_id":    planID,
		"goal":       taskDesc,
		"scopes":     len(scopes),
		"mode":       "solo",
	})

	return &SoloMission{
		Mission:     mission,
		OwnerID:     "god-" + planID,
		WorkingSet:  symbolNames,
		PFPlaybook:  playbook,
		TestTargets: testTargets,
		Constraints: constraints,
	}, nil
}

func (sp *SoloPlanner) gatherAllSymbols(keywords []string) []SymbolResult {
	var all []SymbolResult
	seen := make(map[int64]bool)
	for _, kw := range keywords {
		syms, err := sp.heaven.IRSearch(kw, 20) // broader search for solo
		if err != nil {
			continue
		}
		for _, s := range syms {
			if !seen[s.ID] {
				seen[s.ID] = true
				all = append(all, s)
			}
		}
	}
	return all
}

// broadScan discovers top-level symbols when the task is too vague for keyword extraction.
// It searches for common symbol patterns to build an overview of the codebase.
func (sp *SoloPlanner) broadScan() []SymbolResult {
	// Search with broad queries that match common code patterns
	probes := []string{"main", "test", "init", "run", "handle", "parse", "match", "get", "set", "new"}
	var all []SymbolResult
	seen := make(map[int64]bool)
	for _, probe := range probes {
		syms, err := sp.heaven.IRSearch(probe, 10)
		if err != nil {
			continue
		}
		for _, s := range syms {
			if !seen[s.ID] {
				seen[s.ID] = true
				all = append(all, s)
			}
		}
		if len(all) >= 20 {
			break // enough for a broad scope
		}
	}
	return all
}

func (sp *SoloPlanner) buildScopes(symbols []SymbolResult) ([]Scope, []string) {
	seen := make(map[string]bool)
	var scopes []Scope
	var names []string
	for _, s := range symbols {
		// Symbol scope
		key := scopeTypeFor(s.Kind) + ":" + s.Name
		if !seen[key] {
			seen[key] = true
			scopes = append(scopes, Scope{
				ScopeType:  scopeTypeFor(s.Kind),
				ScopeValue: s.Name,
			})
			names = append(names, s.Name)
		}
		// File scope — from symbol's file path
		fileKey := "file:" + s.Path
		if s.Path != "" && !seen[fileKey] {
			seen[fileKey] = true
			scopes = append(scopes, Scope{ScopeType: "file", ScopeValue: s.Path})
		}
	}
	return scopes, names
}

func (sp *SoloPlanner) findTestTargets(symbols []SymbolResult) []string {
	seen := make(map[string]bool)
	var targets []string
	for _, s := range symbols {
		testFile := strings.TrimSuffix(s.Path, ".go") + "_test.go"
		if !seen[testFile] {
			seen[testFile] = true
			targets = append(targets, testFile)
		}
	}
	return targets
}

func buildPFPlaybook(maxCalls int) string {
	return fmt.Sprintf(`PF PLAYBOOK (Context Paging):
- You have %d PF calls available. Use them wisely.
- PF_SYMDEF <symbol>: get definition of a symbol
- PF_CALLERS <symbol>: get call sites referencing a symbol
- PF_SLICE <path> <start> <n>: get n lines of a file starting at line start
- PF_TESTS <symbol>: get tests covering a symbol
- PF_SEARCH <query>: search for symbols by name
- DO NOT guess file contents. Use PF_SLICE first, then edit.
- DO NOT request entire files. Request only the relevant span (30-50 lines).
- Prefer PF_SYMDEF + PF_CALLERS before editing a symbol.`, maxCalls)
}

// SoloPacker compiles a MissionPack optimized for single-agent mode.
// Biases shard scoring toward interface contracts, test coverage, and
// minimal file skeletons.
type SoloPacker struct {
	compiler *PromptCompiler
	config   SoloConfig
}

// NewSoloPacker creates a SoloPacker.
func NewSoloPacker(pfEndpoint string, config SoloConfig) *SoloPacker {
	return &SoloPacker{
		compiler: NewPromptCompiler(pfEndpoint),
		config:   config,
	}
}

// Pack compiles a MissionPack for a solo mission, including the PF playbook
// in the header and biasing shard selection toward contracts and tests.
func (sp *SoloPacker) Pack(sm *SoloMission, candidates []CandidateShard) (*MissionPack, error) {
	// Bias candidates for solo mode: boost test-relevant and interface shards
	biased := sp.biasCandidates(candidates)

	var pack *MissionPack
	var err error

	if sm.PromptRef != nil {
		// Use PromptRef-aware compilation: inline only pinned sections
		// For solo mode, always pin constraints + acceptance
		pack, err = sp.compiler.CompileWithPromptRef(sm.Mission, biased, sm.PromptRef)
	} else {
		pack, err = sp.compiler.Compile(sm.Mission, biased)
	}
	if err != nil {
		return nil, err
	}

	// Append PF playbook and constraints to header
	pack.Header += "\n\n" + sm.PFPlaybook

	// If PromptRef is set, add section index to PF playbook
	if sm.PromptRef != nil {
		pack.Header += "\n\nPROMPT SECTIONS AVAILABLE VIA PF:\n"
		for i := 0; i < sm.PromptRef.TotalSections; i++ {
			pinned := false
			for _, p := range sm.PromptRef.PinnedSections {
				if p == i {
					pinned = true
					break
				}
			}
			if pinned {
				pack.Header += fmt.Sprintf("  [%d] (INLINED)\n", i)
			} else {
				pack.Header += fmt.Sprintf("  [%d] — use PF_PROMPT_SECTION(%s, %d)\n", i, sm.PromptRef.PromptID, i)
			}
		}
	}

	pack.Header += "\n\nCONSTRAINTS:\n"
	for i, c := range sm.Constraints {
		pack.Header += fmt.Sprintf("%d. %s\n", i+1, c)
	}
	if len(sm.TestTargets) > 0 {
		pack.Header += "\nTEST TARGETS: " + strings.Join(sm.TestTargets, ", ")
	}

	return pack, nil
}

// biasCandidates adjusts shard metadata to favor solo-mode-useful shards.
func (sp *SoloPacker) biasCandidates(candidates []CandidateShard) []CandidateShard {
	biased := make([]CandidateShard, len(candidates))
	copy(biased, candidates)
	for i := range biased {
		// Boost test-related shards
		if strings.Contains(biased[i].Path, "_test") ||
			strings.Contains(biased[i].Path, "test_") {
			biased[i].TestRelevant = true
		}
		// Boost interface/type shards
		if biased[i].Kind == "symdef" {
			biased[i].HotsetHit = true // treat all symdefs as hot in solo mode
		}
	}
	return biased
}

// SoloExecutor runs a full solo execution loop: plan -> pack -> send -> integrate.
type SoloExecutor struct {
	heaven     *HeavenClient
	planner    *SoloPlanner
	packer     *SoloPacker
	provider   Provider
	integrator *Integrator
	config     SoloConfig
}

// NewSoloExecutor creates a SoloExecutor with all required components.
func NewSoloExecutor(heaven *HeavenClient, provider Provider, config SoloConfig) *SoloExecutor {
	return &SoloExecutor{
		heaven:     heaven,
		planner:    NewSoloPlanner(heaven, config),
		packer:     NewSoloPacker(heaven.BaseURL+"/pf", config),
		provider:   provider,
		integrator: NewIntegrator(heaven),
		config:     config,
	}
}

// Execute runs the full solo pipeline on a task.
func (se *SoloExecutor) Execute(taskDesc, repoPath string) (*SoloResult, error) {
	result := &SoloResult{}

	// 1. Plan
	sm, err := se.planner.Plan(taskDesc, repoPath)
	if err != nil {
		return nil, fmt.Errorf("solo execute: plan: %w", err)
	}
	result.Mission = sm.Mission

	// 2. Build initial candidates from scopes
	candidates := se.buildCandidates(sm)

	// 3. Pack
	pack, err := se.packer.Pack(sm, candidates)
	if err != nil {
		return nil, fmt.Errorf("solo execute: pack: %w", err)
	}

	packJSON, _ := json.Marshal(pack)
	result.TokensIn += pack.BudgetMeta.TotalTokens
	result.Calls++
	result.Turns++

	// 4. Send to provider (with output enforcement)
	adapter := NewProviderAdapter(se.provider)
	angelResp, usage, err := adapter.Execute(pack)
	if err != nil {
		result.Error = err.Error()
		result.Success = false
		// Still capture real usage if provider supports it
		if up, ok := se.provider.(CLIUsageProvider); ok {
			if cu := up.CLIUsage(); cu != nil {
				result.TokensIn = int(cu.InputTokens + cu.CacheReadInputTokens + cu.CacheCreationInputTokens)
				result.TokensOut = int(cu.OutputTokens)
				result.CacheReadTokens = int(cu.CacheReadInputTokens)
				result.CacheCreationTokens = int(cu.CacheCreationInputTokens)
				result.CostUSD = cu.TotalCostUSD
				result.Turns = cu.NumTurns
				result.CLIDurationMS = cu.DurationMS
			}
		}
		return result, nil
	}
	result.TokensOut += EstimateTokens([]byte(fmt.Sprintf("%v", usage.ResponseBytes)))

	// Override with real token data if provider supports it
	if up, ok := se.provider.(CLIUsageProvider); ok {
		if cu := up.CLIUsage(); cu != nil {
			result.TokensIn = int(cu.InputTokens + cu.CacheReadInputTokens + cu.CacheCreationInputTokens)
			result.TokensOut = int(cu.OutputTokens)
			result.CacheReadTokens = int(cu.CacheReadInputTokens)
			result.CacheCreationTokens = int(cu.CacheCreationInputTokens)
			result.CostUSD = cu.TotalCostUSD
			result.Turns = cu.NumTurns
			result.CLIDurationMS = cu.DurationMS
		}
	}

	// 5. Enforce Edit IR output
	if se.config.StrictEditIR && angelResp.OutputType != "edit_ir" {
		result.Error = fmt.Sprintf("output enforcement: expected edit_ir, got %s", angelResp.OutputType)
		result.Success = false
		return result, nil
	}

	// 5.5 Acquire supplemental leases for files/symbols Angel touched but weren't in original scopes
	{
		var extraScopes []Scope
		for _, f := range angelResp.Manifest.FilesTouched {
			extraScopes = append(extraScopes, Scope{ScopeType: "file", ScopeValue: f})
		}
		for _, s := range angelResp.Manifest.SymbolsTouched {
			extraScopes = append(extraScopes, Scope{ScopeType: "symbol", ScopeValue: s})
		}
		if len(extraScopes) > 0 {
			se.heaven.LeaseAcquire(sm.OwnerID, sm.Mission.MissionID, extraScopes)
		}
	}

	// 6. Integrate (skip lease check in solo mode — supplemental leases may not cover everything)
	clocks, _ := se.heaven.FileClockGet(angelResp.Manifest.FilesTouched)
	intResult, err := se.integrator.Integrate(IntegrateRequest{
		OwnerID:        sm.OwnerID,
		RepoRoot:       repoPath,
		Response:       angelResp,
		Mission:        sm.Mission,
		FileClocks:     clocks,
		SkipLeaseCheck: true,
	})
	if err != nil {
		result.Error = err.Error()
		result.Success = false
		return result, nil
	}

	result.Success = intResult.Success
	result.FilesModified = intResult.FilesModified
	result.FilesCreated = intResult.FilesCreated
	if !intResult.Success {
		result.Error = intResult.Error
	}

	_ = packJSON // used for size accounting above
	return result, nil
}

// ---------------------------------------------------------------------------
// Solo micro-phases
// ---------------------------------------------------------------------------

// SoloPhase represents a micro-execution phase in solo mode.
type SoloPhase string

const (
	PhaseUnderstand SoloPhase = "understand"
	PhasePlan       SoloPhase = "plan"
	PhaseExecute    SoloPhase = "execute"
	PhaseVerify     SoloPhase = "verify"
)

// SoloPhaseOrder is the canonical execution order for solo phases.
var SoloPhaseOrder = []SoloPhase{PhaseUnderstand, PhasePlan, PhaseExecute, PhaseVerify}

// PhaseConfig holds per-phase execution parameters.
type PhaseConfig struct {
	TokenFrac  float64 // fraction of total budget (e.g., 0.15 = 15%)
	MaxPFCalls int     // max PF calls for this phase
	AllowEdits bool    // whether Edit IR output is expected
	IsVerify   bool    // whether this is the verification phase
}

// DefaultPhaseConfigs returns the standard 4-phase configuration.
func DefaultPhaseConfigs() map[SoloPhase]PhaseConfig {
	return map[SoloPhase]PhaseConfig{
		PhaseUnderstand: {TokenFrac: 0.15, MaxPFCalls: 5, AllowEdits: false, IsVerify: false},
		PhasePlan:       {TokenFrac: 0.10, MaxPFCalls: 2, AllowEdits: false, IsVerify: false},
		PhaseExecute:    {TokenFrac: 0.60, MaxPFCalls: 0, AllowEdits: true, IsVerify: false},
		PhaseVerify:     {TokenFrac: 0.15, MaxPFCalls: 1, AllowEdits: false, IsVerify: true},
	}
}

// PhaseResult captures the outcome of a single phase.
type PhaseResult struct {
	Phase     SoloPhase `json:"phase"`
	TokensIn  int       `json:"tokens_in"`
	TokensOut int       `json:"tokens_out"`
}

// ExecutePhased runs the full solo pipeline in 4 micro-phases:
// understand -> plan -> execute -> verify.
// Each phase gets a fraction of the total token budget and a phase-specific
// playbook. Only the Execute phase produces and integrates Edit IR.
func (se *SoloExecutor) ExecutePhased(taskDesc, repoPath string) (*SoloResult, error) {
	result := &SoloResult{}

	// 1. Plan mission (same as single-turn)
	sm, err := se.planner.Plan(taskDesc, repoPath)
	if err != nil {
		return nil, fmt.Errorf("solo phased: plan: %w", err)
	}
	result.Mission = sm.Mission

	// 2. Build initial candidates
	candidates := se.buildCandidates(sm)

	phases := DefaultPhaseConfigs()
	totalBudget := sm.Mission.TokenBudget
	var prevContext json.RawMessage

	for _, phase := range SoloPhaseOrder {
		pc := phases[phase]
		phaseBudget := int(float64(totalBudget) * pc.TokenFrac)

		// Build phase-specific mission with adjusted budget
		phaseMission := sm.Mission
		phaseMission.TokenBudget = phaseBudget

		phaseSM := &SoloMission{
			Mission:     phaseMission,
			WorkingSet:  sm.WorkingSet,
			PFPlaybook:  phasePlaybook(phase, pc, prevContext),
			TestTargets: sm.TestTargets,
			Constraints: phaseConstraints(phase, sm.Constraints),
			PromptRef:   sm.PromptRef,
		}

		pack, err := se.packer.Pack(phaseSM, candidates)
		if err != nil {
			return nil, fmt.Errorf("solo phase %s: pack: %w", phase, err)
		}
		pack.Phase = string(phase)

		phaseTokensIn := pack.BudgetMeta.TotalTokens
		result.TokensIn += phaseTokensIn
		result.Calls++
		result.Turns++

		// Send to provider
		resp, err := se.provider.Send(pack)
		if err != nil {
			result.Error = fmt.Sprintf("phase %s: %v", phase, err)
			return result, nil
		}

		phaseTokensOut := EstimateTokens(resp)
		result.TokensOut += phaseTokensOut
		prevContext = json.RawMessage(resp)

		result.Phases = append(result.Phases, PhaseResult{
			Phase:     phase,
			TokensIn:  phaseTokensIn,
			TokensOut: phaseTokensOut,
		})

		// Only integrate during execute phase
		if pc.AllowEdits {
			var angelResp AngelResponse
			if err := json.Unmarshal(resp, &angelResp); err != nil {
				result.Error = fmt.Sprintf("phase %s: parse: %v", phase, err)
				return result, nil
			}

			if angelResp.OutputType == "edit_ir" || angelResp.OutputType == "macro_ops" {
				// Acquire supplemental leases for files/symbols Angel touched but weren't in original scopes
				{
					var extraScopes []Scope
					for _, f := range angelResp.Manifest.FilesTouched {
						extraScopes = append(extraScopes, Scope{ScopeType: "file", ScopeValue: f})
					}
					for _, s := range angelResp.Manifest.SymbolsTouched {
						extraScopes = append(extraScopes, Scope{ScopeType: "symbol", ScopeValue: s})
					}
					if len(extraScopes) > 0 {
						se.heaven.LeaseAcquire(sm.OwnerID, sm.Mission.MissionID, extraScopes)
					}
				}

				clocks, _ := se.heaven.FileClockGet(angelResp.Manifest.FilesTouched)
				intResult, err := se.integrator.Integrate(IntegrateRequest{
					OwnerID:    sm.OwnerID,
					RepoRoot:   repoPath,
					Response:   &angelResp,
					Mission:    sm.Mission,
					FileClocks: clocks,
				})
				if err != nil {
					result.Error = err.Error()
					return result, nil
				}
				result.Success = intResult.Success
				result.FilesModified = intResult.FilesModified
				result.FilesCreated = intResult.FilesCreated
				if !intResult.Success {
					result.Error = intResult.Error
					return result, nil
				}
			}
		}
	}

	if result.Error == "" && !result.Success {
		result.Success = true
	}

	return result, nil
}

// phasePlaybook generates phase-specific PF instructions.
func phasePlaybook(phase SoloPhase, pc PhaseConfig, prevContext json.RawMessage) string {
	var sb strings.Builder

	switch phase {
	case PhaseUnderstand:
		sb.WriteString("PHASE: UNDERSTAND\n")
		sb.WriteString("Analyze the codebase and understand the task requirements.\n")
		sb.WriteString("Output a JSON summary of relevant symbols, dependencies, and risks.\n")
		sb.WriteString("DO NOT produce Edit IR in this phase.\n")
	case PhasePlan:
		sb.WriteString("PHASE: PLAN\n")
		sb.WriteString("Create a step-by-step edit plan based on your understanding.\n")
		sb.WriteString("Output a JSON edit plan listing files, symbols, and changes needed.\n")
		sb.WriteString("DO NOT produce Edit IR in this phase.\n")
	case PhaseExecute:
		sb.WriteString("PHASE: EXECUTE\n")
		sb.WriteString("Implement the edit plan using Edit IR operations.\n")
		sb.WriteString("Output Edit IR JSON with all necessary changes.\n")
	case PhaseVerify:
		sb.WriteString("PHASE: VERIFY\n")
		sb.WriteString("Verify the changes are correct and complete.\n")
		sb.WriteString("Output a JSON verification report.\n")
		sb.WriteString("DO NOT produce Edit IR in this phase.\n")
	}

	if pc.MaxPFCalls > 0 {
		fmt.Fprintf(&sb, "\nPF budget: %d calls for this phase.\n", pc.MaxPFCalls)
	}

	if len(prevContext) > 0 {
		sb.WriteString("\nPREVIOUS PHASE CONTEXT:\n")
		ctx := string(prevContext)
		if len(ctx) > 2000 {
			ctx = ctx[:2000] + "...(truncated)"
		}
		sb.WriteString(ctx)
	}

	return sb.String()
}

// phaseConstraints adds phase-specific constraints to the base constraints.
func phaseConstraints(phase SoloPhase, base []string) []string {
	out := make([]string, len(base))
	copy(out, base)

	switch phase {
	case PhaseUnderstand, PhasePlan, PhaseVerify:
		out = append(out, "DO NOT output Edit IR in this phase")
	case PhaseExecute:
		out = append(out, "Output MUST be Edit IR or Macro Ops")
	}

	return out
}

// buildCandidates queries Heaven for shards relevant to the solo mission.
func (se *SoloExecutor) buildCandidates(sm *SoloMission) []CandidateShard {
	var candidates []CandidateShard
	seen := make(map[string]bool)

	for _, scope := range sm.Mission.Scopes {
		if scope.ScopeType != "symbol" || seen[scope.ScopeValue] {
			continue
		}
		seen[scope.ScopeValue] = true

		// Symdef
		syms, err := se.heaven.IRSymdef(scope.ScopeValue)
		if err != nil || len(syms) == 0 {
			candidates = append(candidates, CandidateShard{
				Kind:    "symdef",
				BlobID:  "blob-" + scope.ScopeValue,
				Content: []byte(fmt.Sprintf(`{"symbol":%q}`, scope.ScopeValue)),
				Symbol:  scope.ScopeValue,
			})
			continue
		}

		sym := syms[0]
		content, _ := json.Marshal(map[string]any{
			"symbol": sym.Name,
			"kind":   sym.Kind,
			"path":   sym.Path,
			"lines":  []int{sym.StartLine, sym.EndLine},
		})
		candidates = append(candidates, CandidateShard{
			Kind:    "symdef",
			BlobID:  fmt.Sprintf("solo-symdef-%s-%d", scope.ScopeValue, sym.ID),
			Content: content,
			Symbol:  scope.ScopeValue,
			Path:    sym.Path,
		})

		// Callers (prefetch)
		refs, _ := se.heaven.IRCallers(scope.ScopeValue, 5)
		if len(refs) > 0 {
			refContent, _ := json.Marshal(map[string]any{
				"symbol":  scope.ScopeValue,
				"callers": refs,
			})
			candidates = append(candidates, CandidateShard{
				Kind:    "callers",
				BlobID:  fmt.Sprintf("solo-callers-%s", scope.ScopeValue),
				Content: refContent,
				Symbol:  scope.ScopeValue,
			})
		}
	}

	return candidates
}
