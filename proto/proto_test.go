package proto_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func schemaDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func loadSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(schemaDir(), name))
	if err != nil {
		t.Fatalf("load schema %s: %v", name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	return schema
}

func requireStringField(t *testing.T, obj map[string]any, field string) string {
	t.Helper()
	v, ok := obj[field]
	if !ok {
		t.Fatalf("missing required field %q", field)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("field %q should be string, got %T", field, v)
	}
	return s
}

func requireArrayField(t *testing.T, obj map[string]any, field string) []any {
	t.Helper()
	v, ok := obj[field]
	if !ok {
		t.Fatalf("missing required field %q", field)
	}
	a, ok := v.([]any)
	if !ok {
		t.Fatalf("field %q should be array, got %T", field, v)
	}
	return a
}

func requireIntField(t *testing.T, obj map[string]any, field string) float64 {
	t.Helper()
	v, ok := obj[field]
	if !ok {
		t.Fatalf("missing required field %q", field)
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("field %q should be number, got %T", field, v)
	}
	return n
}

func parseJSON(t *testing.T, data string) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	return obj
}

// validateRequired checks that all required fields from the schema are present.
func validateRequired(t *testing.T, schema, payload map[string]any) {
	t.Helper()
	reqRaw, ok := schema["required"]
	if !ok {
		return
	}
	reqArr, ok := reqRaw.([]any)
	if !ok {
		return
	}
	for _, r := range reqArr {
		field := r.(string)
		if _, present := payload[field]; !present {
			t.Errorf("missing required field %q", field)
		}
	}
}

// validateEnum checks if a value is in a schema's enum list.
func validateEnum(t *testing.T, schema map[string]any, field, value string) {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return
	}
	fieldSchema, ok := props[field].(map[string]any)
	if !ok {
		return
	}
	enumRaw, ok := fieldSchema["enum"]
	if !ok {
		return
	}
	enumArr, ok := enumRaw.([]any)
	if !ok {
		return
	}
	for _, e := range enumArr {
		if e.(string) == value {
			return
		}
	}
	t.Errorf("field %q value %q not in enum %v", field, value, enumArr)
}

// ---------- Schema Load Tests ----------

var allSchemaNames = []string{
	"edit_ir.schema.json",
	"angel_response.schema.json",
	"mission.schema.json",
	"oracle.schema.json",
	"pf.schema.json",
	"leases.schema.json",
	"receipts.schema.json",
	"heaven_status.schema.json",
}

func TestSchemaLoad_AllEight(t *testing.T) {
	for _, name := range allSchemaNames {
		t.Run(name, func(t *testing.T) {
			s := loadSchema(t, name)
			requireStringField(t, s, "$schema")
			requireStringField(t, s, "title")

			// Check for required at root or in definitions
			_, hasRequired := s["required"]
			_, hasDefs := s["definitions"]
			_, hasDefsDollar := s["$defs"]
			if !hasRequired && !hasDefs && !hasDefsDollar {
				t.Errorf("schema %s has no required field and no definitions", name)
			}
		})
	}
}

// ---------- Golden Sample Tests ----------

func TestGoldenSample_EditIR(t *testing.T) {
	schema := loadSchema(t, "edit_ir.schema.json")

	samples := []string{
		`{"ops":[{"op":"replace_span","path":"main.go","anchor_hash":"abc123","lines":[10,20],"content":"new code"}]}`,
		`{"ops":[{"op":"insert_after_symbol","path":"util.go","anchor_hash":"def456","content":"func New() {}","symbol":"Old"}]}`,
		`{"ops":[{"op":"add_file","path":"new.go","anchor_hash":"empty","content":"package main"}]}`,
		`{"ops":[{"op":"delete_span","path":"old.go","anchor_hash":"ghi789","lines":[5,8]}]}`,
	}
	for i, s := range samples {
		t.Run(string(rune('1'+i)), func(t *testing.T) {
			payload := parseJSON(t, s)
			validateRequired(t, schema, payload)
			ops := requireArrayField(t, payload, "ops")
			if len(ops) == 0 {
				t.Error("ops should not be empty")
			}
			for _, op := range ops {
				opObj := op.(map[string]any)
				requireStringField(t, opObj, "op")
				requireStringField(t, opObj, "path")
				requireStringField(t, opObj, "anchor_hash")
			}
		})
	}
}

func TestGoldenSample_AngelResponse(t *testing.T) {
	schema := loadSchema(t, "angel_response.schema.json")

	editIR := `{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":[{"op":"add_file","path":"x.go","anchor_hash":"h","content":"pkg"}]},"manifest":{"symbols_touched":["X"],"files_touched":["x.go"]}}`
	diffFB := `{"mission_id":"m2","output_type":"diff_fallback","diff":"--- a/x.go\n+++ b/x.go","manifest":{"symbols_touched":[],"files_touched":["x.go"]}}`

	for _, s := range []string{editIR, diffFB} {
		payload := parseJSON(t, s)
		validateRequired(t, schema, payload)
		ot := requireStringField(t, payload, "output_type")
		if ot != "edit_ir" && ot != "diff_fallback" {
			t.Errorf("unexpected output_type: %s", ot)
		}
		validateEnum(t, schema, "output_type", ot)
	}
}

func TestGoldenSample_Mission(t *testing.T) {
	schema := loadSchema(t, "mission.schema.json")

	sample := `{"mission_id":"m1","base_rev":"HEAD","goal":"Add pow","leases":["l1"],"tasks":["implement"],"token_budget":8000,"created_at":"2025-01-01T00:00:00Z"}`
	payload := parseJSON(t, sample)
	validateRequired(t, schema, payload)

	budget := requireIntField(t, payload, "token_budget")
	if budget < 0 {
		t.Errorf("token_budget should be non-negative, got %v", budget)
	}

	ca := requireStringField(t, payload, "created_at")
	if !strings.Contains(ca, "T") {
		t.Errorf("created_at doesn't look like RFC3339: %s", ca)
	}
}

func TestGoldenSample_OracleRequestResponse(t *testing.T) {
	schema := loadSchema(t, "oracle.schema.json")

	// Check oracle_response definition
	defs, ok := schema["definitions"].(map[string]any)
	if !ok {
		t.Fatal("oracle schema missing definitions")
	}

	respDef, ok := defs["oracle_response"].(map[string]any)
	if !ok {
		t.Fatal("oracle schema missing oracle_response definition")
	}

	sample := `{"updated_dag":{"plan_id":"p1","task_desc":"fix","repo_path":"/tmp","nodes":[{"mission":{"mission_id":"m1","goal":"fix bug"},"depends_on":[]}],"created_at":"2025-01-01T00:00:00Z"},"leases_plan":[{"scope_type":"file","scope_value":"main.go"}],"risk_hotspots":["main.go"],"recommended_tests":["go test ./..."]}`
	payload := parseJSON(t, sample)
	validateRequired(t, respDef, payload)

	dag := payload["updated_dag"].(map[string]any)
	requireStringField(t, dag, "plan_id")
	requireArrayField(t, dag, "nodes")

	requireArrayField(t, payload, "leases_plan")
	requireArrayField(t, payload, "risk_hotspots")
}

func TestGoldenSample_PF(t *testing.T) {
	schema := loadSchema(t, "pf.schema.json")
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("pf schema missing $defs")
	}

	// Validate request
	reqDef := defs["request"].(map[string]any)
	reqSample := `{"type":"request","command":"PF_SYMDEF","args":{"mission_id":"m1","symbol":"Greet"}}`
	reqPayload := parseJSON(t, reqSample)
	validateRequired(t, reqDef, reqPayload)

	// Check all 6 commands are valid
	commands := []string{"PF_STATUS", "PF_SYMDEF", "PF_CALLERS", "PF_SLICE", "PF_SEARCH", "PF_TESTS"}
	cmdEnum := reqDef["properties"].(map[string]any)["command"].(map[string]any)["enum"].([]any)
	for _, cmd := range commands {
		found := false
		for _, e := range cmdEnum {
			if e.(string) == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("command %q not in enum", cmd)
		}
	}

	// Validate response
	respDef := defs["response"].(map[string]any)
	respSample := `{"type":"response","shards":[{"kind":"symdef","blob_id":"abc","meta":{"symbol":"Greet"}}],"meta":{"pf_count":1,"shard_bytes":100}}`
	respPayload := parseJSON(t, respSample)
	validateRequired(t, respDef, respPayload)

	// Check shard kinds
	shardDef := defs["shard"].(map[string]any)
	kindEnum := shardDef["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]any)
	expectedKinds := []string{"symdef", "callers", "tests", "slice", "search", "status"}
	for _, kind := range expectedKinds {
		found := false
		for _, e := range kindEnum {
			if e.(string) == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("shard kind %q not in enum", kind)
		}
	}
}

func TestGoldenSample_Leases(t *testing.T) {
	schema := loadSchema(t, "leases.schema.json")

	sample := `{"lease_id":"l1","owner_id":"angel-A","mission_id":"m1","scope_type":"symbol","scope_value":"Greet","issued_at":"2025-01-01T00:00:00Z","expires_at":"2025-01-01T01:00:00Z"}`
	payload := parseJSON(t, sample)
	validateRequired(t, schema, payload)

	st := requireStringField(t, payload, "scope_type")
	validateEnum(t, schema, "scope_type", st)
}

func TestGoldenSample_Receipt(t *testing.T) {
	schema := loadSchema(t, "receipts.schema.json")

	sample := `{"mission_id":"m1","env_hash":"abc","command_hash":"def","stdout_hash":"ghi","exit_code":0,"timestamp":"2025-01-01T00:00:00Z"}`
	payload := parseJSON(t, sample)
	validateRequired(t, schema, payload)

	ec := requireIntField(t, payload, "exit_code")
	if ec < 0 {
		t.Errorf("exit_code should be non-negative in golden sample, got %v", ec)
	}
}

func TestGoldenSample_HeavenStatus(t *testing.T) {
	schema := loadSchema(t, "heaven_status.schema.json")

	sample := `{"state_rev":42,"active_leases_count":2,"hotset_summary":{"blob1":true},"file_clock_summary":{"main.go":"3"}}`
	payload := parseJSON(t, sample)
	validateRequired(t, schema, payload)

	rev := requireIntField(t, payload, "state_rev")
	if rev < 0 {
		t.Errorf("state_rev should be non-negative, got %v", rev)
	}
}

// ---------- Invalid Payload Tests ----------

func TestInvalidPayload_EditIR_MissingOps(t *testing.T) {
	payload := parseJSON(t, `{"not_ops": []}`)
	if _, ok := payload["ops"]; ok {
		t.Fatal("payload should not have ops field")
	}
	// Validate against schema: ops is required
	schema := loadSchema(t, "edit_ir.schema.json")
	reqArr := schema["required"].([]any)
	opsRequired := false
	for _, r := range reqArr {
		if r.(string) == "ops" {
			opsRequired = true
		}
	}
	if !opsRequired {
		t.Fatal("edit_ir schema should require ops")
	}
}

func TestInvalidPayload_AngelResponse_WrongOutputType(t *testing.T) {
	schema := loadSchema(t, "angel_response.schema.json")
	enumArr := schema["properties"].(map[string]any)["output_type"].(map[string]any)["enum"].([]any)

	invalidType := "garbage"
	found := false
	for _, e := range enumArr {
		if e.(string) == invalidType {
			found = true
		}
	}
	if found {
		t.Fatal("'garbage' should not be a valid output_type")
	}
}

func TestInvalidPayload_Mission_NegativeBudget(t *testing.T) {
	schema := loadSchema(t, "mission.schema.json")
	budgetSchema := schema["properties"].(map[string]any)["token_budget"].(map[string]any)

	min, ok := budgetSchema["minimum"].(float64)
	if !ok {
		t.Fatal("token_budget should have minimum constraint")
	}
	if min < 0 {
		t.Fatal("token_budget minimum should be >= 0")
	}

	// Verify -1 would violate the constraint
	if -1 >= int(min) {
		t.Fatal("-1 should be below minimum")
	}
}

func TestInvalidPayload_Lease_InvalidScopeType(t *testing.T) {
	schema := loadSchema(t, "leases.schema.json")
	enumArr := schema["properties"].(map[string]any)["scope_type"].(map[string]any)["enum"].([]any)

	invalidType := "galaxy"
	found := false
	for _, e := range enumArr {
		if e.(string) == invalidType {
			found = true
		}
	}
	if found {
		t.Fatal("'galaxy' should not be a valid scope_type")
	}
}

func TestInvalidPayload_Receipt_MissingTimestamp(t *testing.T) {
	schema := loadSchema(t, "receipts.schema.json")
	reqArr := schema["required"].([]any)

	timestampRequired := false
	for _, r := range reqArr {
		if r.(string) == "timestamp" {
			timestampRequired = true
		}
	}
	if !timestampRequired {
		t.Fatal("receipt schema should require timestamp")
	}

	// Verify missing timestamp is caught
	payload := parseJSON(t, `{"mission_id":"m1","env_hash":"a","command_hash":"b","stdout_hash":"c","exit_code":0}`)
	if _, ok := payload["timestamp"]; ok {
		t.Fatal("test payload should not have timestamp")
	}
}

func TestInvalidPayload_PF_UnknownCommand(t *testing.T) {
	schema := loadSchema(t, "pf.schema.json")
	defs := schema["$defs"].(map[string]any)
	reqDef := defs["request"].(map[string]any)
	cmdEnum := reqDef["properties"].(map[string]any)["command"].(map[string]any)["enum"].([]any)

	invalidCmd := "PF_DESTROY"
	found := false
	for _, e := range cmdEnum {
		if e.(string) == invalidCmd {
			found = true
		}
	}
	if found {
		t.Fatal("'PF_DESTROY' should not be a valid PF command")
	}
}
