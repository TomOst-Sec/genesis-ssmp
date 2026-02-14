package god

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock provider
// ---------------------------------------------------------------------------

// mockProvider implements Provider for testing.
type mockProvider struct {
	responses [][]byte // returns responses in order, cycling last one
	callCount int
}

func (m *mockProvider) Send(pack *MissionPack) ([]byte, error) {
	idx := m.callCount
	if idx >= len(m.responses) {
		idx = len(m.responses) - 1
	}
	m.callCount++
	return m.responses[idx], nil
}

// mockErrorProvider always returns an error.
type mockErrorProvider struct{}

func (m *mockErrorProvider) Send(pack *MissionPack) ([]byte, error) {
	return nil, fmt.Errorf("connection refused")
}

func validAngelResponse(missionID string) AngelResponse {
	return AngelResponse{
		MissionID:  missionID,
		OutputType: "edit_ir",
		EditIR: &EditIR{
			Ops: []EditOp{
				{
					Op:         "replace_span",
					Path:       "main.go",
					AnchorHash: "abc123",
					Lines:      []int{10, 20},
					Content:    "func Greet(p Person) string {\n  return \"Hello, \" + p.Name\n}",
				},
			},
		},
		Manifest: Manifest{
			SymbolsTouched: []string{"Greet"},
			FilesTouched:   []string{"main.go"},
		},
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func testMissionPack(missionID string) *MissionPack {
	return &MissionPack{
		Header: "test header",
		Mission: Mission{
			MissionID:   missionID,
			Goal:        "test goal",
			BaseRev:     "abc123",
			Scopes:      []Scope{{ScopeType: "symbol", ScopeValue: "Greet"}},
			LeaseIDs:    []string{},
			Tasks:       []string{"task1"},
			TokenBudget: 8000,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		},
		InlineShards: []PackedShard{},
		PFEndpoint:   "http://localhost/pf",
		BudgetMeta:   BudgetMeta{TokenBudget: 8000},
	}
}

// ---------------------------------------------------------------------------
// AngelResponse validation tests
// ---------------------------------------------------------------------------

func TestValidateAngelResponseValid(t *testing.T) {
	resp := validAngelResponse("mission-1")
	if err := validateAngelResponse(&resp, "mission-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAngelResponseMissingMissionID(t *testing.T) {
	resp := validAngelResponse("mission-1")
	resp.MissionID = ""
	err := validateAngelResponse(&resp, "mission-1")
	if err == nil || !strings.Contains(err.Error(), "mission_id") {
		t.Fatalf("expected mission_id error, got: %v", err)
	}
}

func TestValidateAngelResponseMismatchedMissionID(t *testing.T) {
	resp := validAngelResponse("wrong-id")
	err := validateAngelResponse(&resp, "mission-1")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
}

func TestValidateAngelResponseInvalidOutputType(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.OutputType = "garbage"
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "invalid output_type") {
		t.Fatalf("expected output_type error, got: %v", err)
	}
}

func TestValidateAngelResponseEditIRNilOps(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.EditIR.Ops = nil
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "ops must not be nil") {
		t.Fatalf("expected nil ops error, got: %v", err)
	}
}

func TestValidateAngelResponseEditIRNilField(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.EditIR = nil
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "edit_ir field is nil") {
		t.Fatalf("expected nil edit_ir error, got: %v", err)
	}
}

func TestValidateAngelResponseDiffFallbackValid(t *testing.T) {
	resp := AngelResponse{
		MissionID:  "m1",
		OutputType: "diff_fallback",
		Diff:       "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n-old\n+new",
		Manifest: Manifest{
			SymbolsTouched: []string{"Greet"},
			FilesTouched:   []string{"main.go"},
		},
	}
	if err := validateAngelResponse(&resp, "m1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAngelResponseDiffFallbackEmpty(t *testing.T) {
	resp := AngelResponse{
		MissionID:  "m1",
		OutputType: "diff_fallback",
		Diff:       "",
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "diff field is empty") {
		t.Fatalf("expected empty diff error, got: %v", err)
	}
}

func TestValidateAngelResponseNilManifestSymbols(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.Manifest.SymbolsTouched = nil
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "symbols_touched") {
		t.Fatalf("expected manifest error, got: %v", err)
	}
}

func TestValidateAngelResponseNilManifestFiles(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.Manifest.FilesTouched = nil
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "files_touched") {
		t.Fatalf("expected manifest error, got: %v", err)
	}
}

func TestValidateEditOpInvalidOp(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.EditIR.Ops[0].Op = "invalid_op"
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "invalid op") {
		t.Fatalf("expected invalid op error, got: %v", err)
	}
}

func TestValidateEditOpMissingPath(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.EditIR.Ops[0].Path = ""
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected missing path error, got: %v", err)
	}
}

func TestValidateEditOpMissingAnchorHash(t *testing.T) {
	resp := validAngelResponse("m1")
	resp.EditIR.Ops[0].AnchorHash = ""
	err := validateAngelResponse(&resp, "m1")
	if err == nil || !strings.Contains(err.Error(), "anchor_hash is required") {
		t.Fatalf("expected missing anchor_hash error, got: %v", err)
	}
}

func TestValidateEditOpAllValidTypes(t *testing.T) {
	validOps := []string{"replace_span", "insert_after_symbol", "add_file", "delete_span"}
	for _, opType := range validOps {
		t.Run(opType, func(t *testing.T) {
			resp := validAngelResponse("m1")
			resp.EditIR.Ops[0].Op = opType
			if err := validateAngelResponse(&resp, "m1"); err != nil {
				t.Fatalf("op type %q should be valid, got: %v", opType, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ProviderAdapter tests
// ---------------------------------------------------------------------------

func TestProviderAdapterSuccessFirstAttempt(t *testing.T) {
	resp := validAngelResponse("m1")
	mock := &mockProvider{responses: [][]byte{mustJSON(resp)}}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	angel, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if angel.MissionID != "m1" {
		t.Errorf("MissionID = %q, want m1", angel.MissionID)
	}
	if angel.OutputType != "edit_ir" {
		t.Errorf("OutputType = %q, want edit_ir", angel.OutputType)
	}
	if len(angel.EditIR.Ops) != 1 {
		t.Errorf("Ops count = %d, want 1", len(angel.EditIR.Ops))
	}
	if !usage.Success {
		t.Error("usage.Success should be true")
	}
	if usage.Retries != 0 {
		t.Errorf("usage.Retries = %d, want 0", usage.Retries)
	}
	if usage.DurationMS < 0 {
		t.Error("usage.DurationMS should be >= 0")
	}
}

func TestProviderAdapterRetryOnInvalidJSON(t *testing.T) {
	validResp := validAngelResponse("m1")
	mock := &mockProvider{
		responses: [][]byte{
			[]byte(`{this is not json}`),   // first: invalid JSON
			mustJSON(validResp),             // second: valid
		},
	}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	angel, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if angel.MissionID != "m1" {
		t.Error("should succeed after retry")
	}
	if usage.Retries != 1 {
		t.Errorf("usage.Retries = %d, want 1", usage.Retries)
	}
	if mock.callCount != 2 {
		t.Errorf("provider called %d times, want 2", mock.callCount)
	}
}

func TestProviderAdapterRetryOnSchemaViolation(t *testing.T) {
	badResp := validAngelResponse("m1")
	badResp.OutputType = "garbage" // schema violation
	validResp := validAngelResponse("m1")

	mock := &mockProvider{
		responses: [][]byte{
			mustJSON(badResp),
			mustJSON(validResp),
		},
	}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	angel, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if angel.OutputType != "edit_ir" {
		t.Errorf("OutputType = %q after retry", angel.OutputType)
	}
	if usage.Retries != 1 {
		t.Errorf("Retries = %d, want 1", usage.Retries)
	}
}

func TestProviderAdapterFailsAfterMaxRetries(t *testing.T) {
	badResp := []byte(`{this is not json}`)
	mock := &mockProvider{
		responses: [][]byte{badResp, badResp}, // both fail
	}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	_, usage, err := adapter.Execute(pack)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should mention validation failure, got: %v", err)
	}
	if usage.Retries != 1 {
		t.Errorf("Retries = %d, want 1", usage.Retries)
	}
	if usage.Success {
		t.Error("usage.Success should be false")
	}
}

func TestProviderAdapterSendError(t *testing.T) {
	adapter := NewProviderAdapter(&mockErrorProvider{})
	pack := testMissionPack("m1")

	_, usage, err := adapter.Execute(pack)
	if err == nil {
		t.Fatal("expected error from send failure")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should mention connection, got: %v", err)
	}
	if usage.Success {
		t.Error("usage.Success should be false")
	}
}

func TestProviderAdapterUsageMetrics(t *testing.T) {
	resp := validAngelResponse("m1")
	respJSON := mustJSON(resp)
	mock := &mockProvider{responses: [][]byte{respJSON}}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	_, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.MissionID != "m1" {
		t.Errorf("MissionID = %q", usage.MissionID)
	}
	if usage.RequestBytes <= 0 {
		t.Error("RequestBytes should be > 0")
	}
	if usage.ResponseBytes != len(respJSON) {
		t.Errorf("ResponseBytes = %d, want %d", usage.ResponseBytes, len(respJSON))
	}
}

func TestProviderAdapterRepairPackContainsError(t *testing.T) {
	// Verify the repair pack includes the error message
	badResp := validAngelResponse("m1")
	badResp.OutputType = "garbage"
	validResp := validAngelResponse("m1")

	var capturedPack *MissionPack
	callCount := 0
	provider := &capturingProvider{
		responses: [][]byte{mustJSON(badResp), mustJSON(validResp)},
		onSend: func(pack *MissionPack) {
			if callCount == 1 {
				capturedPack = pack
			}
			callCount++
		},
	}
	adapter := NewProviderAdapter(provider)
	pack := testMissionPack("m1")

	_, _, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPack == nil {
		t.Fatal("repair pack was not captured")
	}
	if !strings.Contains(capturedPack.Header, "REPAIR REQUEST") {
		t.Error("repair pack header should contain REPAIR REQUEST")
	}
	if !strings.Contains(capturedPack.Header, "invalid output_type") {
		t.Error("repair pack header should contain the validation error")
	}
}

// capturingProvider captures the packs sent to it.
type capturingProvider struct {
	responses [][]byte
	callCount int
	onSend    func(pack *MissionPack)
}

func (p *capturingProvider) Send(pack *MissionPack) ([]byte, error) {
	if p.onSend != nil {
		p.onSend(pack)
	}
	idx := p.callCount
	if idx >= len(p.responses) {
		idx = len(p.responses) - 1
	}
	p.callCount++
	return p.responses[idx], nil
}

// ---------------------------------------------------------------------------
// HTTPProvider tests
// ---------------------------------------------------------------------------

func TestHTTPProviderSend(t *testing.T) {
	resp := validAngelResponse("m1")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewHTTPProvider(ts.URL, "test-key")
	pack := testMissionPack("m1")

	raw, err := provider.Send(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got AngelResponse
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.MissionID != "m1" {
		t.Errorf("MissionID = %q", got.MissionID)
	}
}

func TestHTTPProviderNonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	provider := NewHTTPProvider(ts.URL, "")
	pack := testMissionPack("m1")

	_, err := provider.Send(pack)
	if err == nil {
		t.Fatal("expected error for non-OK status")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

func TestHTTPProviderNoAPIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("should not send Authorization header when API key is empty")
		}
		resp := validAngelResponse("m1")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewHTTPProvider(ts.URL, "")
	pack := testMissionPack("m1")

	_, err := provider.Send(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: HTTPProvider + ProviderAdapter
// ---------------------------------------------------------------------------

func TestEndToEndHTTPProviderAdapter(t *testing.T) {
	resp := validAngelResponse("m1")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewHTTPProvider(ts.URL, "key")
	adapter := NewProviderAdapter(provider)
	pack := testMissionPack("m1")

	angel, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if angel.MissionID != "m1" {
		t.Error("mission ID mismatch")
	}
	if !usage.Success {
		t.Error("should succeed")
	}
	if usage.Retries != 0 {
		t.Errorf("Retries = %d", usage.Retries)
	}
}

func TestRepairPackContainsPreviousResponse(t *testing.T) {
	badResponse := []byte(`{"mission_id":"m1","output_type":"garbage","manifest":{"symbols_touched":[],"files_touched":[]}}`)
	goodResponse := validAngelResponse("m1")
	goodJSON, _ := json.Marshal(goodResponse)

	var capturedPacks []*MissionPack
	provider := &capturingProvider{
		responses: [][]byte{badResponse, goodJSON},
		onSend: func(pack *MissionPack) {
			cp := *pack
			capturedPacks = append(capturedPacks, &cp)
		},
	}
	adapter := NewProviderAdapter(provider)
	pack := testMissionPack("m1")

	_, _, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The second call (retry) should include the bad response in the repair header
	if len(capturedPacks) < 2 {
		t.Fatalf("expected 2 calls, got %d", len(capturedPacks))
	}
	repairPack := capturedPacks[1]
	if !strings.Contains(repairPack.Header, "REPAIR REQUEST") {
		t.Error("repair pack should contain REPAIR REQUEST")
	}
	if !strings.Contains(repairPack.Header, "garbage") {
		t.Error("repair pack should contain the original bad response")
	}
}

func TestProviderAdapterMissionIDPassthrough(t *testing.T) {
	// Send a response with mismatched mission_id -> triggers retry
	mismatchResp, _ := json.Marshal(AngelResponse{
		MissionID:  "wrong-id",
		OutputType: "edit_ir",
		EditIR:     &EditIR{Ops: []EditOp{}},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	})
	correctResp := validAngelResponse("m1")
	correctJSON, _ := json.Marshal(correctResp)

	provider := &capturingProvider{
		responses: [][]byte{mismatchResp, correctJSON},
	}
	adapter := NewProviderAdapter(provider)
	pack := testMissionPack("m1")

	resp, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.MissionID != "m1" {
		t.Errorf("MissionID = %q, want m1", resp.MissionID)
	}
	if usage.Retries != 1 {
		t.Errorf("Retries = %d, want 1", usage.Retries)
	}
}

// ---------------------------------------------------------------------------
// Macro Ops provider integration tests
// ---------------------------------------------------------------------------

func TestProviderMacroOpsValidation(t *testing.T) {
	// Valid macro_ops response
	resp := AngelResponse{
		MissionID:  "m1",
		OutputType: "macro_ops",
		MacroOps: &MacroOps{
			Ops: []MacroOp{
				{Kind: MacroRenameSymbol, OldName: "Foo", NewName: "Bar"},
			},
		},
		Manifest: Manifest{
			SymbolsTouched: []string{"Foo"},
			FilesTouched:   []string{"main.go"},
		},
	}
	if err := validateAngelResponse(&resp, "m1"); err != nil {
		t.Fatalf("valid macro_ops should pass: %v", err)
	}

	// macro_ops with nil MacroOps field
	resp2 := AngelResponse{
		MissionID:  "m1",
		OutputType: "macro_ops",
		MacroOps:   nil,
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	if err := validateAngelResponse(&resp2, "m1"); err == nil {
		t.Error("macro_ops with nil macro_ops field should fail")
	}

	// macro_ops with nil ops array
	resp3 := AngelResponse{
		MissionID:  "m1",
		OutputType: "macro_ops",
		MacroOps:   &MacroOps{Ops: nil},
		Manifest:   Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	if err := validateAngelResponse(&resp3, "m1"); err == nil {
		t.Error("macro_ops with nil ops should fail")
	}

	// macro_ops with invalid macro op
	resp4 := AngelResponse{
		MissionID:  "m1",
		OutputType: "macro_ops",
		MacroOps: &MacroOps{
			Ops: []MacroOp{{Kind: MacroRenameSymbol}}, // missing old_name
		},
		Manifest: Manifest{SymbolsTouched: []string{}, FilesTouched: []string{}},
	}
	if err := validateAngelResponse(&resp4, "m1"); err == nil {
		t.Error("macro_ops with invalid op should fail validation")
	}
}

func TestProviderMacroOpsAutoExpand(t *testing.T) {
	macroResp := AngelResponse{
		MissionID:  "m1",
		OutputType: "macro_ops",
		MacroOps: &MacroOps{
			Ops: []MacroOp{
				{Kind: MacroRenameSymbol, OldName: "Foo", NewName: "Bar", ScopePath: "main.go"},
				{Kind: MacroReplaceSpan, Path: "util.go", StartLine: 5, EndLine: 10, Content: "new code"},
			},
		},
		Manifest: Manifest{
			SymbolsTouched: []string{"Foo"},
			FilesTouched:   []string{"main.go", "util.go"},
		},
	}

	mock := &mockProvider{responses: [][]byte{mustJSON(macroResp)}}
	adapter := NewProviderAdapter(mock)
	pack := testMissionPack("m1")

	angel, usage, err := adapter.Execute(pack)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// After auto-expansion, output_type should be edit_ir
	if angel.OutputType != "edit_ir" {
		t.Errorf("OutputType = %q, want edit_ir (auto-expanded)", angel.OutputType)
	}
	if angel.EditIR == nil {
		t.Fatal("EditIR should not be nil after expansion")
	}
	if len(angel.EditIR.Ops) != 2 {
		t.Errorf("EditIR.Ops = %d, want 2", len(angel.EditIR.Ops))
	}
	if !usage.Success {
		t.Error("should succeed")
	}
}
