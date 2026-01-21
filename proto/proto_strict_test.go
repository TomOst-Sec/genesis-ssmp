package proto_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Cross-schema reference validation: oracle response.updated_dag must
// contain nodes whose missions satisfy mission.schema required fields.
// ---------------------------------------------------------------------------

func TestCrossSchemaOracleDAGMatchesMission(t *testing.T) {
	missionSchema := loadSchema(t, "mission.schema.json")
	missionRequired := missionSchema["required"].([]any)

	// Build a valid oracle response with an embedded DAG
	oracleResp := `{
		"updated_dag": {
			"plan_id": "p1",
			"task_desc": "fix issue",
			"repo_path": "/tmp",
			"nodes": [{
				"mission": {
					"mission_id": "m1",
					"base_rev": "HEAD",
					"goal": "Fix the bug",
					"leases": ["l1"],
					"tasks": ["implement"],
					"token_budget": 8000,
					"created_at": "2025-01-01T00:00:00Z"
				},
				"depends_on": []
			}],
			"created_at": "2025-01-01T00:00:00Z"
		},
		"leases_plan": [{"scope_type": "file", "scope_value": "main.go"}],
		"risk_hotspots": ["main.go"],
		"recommended_tests": ["go test ./..."]
	}`

	var resp map[string]any
	if err := json.Unmarshal([]byte(oracleResp), &resp); err != nil {
		t.Fatalf("parse oracle response: %v", err)
	}

	dag := resp["updated_dag"].(map[string]any)
	nodes := dag["nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 node in DAG")
	}

	for i, node := range nodes {
		nodeObj := node.(map[string]any)
		mission := nodeObj["mission"].(map[string]any)

		// Verify every required field from mission.schema is present
		for _, req := range missionRequired {
			field := req.(string)
			if _, ok := mission[field]; !ok {
				t.Errorf("node[%d] mission missing required field %q from mission.schema", i, field)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Cross-schema: oracle DAG node missing required mission field detected
// ---------------------------------------------------------------------------

func TestCrossSchemaOracleDAGMissingMissionField(t *testing.T) {
	missionSchema := loadSchema(t, "mission.schema.json")
	missionRequired := missionSchema["required"].([]any)

	// Mission is missing "base_rev"
	oracleResp := `{
		"updated_dag": {
			"plan_id": "p1",
			"task_desc": "fix",
			"repo_path": "/tmp",
			"nodes": [{
				"mission": {
					"mission_id": "m1",
					"goal": "Fix bug",
					"leases": [],
					"tasks": [],
					"token_budget": 8000,
					"created_at": "2025-01-01T00:00:00Z"
				},
				"depends_on": []
			}],
			"created_at": "2025-01-01T00:00:00Z"
		},
		"leases_plan": [],
		"risk_hotspots": [],
		"recommended_tests": []
	}`

	var resp map[string]any
	json.Unmarshal([]byte(oracleResp), &resp)
	dag := resp["updated_dag"].(map[string]any)
	nodes := dag["nodes"].([]any)
	mission := nodes[0].(map[string]any)["mission"].(map[string]any)

	// base_rev must be required and missing
	baseRevRequired := false
	for _, req := range missionRequired {
		if req.(string) == "base_rev" {
			baseRevRequired = true
		}
	}
	if !baseRevRequired {
		t.Fatal("mission.schema should require base_rev")
	}
	if _, ok := mission["base_rev"]; ok {
		t.Fatal("test payload should not have base_rev")
	}
}

// ---------------------------------------------------------------------------
// Strict unknown fields: decoder should detect extra fields
// ---------------------------------------------------------------------------

func TestStrictUnknownFieldsEditIR(t *testing.T) {
	// EditIR with an unknown field "extra_field"
	payload := `{"ops":[{"op":"add_file","path":"x.go","anchor_hash":"h","content":"pkg"}],"extra_field":"should_be_caught"}`

	type StrictEditIR struct {
		Ops []map[string]any `json:"ops"`
	}

	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	var result StrictEditIR
	err := dec.Decode(&result)
	if err == nil {
		t.Error("expected error for unknown field 'extra_field' in EditIR")
	}
}

func TestStrictUnknownFieldsAngelResponse(t *testing.T) {
	// AngelResponse with an unknown field
	payload := `{"mission_id":"m1","output_type":"edit_ir","edit_ir":{"ops":[]},"manifest":{"symbols_touched":[],"files_touched":[]},"rogue_field":true}`

	type StrictAngelResponse struct {
		MissionID  string `json:"mission_id"`
		OutputType string `json:"output_type"`
		EditIR     any    `json:"edit_ir"`
		Manifest   any    `json:"manifest"`
	}

	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	var result StrictAngelResponse
	err := dec.Decode(&result)
	if err == nil {
		t.Error("expected error for unknown field 'rogue_field' in AngelResponse")
	}
}

func TestStrictUnknownFieldsMission(t *testing.T) {
	payload := `{"mission_id":"m1","base_rev":"HEAD","goal":"x","leases":[],"tasks":[],"token_budget":8000,"created_at":"2025-01-01T00:00:00Z","unknown_danger":42}`

	type StrictMission struct {
		MissionID   string   `json:"mission_id"`
		BaseRev     string   `json:"base_rev"`
		Goal        string   `json:"goal"`
		Leases      []string `json:"leases"`
		Tasks       []string `json:"tasks"`
		TokenBudget int      `json:"token_budget"`
		CreatedAt   string   `json:"created_at"`
	}

	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	var result StrictMission
	err := dec.Decode(&result)
	if err == nil {
		t.Error("expected error for unknown field 'unknown_danger' in Mission")
	}
}
