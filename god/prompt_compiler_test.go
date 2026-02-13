package god

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Token estimator tests
// ---------------------------------------------------------------------------

func TestEstimateTokensEmpty(t *testing.T) {
	tokens := EstimateTokens([]byte{})
	if tokens != 10 { // overhead only
		t.Errorf("EstimateTokens(empty) = %d, want 10", tokens)
	}
}

func TestEstimateTokensSmall(t *testing.T) {
	data := []byte("hello world") // 11 chars → 11/4 + 10 = 12
	tokens := EstimateTokens(data)
	if tokens != 12 {
		t.Errorf("EstimateTokens(%q) = %d, want 12", data, tokens)
	}
}

func TestEstimateTokensLarger(t *testing.T) {
	data := make([]byte, 400) // 400/4 + 10 = 110
	tokens := EstimateTokens(data)
	if tokens != 110 {
		t.Errorf("EstimateTokens(400 bytes) = %d, want 110", tokens)
	}
}

// ---------------------------------------------------------------------------
// Shard scoring tests
// ---------------------------------------------------------------------------

func TestScoreShardSymdef(t *testing.T) {
	shard := CandidateShard{Kind: "symdef", Content: make([]byte, 100)}
	score := ScoreShard(shard)
	// 3.0 - 0.01*(100/4+10) = 3.0 - 0.35 = 2.65
	if score < 2.6 || score > 2.7 {
		t.Errorf("symdef score = %f, want ~2.65", score)
	}
}

func TestScoreShardCallers(t *testing.T) {
	shard := CandidateShard{Kind: "callers", Content: make([]byte, 100)}
	score := ScoreShard(shard)
	// 1.5 - 0.01*35 = 1.5 - 0.35 = 1.15
	if score < 1.1 || score > 1.2 {
		t.Errorf("callers score = %f, want ~1.15", score)
	}
}

func TestScoreShardSlice(t *testing.T) {
	shard := CandidateShard{Kind: "slice", Content: make([]byte, 100)}
	score := ScoreShard(shard)
	// 0 - 0.35 = -0.35
	if score > -0.3 || score < -0.4 {
		t.Errorf("slice score = %f, want ~-0.35", score)
	}
}

func TestScoreShardAllBonuses(t *testing.T) {
	shard := CandidateShard{
		Kind:            "symdef",
		Content:         make([]byte, 40), // 40/4+10=20 tokens → -0.20
		TestRelevant:    true,
		HotsetHit:       true,
		RecentlyTouched: true,
	}
	score := ScoreShard(shard)
	// 3.0 + 2.0 + 1.0 + 0.5 - 0.01*20 = 6.5 - 0.20 = 6.30
	if score < 6.2 || score > 6.4 {
		t.Errorf("all bonuses score = %f, want ~6.30", score)
	}
}

func TestScoreShardTestRelevant(t *testing.T) {
	base := CandidateShard{Kind: "slice", Content: make([]byte, 40)}
	withTest := CandidateShard{Kind: "slice", Content: make([]byte, 40), TestRelevant: true}
	diff := ScoreShard(withTest) - ScoreShard(base)
	if diff < 1.9 || diff > 2.1 {
		t.Errorf("test_relevant bonus = %f, want 2.0", diff)
	}
}

// ---------------------------------------------------------------------------
// ScoreShards ordering tests
// ---------------------------------------------------------------------------

func TestScoreShardsOrdering(t *testing.T) {
	candidates := []CandidateShard{
		{Kind: "slice", Content: make([]byte, 100)},   // lowest score
		{Kind: "symdef", Content: make([]byte, 100)},  // highest score
		{Kind: "callers", Content: make([]byte, 100)},  // middle score
	}
	scored := ScoreShards(candidates)
	if scored[0].Shard.Kind != "symdef" {
		t.Errorf("first should be symdef, got %s", scored[0].Shard.Kind)
	}
	if scored[1].Shard.Kind != "callers" {
		t.Errorf("second should be callers, got %s", scored[1].Shard.Kind)
	}
	if scored[2].Shard.Kind != "slice" {
		t.Errorf("third should be slice, got %s", scored[2].Shard.Kind)
	}
	// Verify descending
	for i := 1; i < len(scored); i++ {
		if scored[i].Score > scored[i-1].Score {
			t.Errorf("scores not descending: [%d]=%f > [%d]=%f", i, scored[i].Score, i-1, scored[i-1].Score)
		}
	}
}

// ---------------------------------------------------------------------------
// Compiler tests
// ---------------------------------------------------------------------------

func makeTestMission(budget int) Mission {
	return Mission{
		MissionID:   "test-mission-id",
		Goal:        "Test goal",
		BaseRev:     "abc123",
		Scopes:      []Scope{{ScopeType: "symbol", ScopeValue: "Greet"}},
		LeaseIDs:    []string{},
		Tasks:       []string{"task1"},
		TokenBudget: budget,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

func TestCompileRespectsBudget(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(500)

	// Create shards that each use ~110 tokens (400 bytes → 400/4+10=110)
	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "blob1", Content: make([]byte, 400), Symbol: "Greet"},
		{Kind: "callers", BlobID: "blob2", Content: make([]byte, 400), Symbol: "Greet"},
		{Kind: "slice", BlobID: "blob3", Content: make([]byte, 400), Path: "main.go"},
		{Kind: "slice", BlobID: "blob4", Content: make([]byte, 400), Path: "util.go"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.BudgetMeta.TotalTokens > pack.BudgetMeta.TokenBudget {
		t.Errorf("total tokens %d exceeds budget %d", pack.BudgetMeta.TotalTokens, pack.BudgetMeta.TokenBudget)
	}
	if pack.BudgetMeta.ShardsIncluded+pack.BudgetMeta.ShardsDropped != len(candidates) {
		t.Errorf("included(%d) + dropped(%d) != candidates(%d)",
			pack.BudgetMeta.ShardsIncluded, pack.BudgetMeta.ShardsDropped, len(candidates))
	}
}

func TestCompileChoosesSymdefOverSlice(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	// Very tight budget: only room for header + mission + 1 shard
	mission := makeTestMission(300)

	symdefContent := []byte(`{"name":"Greet","kind":"function"}`)
	sliceContent := []byte(`{"content":"func main() {}"}`)

	candidates := []CandidateShard{
		{Kind: "slice", BlobID: "slice1", Content: sliceContent, Path: "main.go"},
		{Kind: "symdef", BlobID: "symdef1", Content: symdefContent, Symbol: "Greet"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pack.InlineShards) == 0 {
		t.Fatal("expected at least one shard to be included")
	}

	// The first included shard should be symdef (higher score)
	if pack.InlineShards[0].Kind != "symdef" {
		t.Errorf("first shard should be symdef, got %s", pack.InlineShards[0].Kind)
	}
}

func TestCompileNoCandidates(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000)

	pack, err := pc.Compile(mission, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pack.InlineShards) != 0 {
		t.Errorf("expected no shards, got %d", len(pack.InlineShards))
	}
	if pack.BudgetMeta.ShardTokens != 0 {
		t.Errorf("shard tokens should be 0, got %d", pack.BudgetMeta.ShardTokens)
	}
	if pack.BudgetMeta.ShardsIncluded != 0 {
		t.Errorf("shards included should be 0, got %d", pack.BudgetMeta.ShardsIncluded)
	}
}

func TestCompileAllFitUnderBudget(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000) // large budget

	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: make([]byte, 100), Symbol: "A"},
		{Kind: "callers", BlobID: "b2", Content: make([]byte, 100), Symbol: "A"},
		{Kind: "slice", BlobID: "b3", Content: make([]byte, 100), Path: "x.go"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.BudgetMeta.ShardsIncluded != 3 {
		t.Errorf("all 3 shards should fit, got %d included", pack.BudgetMeta.ShardsIncluded)
	}
	if pack.BudgetMeta.ShardsDropped != 0 {
		t.Errorf("no shards should be dropped, got %d", pack.BudgetMeta.ShardsDropped)
	}
}

func TestCompileTightBudgetDropsExcess(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	// Budget so tight that only header+mission fit, no shards
	mission := makeTestMission(100)

	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: make([]byte, 400), Symbol: "A"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.BudgetMeta.ShardsIncluded != 0 {
		t.Errorf("expected 0 shards included with tiny budget, got %d", pack.BudgetMeta.ShardsIncluded)
	}
	if pack.BudgetMeta.ShardsDropped != 1 {
		t.Errorf("expected 1 shard dropped, got %d", pack.BudgetMeta.ShardsDropped)
	}
}

func TestCompilePackHeader(t *testing.T) {
	pc := NewPromptCompiler("http://heaven:8080/pf")
	mission := makeTestMission(8000)

	pack, err := pc.Compile(mission, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(pack.Header, "GENESIS SSMP") {
		t.Error("header should contain 'GENESIS SSMP'")
	}
	if !strings.Contains(pack.Header, mission.MissionID) {
		t.Error("header should contain mission ID")
	}
	if !strings.Contains(pack.Header, "http://heaven:8080/pf") {
		t.Error("header should contain PF endpoint")
	}
}

func TestCompilePFEndpoint(t *testing.T) {
	endpoint := "http://heaven:8080/pf"
	pc := NewPromptCompiler(endpoint)
	mission := makeTestMission(8000)

	pack, err := pc.Compile(mission, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.PFEndpoint != endpoint {
		t.Errorf("PFEndpoint = %q, want %q", pack.PFEndpoint, endpoint)
	}
}

func TestCompileMissionPassedThrough(t *testing.T) {
	pc := NewPromptCompiler("http://localhost/pf")
	mission := makeTestMission(8000)

	pack, err := pc.Compile(mission, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pack.Mission.MissionID != mission.MissionID {
		t.Error("mission should be passed through unchanged")
	}
	if pack.Mission.Goal != mission.Goal {
		t.Error("goal should match")
	}
}

func TestCompileBudgetMetaAccounting(t *testing.T) {
	pc := NewPromptCompiler("http://localhost/pf")
	mission := makeTestMission(8000)

	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: make([]byte, 200), Symbol: "X"},
		{Kind: "callers", BlobID: "b2", Content: make([]byte, 200), Symbol: "X"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := pack.BudgetMeta
	if meta.TokenBudget != 8000 {
		t.Errorf("TokenBudget = %d, want 8000", meta.TokenBudget)
	}
	if meta.HeaderTokens <= 0 {
		t.Error("HeaderTokens should be > 0")
	}
	if meta.MissionTokens <= 0 {
		t.Error("MissionTokens should be > 0")
	}
	if meta.TotalTokens != meta.HeaderTokens+meta.MissionTokens+meta.ShardTokens {
		t.Errorf("TotalTokens(%d) != Header(%d) + Mission(%d) + Shards(%d)",
			meta.TotalTokens, meta.HeaderTokens, meta.MissionTokens, meta.ShardTokens)
	}
}

func TestCompileShardMetaIncludes(t *testing.T) {
	pc := NewPromptCompiler("http://localhost/pf")
	mission := makeTestMission(8000)

	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: []byte(`{"name":"Foo"}`), Symbol: "Foo"},
		{Kind: "slice", BlobID: "b2", Content: []byte(`{"content":"x"}`), Path: "bar.go"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, ps := range pack.InlineShards {
		if ps.Meta["score"] == nil {
			t.Errorf("shard %s missing score in meta", ps.Kind)
		}
		if ps.Meta["estimated_tokens"] == nil {
			t.Errorf("shard %s missing estimated_tokens in meta", ps.Kind)
		}
	}

	// Find symdef shard and check symbol meta
	for _, ps := range pack.InlineShards {
		if ps.Kind == "symdef" {
			if ps.Meta["symbol"] != "Foo" {
				t.Errorf("symdef shard meta symbol = %v, want Foo", ps.Meta["symbol"])
			}
		}
		if ps.Kind == "slice" {
			if ps.Meta["path"] != "bar.go" {
				t.Errorf("slice shard meta path = %v, want bar.go", ps.Meta["path"])
			}
		}
	}
}

func TestCompilePackIsValidJSON(t *testing.T) {
	pc := NewPromptCompiler("http://localhost/pf")
	mission := makeTestMission(8000)

	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: []byte(`{"name":"Greet"}`), Symbol: "Greet"},
	}

	pack, err := pc.Compile(mission, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("failed to marshal pack: %v", err)
	}
	if len(data) == 0 {
		t.Error("marshalled pack should not be empty")
	}

	// Verify it round-trips
	var decoded MissionPack
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal pack: %v", err)
	}
	if decoded.Mission.MissionID != mission.MissionID {
		t.Error("round-trip mission ID mismatch")
	}
}

// ---------------------------------------------------------------------------
// Integration: AA Parse → Compile → Prompt Compile
// ---------------------------------------------------------------------------

func TestAAToPromptCompileRoundTrip(t *testing.T) {
	src := `
BASE_REV abc123
LEASE symbol:Greet file:main.go
NEED symdef Greet
NEED callers Greet 5
NEED slice main.go 1 10
DO Refactor the Greet function
RETURN edit_ir
`
	prog, err := ParseAA(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cr, err := CompileAA(prog)
	if err != nil {
		t.Fatalf("compile AA: %v", err)
	}

	if err := ValidateMission(cr.Mission); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Simulate shard responses from Heaven
	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "sha-1", Content: []byte(`[{"name":"Greet","kind":"function"}]`), Symbol: "Greet"},
		{Kind: "callers", BlobID: "sha-2", Content: []byte(`[{"ref_kind":"call","path":"api.go"}]`), Symbol: "Greet"},
		{Kind: "slice", BlobID: "sha-3", Content: []byte(`{"content":"func main() {\n  Greet()\n}"}`), Path: "main.go"},
	}

	pc := NewPromptCompiler("http://heaven:8080/pf")
	pack, err := pc.Compile(cr.Mission, candidates)
	if err != nil {
		t.Fatalf("prompt compile: %v", err)
	}

	if pack.Mission.MissionID != cr.Mission.MissionID {
		t.Error("mission ID should match")
	}
	if pack.BudgetMeta.TokenBudget != 8000 {
		t.Errorf("budget = %d, want 8000", pack.BudgetMeta.TokenBudget)
	}
	if pack.BudgetMeta.TotalTokens > 8000 {
		t.Errorf("total tokens %d exceeds budget", pack.BudgetMeta.TotalTokens)
	}
	if len(pack.InlineShards) == 0 {
		t.Error("expected at least one shard in pack")
	}
	// Symdef should be first (highest scored)
	if pack.InlineShards[0].Kind != "symdef" {
		t.Errorf("first shard should be symdef, got %s", pack.InlineShards[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// Determinism tests
// ---------------------------------------------------------------------------

func TestCompileDeterminism(t *testing.T) {
	// Fix the clock for deterministic timestamps
	origNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = origNow }()

	pc := NewPromptCompiler("http://localhost/pf")
	mission := Mission{
		MissionID:   "determinism-test",
		Goal:        "Test determinism",
		BaseRev:     "HEAD",
		Scopes:      []Scope{{ScopeType: "symbol", ScopeValue: "Foo"}},
		LeaseIDs:    []string{},
		Tasks:       []string{"implement"},
		TokenBudget: 8000,
		CreatedAt:   "2025-01-01T00:00:00Z",
	}
	candidates := []CandidateShard{
		{Kind: "symdef", BlobID: "b1", Content: []byte(`{"name":"Foo"}`), Symbol: "Foo"},
		{Kind: "callers", BlobID: "b2", Content: []byte(`[{"ref":"bar"}]`), Symbol: "Foo"},
		{Kind: "slice", BlobID: "b3", Content: []byte(`{"content":"x"}`), Path: "x.go"},
	}

	var packs [][]byte
	for i := 0; i < 10; i++ {
		pack, err := pc.Compile(mission, candidates)
		if err != nil {
			t.Fatalf("compile %d: %v", i, err)
		}
		data, _ := json.Marshal(pack)
		packs = append(packs, data)
	}

	for i := 1; i < len(packs); i++ {
		if string(packs[i]) != string(packs[0]) {
			t.Fatalf("pack %d differs from pack 0", i)
		}
	}
}

// ---------------------------------------------------------------------------
// PromptRef tests
// ---------------------------------------------------------------------------

func TestCompileWithPromptRef(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000)

	promptRef := &PromptRef{
		PromptID:       "abc123",
		PinnedSections: []int{0, 1},
		TotalSections:  5,
		TotalTokens:    2000,
		InlinedTokens:  400,
	}

	// Create pinned prompt_section candidates
	candidates := []CandidateShard{
		{Kind: "prompt_section", BlobID: "ps0", Content: []byte(`{"content":"spec text"}`), Symbol: "section_0"},
		{Kind: "prompt_section", BlobID: "ps1", Content: []byte(`{"content":"constraints text"}`), Symbol: "section_1"},
		{Kind: "symdef", BlobID: "b1", Content: []byte(`{"name":"Foo"}`), Symbol: "Foo"},
	}

	pack, err := pc.CompileWithPromptRef(mission, candidates, promptRef)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Pack should have PromptRef set
	if pack.PromptRef == nil {
		t.Fatal("pack.PromptRef should not be nil")
	}
	if pack.PromptRef.PromptID != "abc123" {
		t.Errorf("PromptRef.PromptID = %q, want abc123", pack.PromptRef.PromptID)
	}

	// Should have prompt_section shards inlined
	hasPromptSection := false
	for _, s := range pack.InlineShards {
		if s.Kind == "prompt_section" {
			hasPromptSection = true
		}
	}
	if !hasPromptSection {
		t.Error("pack should contain prompt_section shards")
	}

	// Header should contain PROMPT VM instructions
	if !strings.Contains(pack.Header, "PROMPT VM") {
		t.Error("header should contain PROMPT VM instructions")
	}
	if !strings.Contains(pack.Header, "PF_PROMPT_SECTION") {
		t.Error("header should contain PF_PROMPT_SECTION instruction")
	}
}

func TestCompilePromptRefTokenBudget(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000)

	promptRef := &PromptRef{
		PromptID:       "budget-test",
		PinnedSections: []int{0},
		TotalSections:  5,
		TotalTokens:    2000,
		InlinedTokens:  100,
	}

	candidates := []CandidateShard{
		{Kind: "prompt_section", BlobID: "ps0", Content: []byte(`{"content":"short"}`), Symbol: "section_0"},
	}

	pack, err := pc.CompileWithPromptRef(mission, candidates, promptRef)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Only pinned section tokens should count, not full prompt tokens
	if pack.BudgetMeta.TotalTokens > pack.BudgetMeta.TokenBudget {
		t.Errorf("total tokens %d > budget %d", pack.BudgetMeta.TotalTokens, pack.BudgetMeta.TokenBudget)
	}
	// Total tokens should be much less than full prompt TotalTokens
	if pack.BudgetMeta.ShardTokens > promptRef.TotalTokens {
		t.Errorf("shard tokens %d > full prompt tokens %d — pinning should save tokens", pack.BudgetMeta.ShardTokens, promptRef.TotalTokens)
	}
}

func TestCompilePromptRefRoleSelection(t *testing.T) {
	sections := []PromptSectionInfo{
		{Index: 0, SectionType: "spec"},
		{Index: 1, SectionType: "constraints"},
		{Index: 2, SectionType: "api"},
		{Index: 3, SectionType: "acceptance"},
		{Index: 4, SectionType: "security"},
		{Index: 5, SectionType: "examples"},
	}

	plannerPins := PinnedSectionsForRole("planner", sections)
	builderPins := PinnedSectionsForRole("builder", sections)
	reviewerPins := PinnedSectionsForRole("reviewer", sections)

	// Planner should pin constraints, acceptance, spec
	plannerHas := map[int]bool{}
	for _, p := range plannerPins {
		plannerHas[p] = true
	}
	if !plannerHas[0] { // spec
		t.Error("planner should pin spec (index 0)")
	}
	if !plannerHas[1] { // constraints
		t.Error("planner should pin constraints (index 1)")
	}
	if !plannerHas[3] { // acceptance
		t.Error("planner should pin acceptance (index 3)")
	}

	// Builder should pin api, constraints, examples
	builderHas := map[int]bool{}
	for _, p := range builderPins {
		builderHas[p] = true
	}
	if !builderHas[2] { // api
		t.Error("builder should pin api (index 2)")
	}

	// Reviewer should pin acceptance, security, constraints
	reviewerHas := map[int]bool{}
	for _, p := range reviewerPins {
		reviewerHas[p] = true
	}
	if !reviewerHas[4] { // security
		t.Error("reviewer should pin security (index 4)")
	}
}

func TestPromptSalienceScoring(t *testing.T) {
	// Constraints should score highest, other lowest
	if ScorePromptSection("constraints") <= ScorePromptSection("acceptance") {
		t.Error("constraints should score > acceptance")
	}
	if ScorePromptSection("acceptance") <= ScorePromptSection("api") {
		t.Error("acceptance should score > api")
	}
	if ScorePromptSection("api") <= ScorePromptSection("style") {
		t.Error("api should score > style")
	}
	if ScorePromptSection("glossary") <= ScorePromptSection("other") {
		t.Error("glossary should score > other")
	}
	if ScorePromptSection("other") != 0.0 {
		t.Errorf("other score = %f, want 0.0", ScorePromptSection("other"))
	}
	if ScorePromptSection("constraints") != 3.0 {
		t.Errorf("constraints score = %f, want 3.0", ScorePromptSection("constraints"))
	}
}

func TestCompilePromptRefPFInstructions(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000)

	promptRef := &PromptRef{
		PromptID:       "pf-instr-test",
		PinnedSections: []int{},
		TotalSections:  3,
		TotalTokens:    1000,
	}

	pack, err := pc.CompileWithPromptRef(mission, nil, promptRef)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Header must contain PF_PROMPT_* usage instructions
	for _, cmd := range []string{"PF_PROMPT_SECTION", "PF_PROMPT_SEARCH", "PF_PROMPT_SUMMARY"} {
		if !strings.Contains(pack.Header, cmd) {
			t.Errorf("header missing %s instruction", cmd)
		}
	}
}

func TestNoFullPromptResend(t *testing.T) {
	pc := NewPromptCompiler("http://localhost:9999/pf")
	mission := makeTestMission(8000)

	// Create a large "full prompt" worth of data
	fullPrompt := make([]byte, 4000) // ~1000 tokens
	for i := range fullPrompt {
		fullPrompt[i] = 'x'
	}
	fullPromptTokens := EstimateTokens(fullPrompt)

	promptRef := &PromptRef{
		PromptID:       "no-resend-test",
		PinnedSections: []int{0},
		TotalSections:  5,
		TotalTokens:    fullPromptTokens,
		InlinedTokens:  100,
	}

	// Only a small pinned section, not the full prompt
	candidates := []CandidateShard{
		{Kind: "prompt_section", BlobID: "ps0", Content: []byte(`{"short":"section"}`), Symbol: "section_0"},
	}

	pack, err := pc.CompileWithPromptRef(mission, candidates, promptRef)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	packJSON, _ := json.Marshal(pack)
	// Pack should be significantly smaller than full prompt
	if len(packJSON) > len(fullPrompt) {
		t.Errorf("pack bytes %d > full prompt bytes %d — full prompt may have leaked", len(packJSON), len(fullPrompt))
	}
}

func TestCompileScoreShardDeterministicOrdering(t *testing.T) {
	// Equal-score shards should be ordered deterministically
	candidates := []CandidateShard{
		{Kind: "slice", BlobID: "b1", Content: make([]byte, 100), Path: "a.go"},
		{Kind: "slice", BlobID: "b2", Content: make([]byte, 100), Path: "b.go"},
		{Kind: "slice", BlobID: "b3", Content: make([]byte, 100), Path: "c.go"},
	}

	// Run 10 times and verify ordering is consistent
	var firstOrder []string
	for i := 0; i < 10; i++ {
		scored := ScoreShards(candidates)
		var order []string
		for _, s := range scored {
			order = append(order, s.Shard.BlobID)
		}
		if i == 0 {
			firstOrder = order
		} else {
			for j := range order {
				if order[j] != firstOrder[j] {
					t.Fatalf("run %d: order differs at position %d: %v vs %v", i, j, order, firstOrder)
				}
			}
		}
	}
}
