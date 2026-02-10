package god

import "time"

// Mission represents a unit of work within a mission DAG.
type Mission struct {
	MissionID   string   `json:"mission_id"`
	Goal        string   `json:"goal"`
	BaseRev     string   `json:"base_rev"`
	Scopes      []Scope  `json:"scopes"`
	LeaseIDs    []string `json:"lease_ids"`
	Tasks       []string `json:"tasks"`
	TokenBudget int      `json:"token_budget"`
	CreatedAt   string   `json:"created_at"`
}

// Scope identifies a symbol or file scope for leasing.
type Scope struct {
	ScopeType  string `json:"scope_type"`
	ScopeValue string `json:"scope_value"`
}

// DAGNode is a node in the mission DAG with dependencies.
type DAGNode struct {
	Mission   Mission  `json:"mission"`
	DependsOn []string `json:"depends_on"` // mission_ids this node depends on
}

// MissionDAG is the full plan produced by God.
type MissionDAG struct {
	PlanID    string    `json:"plan_id"`
	TaskDesc  string    `json:"task_desc"`
	RepoPath  string    `json:"repo_path"`
	Nodes     []DAGNode `json:"nodes"`
	CreatedAt string    `json:"created_at"`
}

// NewMissionDAG creates an empty DAG for the given task.
func NewMissionDAG(planID, taskDesc, repoPath string) *MissionDAG {
	return &MissionDAG{
		PlanID:    planID,
		TaskDesc:  taskDesc,
		RepoPath:  repoPath,
		Nodes:     []DAGNode{},
		CreatedAt: nowFunc().UTC().Format(time.RFC3339),
	}
}

// AddNode adds a mission node to the DAG.
func (dag *MissionDAG) AddNode(node DAGNode) {
	dag.Nodes = append(dag.Nodes, node)
}

// MissionIDs returns all mission IDs in the DAG.
func (dag *MissionDAG) MissionIDs() []string {
	ids := make([]string, len(dag.Nodes))
	for i, n := range dag.Nodes {
		ids[i] = n.Mission.MissionID
	}
	return ids
}

// Roots returns nodes with no dependencies (entry points).
func (dag *MissionDAG) Roots() []DAGNode {
	var roots []DAGNode
	for _, n := range dag.Nodes {
		if len(n.DependsOn) == 0 {
			roots = append(roots, n)
		}
	}
	return roots
}
