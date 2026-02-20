package god

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/genesis-ssmp/genesis/internal/lean"
)

// OracleRequest is the state vector sent to the Cloud God Oracle.
// Contains only IDs and summaries — no repo dump.
type OracleRequest struct {
	SpecBlobID            string          `json:"spec_blob_id"`
	DecisionLedgerBlobID  string          `json:"decision_ledger_blob_id,omitempty"`
	FailingTestBlobID     string          `json:"failing_test_blob_id,omitempty"`
	SymbolShortlist       []string        `json:"symbol_shortlist"`
	MetricsSummary        *MissionMetrics `json:"metrics_summary"`
}

// OracleResponse is the structured output from the Cloud God Oracle.
type OracleResponse struct {
	UpdatedDAG       *MissionDAG `json:"updated_dag"`
	LeasesPlan       []Scope     `json:"leases_plan"`
	RiskHotspots     []string    `json:"risk_hotspots"`
	RecommendedTests []string    `json:"recommended_tests"`
}

// OracleConfig controls the oracle escalation behavior.
type OracleConfig struct {
	Enabled  bool     // whether oracle escalation is active
	Provider Provider // the LLM provider to use for oracle calls
}

// Oracle handles escalation to the Cloud God Oracle when thrash is detected.
type Oracle struct {
	config  OracleConfig
	heaven  *HeavenClient
	adapter *ProviderAdapter
}

// NewOracle creates an Oracle with the given configuration.
func NewOracle(config OracleConfig, heaven *HeavenClient) *Oracle {
	o := &Oracle{
		config: config,
		heaven: heaven,
	}
	if config.Provider != nil {
		o.adapter = NewProviderAdapter(config.Provider)
	}
	return o
}

// Escalate calls the Cloud God Oracle with the given state vector and returns
// the oracle's recommended plan. Returns nil, nil if the oracle is disabled.
func (o *Oracle) Escalate(req OracleRequest) (*OracleResponse, error) {
	if !o.config.Enabled || o.adapter == nil {
		return nil, nil
	}

	// Build the oracle prompt as a mission pack
	pack := buildOraclePrompt(req)

	// Send to provider
	raw, err := o.config.Provider.Send(pack)
	if err != nil {
		return nil, fmt.Errorf("oracle: provider send: %w", err)
	}

	// Parse and validate response
	resp, err := parseOracleResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("oracle: parse response: %w", err)
	}

	if err := validateOracleResponse(resp); err != nil {
		return nil, fmt.Errorf("oracle: validation: %w", err)
	}

	// Log oracle escalation event
	o.heaven.AppendEvent(map[string]any{
		"type":              "oracle_escalation",
		"spec_blob_id":     req.SpecBlobID,
		"new_plan_id":      resp.UpdatedDAG.PlanID,
		"risk_hotspots":    resp.RiskHotspots,
		"recommended_tests": resp.RecommendedTests,
		"leases_count":     len(resp.LeasesPlan),
		"nodes_count":      len(resp.UpdatedDAG.Nodes),
	})

	return resp, nil
}

// EscalateOnThrash checks if a mission is thrashing and if so, calls the oracle
// to get an updated plan. Returns the new DAG or nil if no escalation occurred.
func (o *Oracle) EscalateOnThrash(missionID string, metrics *MissionMetrics, specBlobID string) (*OracleResponse, error) {
	if metrics == nil || metrics.Status != "thrashing" {
		return nil, nil
	}

	// Build symbol shortlist from metrics context
	symbolShortlist := []string{}

	req := OracleRequest{
		SpecBlobID:      specBlobID,
		SymbolShortlist: symbolShortlist,
		MetricsSummary:  metrics,
	}

	return o.Escalate(req)
}

// oraclePromptTemplate is the system prompt that forces JSON output from the oracle.
const oraclePromptTemplate = `You are the Cloud God Oracle for GENESIS SSMP.
A mission has entered THRASHING state and requires your intervention.

You will receive a state vector containing:
- The original task specification (spec_blob_id)
- Recent decision history (decision_ledger_blob_id)
- Failing test output (failing_test_blob_id)
- Relevant symbols (symbol_shortlist)
- Mission metrics showing the thrashing pattern (metrics_summary)

Your job is to analyze the thrashing pattern and produce a corrected mission plan.

RESPOND WITH EXACTLY ONE JSON OBJECT matching this schema:
{
  "updated_dag": {
    "plan_id": "<new unique plan ID>",
    "task_desc": "<updated task description>",
    "repo_path": "<repo path>",
    "nodes": [
      {
        "mission": {
          "mission_id": "<unique ID>",
          "goal": "<goal>",
          "base_rev": "HEAD",
          "scopes": [{"scope_type": "symbol|file", "scope_value": "<name>"}],
          "lease_ids": [],
          "tasks": ["<task>"],
          "token_budget": 8000,
          "created_at": "<RFC3339>"
        },
        "depends_on": []
      }
    ],
    "created_at": "<RFC3339>"
  },
  "leases_plan": [{"scope_type": "symbol|file", "scope_value": "<name>"}],
  "risk_hotspots": ["<file_path_or_symbol>"],
  "recommended_tests": ["<test_command_or_selector>"]
}

Do NOT include any text outside the JSON object. Output ONLY valid JSON.`

// tslnFormatNote is appended to the oracle prompt when lean mode is active.
const tslnFormatNote = `
METRICS FORMAT: TSLN pipe-delimited. Schema header has field names. +N = delta, = = repeat.`

// buildOraclePrompt creates a MissionPack containing the oracle prompt and state vector.
func buildOraclePrompt(req OracleRequest) *MissionPack {
	var stateStr string
	var stateBytes []byte

	if lean.Enabled() && req.MetricsSummary != nil {
		// Encode metrics as TSLN; keep other fields as JSON envelope
		metricsRow := metricsToLeanRow(req.MetricsSummary)
		tslnData, err := lean.EncodeMetricsRow(metricsRow)
		if err == nil {
			envelope := map[string]any{
				"spec_blob_id":            req.SpecBlobID,
				"decision_ledger_blob_id": req.DecisionLedgerBlobID,
				"failing_test_blob_id":    req.FailingTestBlobID,
				"symbol_shortlist":        req.SymbolShortlist,
			}
			envJSON, _ := json.Marshal(envelope)
			stateStr = string(envJSON) + "\n\n--- METRICS (TSLN) ---\n" + string(tslnData)
			stateBytes = []byte(stateStr)
		}
	}

	if stateStr == "" {
		// Fallback: original JSON path
		stateBytes, _ = json.Marshal(req)
		stateStr = string(stateBytes)
	}

	header := oraclePromptTemplate
	if lean.Enabled() {
		header += tslnFormatNote
	}
	header += "\n\n--- STATE VECTOR ---\n" + stateStr

	return &MissionPack{
		Header: header,
		Mission: Mission{
			MissionID:   "oracle-" + genID(),
			Goal:        "Resolve thrashing mission",
			BaseRev:     "HEAD",
			Tasks:       []string{"oracle_resolution"},
			TokenBudget: 16000,
			CreatedAt:   nowFunc().UTC().Format(time.RFC3339),
		},
		BudgetMeta: BudgetMeta{
			TokenBudget: 16000,
			TotalTokens: EstimateTokens(stateBytes) + EstimateTokens([]byte(oraclePromptTemplate)),
		},
	}
}

// metricsToLeanRow converts a MissionMetrics to a lean.MetricsRow.
func metricsToLeanRow(m *MissionMetrics) lean.MetricsRow {
	ts := nowFunc().UTC()
	if m.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, m.StartedAt); err == nil {
			ts = t
		}
	}
	return lean.MetricsRow{
		Timestamp:        ts,
		MissionID:        m.MissionID,
		Status:           m.Status,
		PFCount:          m.PFCount,
		PFResponseSize:   m.PFResponseSize,
		Retries:          m.Retries,
		Rejects:          m.Rejects,
		Conflicts:        m.Conflicts,
		TestFailures:     m.TestFailures,
		TokensIn:         m.TokensIn,
		TokensOut:        m.TokensOut,
		Turns:            m.Turns,
		ElapsedMS:        m.ElapsedMS,
		PhaseTransitions: m.PhaseTransitions,
	}
}

// parseOracleResponse unmarshals the raw provider response into an OracleResponse.
func parseOracleResponse(raw []byte) (*OracleResponse, error) {
	var resp OracleResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &resp, nil
}

// validateOracleResponse checks that an OracleResponse has all required fields.
func validateOracleResponse(resp *OracleResponse) error {
	if resp.UpdatedDAG == nil {
		return fmt.Errorf("missing updated_dag")
	}
	if resp.UpdatedDAG.PlanID == "" {
		return fmt.Errorf("updated_dag.plan_id is empty")
	}
	if resp.UpdatedDAG.Nodes == nil {
		return fmt.Errorf("updated_dag.nodes must not be nil")
	}
	if len(resp.UpdatedDAG.Nodes) == 0 {
		return fmt.Errorf("updated_dag.nodes must not be empty")
	}

	// Validate each node has a mission_id and goal
	for i, node := range resp.UpdatedDAG.Nodes {
		if node.Mission.MissionID == "" {
			return fmt.Errorf("updated_dag.nodes[%d].mission.mission_id is empty", i)
		}
		if node.Mission.Goal == "" {
			return fmt.Errorf("updated_dag.nodes[%d].mission.goal is empty", i)
		}
	}

	// Check for cyclic dependencies
	if err := detectCycle(resp.UpdatedDAG.Nodes); err != nil {
		return err
	}

	if resp.LeasesPlan == nil {
		return fmt.Errorf("missing leases_plan")
	}
	if resp.RiskHotspots == nil {
		return fmt.Errorf("missing risk_hotspots")
	}
	if resp.RecommendedTests == nil {
		return fmt.Errorf("missing recommended_tests")
	}

	return nil
}

// detectCycle returns an error if the DAG nodes contain a dependency cycle.
func detectCycle(nodes []DAGNode) error {
	// Build adjacency map: mission_id -> depends_on
	deps := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		deps[n.Mission.MissionID] = n.DependsOn
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // finished
	)
	color := make(map[string]int, len(nodes))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for _, dep := range deps[id] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("cyclic dependency detected: %s -> %s", id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, n := range nodes {
		id := n.Mission.MissionID
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}
