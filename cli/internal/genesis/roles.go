package genesis

// GenesisRole defines the three model assignment roles in Genesis SSMP.
type GenesisRole string

const (
	// RoleGod is the orchestrator/planner role.
	RoleGod GenesisRole = "god"
	// RoleAngel is the LLM worker role.
	RoleAngel GenesisRole = "angel"
	// RoleOracle is the escalation advisor role.
	RoleOracle GenesisRole = "oracle"
)

// AllRoles returns all defined Genesis roles in display order.
func AllRoles() []GenesisRole {
	return []GenesisRole{RoleGod, RoleAngel, RoleOracle}
}

// RoleDisplayName returns a human-friendly name for the role.
func RoleDisplayName(r GenesisRole) string {
	switch r {
	case RoleGod:
		return "God (Orchestrator)"
	case RoleAngel:
		return "Angel (Worker)"
	case RoleOracle:
		return "Oracle (Advisor)"
	default:
		return string(r)
	}
}
