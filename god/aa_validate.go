package god

import (
	"fmt"
	"time"
)

// ValidateMission checks a Mission struct against the proto/mission.schema.json
// required fields at runtime.
func ValidateMission(m Mission) error {
	if m.MissionID == "" {
		return fmt.Errorf("validate mission: mission_id is required")
	}
	if m.BaseRev == "" {
		return fmt.Errorf("validate mission: base_rev is required")
	}
	if m.Goal == "" {
		return fmt.Errorf("validate mission: goal is required")
	}
	if m.Tasks == nil || len(m.Tasks) == 0 {
		return fmt.Errorf("validate mission: tasks must be non-nil and non-empty")
	}
	if m.TokenBudget < 0 {
		return fmt.Errorf("validate mission: token_budget must be >= 0")
	}
	if m.CreatedAt == "" {
		return fmt.Errorf("validate mission: created_at is required")
	}
	if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
		return fmt.Errorf("validate mission: created_at must be valid RFC3339: %w", err)
	}
	return nil
}
