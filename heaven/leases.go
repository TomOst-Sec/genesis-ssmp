package heaven

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// Lease represents an exclusive scope lock held by an Angel.
type Lease struct {
	LeaseID    string `json:"lease_id"`
	OwnerID    string `json:"owner_id"`
	MissionID  string `json:"mission_id"`
	ScopeType  string `json:"scope_type"`
	ScopeValue string `json:"scope_value"`
	IssuedAt   string `json:"issued_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// LeaseManager manages in-memory exclusive leases, persisted via EventLog.
type LeaseManager struct {
	mu     sync.RWMutex
	leases map[string]*Lease // lease_id -> Lease
	scopes map[string]string // "scope_type:scope_value" -> lease_id (active index)
	events *EventLog
}

// NewLeaseManager creates a LeaseManager and replays events to rebuild state.
func NewLeaseManager(events *EventLog) (*LeaseManager, error) {
	lm := &LeaseManager{
		leases: make(map[string]*Lease),
		scopes: make(map[string]string),
		events: events,
	}
	if err := lm.replayEvents(); err != nil {
		return nil, fmt.Errorf("lease manager init: %w", err)
	}
	return lm, nil
}

func (lm *LeaseManager) replayEvents() error {
	all, err := lm.events.All()
	if err != nil {
		return err
	}
	for _, raw := range all {
		var evt struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &evt) != nil {
			continue
		}
		switch evt.Type {
		case "lease_issued":
			var le struct {
				Lease Lease `json:"lease"`
			}
			if json.Unmarshal(raw, &le) == nil {
				lm.leases[le.Lease.LeaseID] = &le.Lease
				key := scopeKey(le.Lease.ScopeType, le.Lease.ScopeValue)
				lm.scopes[key] = le.Lease.LeaseID
			}
		case "lease_released":
			var le struct {
				LeaseID string `json:"lease_id"`
			}
			if json.Unmarshal(raw, &le) == nil {
				if existing, ok := lm.leases[le.LeaseID]; ok {
					key := scopeKey(existing.ScopeType, existing.ScopeValue)
					delete(lm.scopes, key)
					delete(lm.leases, le.LeaseID)
				}
			}
		}
	}
	return nil
}

// AcquireRequest is the input for lease acquisition.
type AcquireRequest struct {
	OwnerID   string        `json:"owner_id"`
	MissionID string        `json:"mission_id"`
	Scopes    []ScopeTarget `json:"scopes"`
}

// ScopeTarget identifies a scope to lease.
type ScopeTarget struct {
	ScopeType  string `json:"scope_type"`
	ScopeValue string `json:"scope_value"`
}

// AcquireResult is the output of lease acquisition.
type AcquireResult struct {
	Acquired []Lease  `json:"acquired"`
	Denied   []string `json:"denied"`
}

// Acquire attempts to exclusively lease the requested scopes.
// If any scope is already held by a different owner, that scope is denied.
func (lm *LeaseManager) Acquire(req AcquireRequest) (AcquireResult, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := nowFunc().UTC().Format(time.RFC3339)
	var result AcquireResult

	for _, scope := range req.Scopes {
		key := scopeKey(scope.ScopeType, scope.ScopeValue)
		if existingID, held := lm.scopes[key]; held {
			existing := lm.leases[existingID]
			if existing != nil && existing.OwnerID != req.OwnerID {
				result.Denied = append(result.Denied, key)
				continue
			}
			// Same owner already holds it — skip, idempotent
			result.Acquired = append(result.Acquired, *existing)
			continue
		}

		lease := Lease{
			LeaseID:    generateID(),
			OwnerID:    req.OwnerID,
			MissionID:  req.MissionID,
			ScopeType:  scope.ScopeType,
			ScopeValue: scope.ScopeValue,
			IssuedAt:   now,
		}
		lm.leases[lease.LeaseID] = &lease
		lm.scopes[key] = lease.LeaseID
		result.Acquired = append(result.Acquired, lease)

		// Log event
		evt, _ := json.Marshal(map[string]any{
			"type":  "lease_issued",
			"lease": lease,
		})
		lm.events.Append(json.RawMessage(evt))
	}

	if result.Acquired == nil {
		result.Acquired = []Lease{}
	}
	if result.Denied == nil {
		result.Denied = []string{}
	}
	return result, nil
}

// Release releases the given lease IDs.
func (lm *LeaseManager) Release(leaseIDs []string) (int, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	released := 0
	for _, id := range leaseIDs {
		existing, ok := lm.leases[id]
		if !ok {
			continue
		}
		key := scopeKey(existing.ScopeType, existing.ScopeValue)
		delete(lm.scopes, key)
		delete(lm.leases, id)
		released++

		evt, _ := json.Marshal(map[string]any{
			"type":     "lease_released",
			"lease_id": id,
		})
		lm.events.Append(json.RawMessage(evt))
	}
	return released, nil
}

// List returns all active leases, or all leases if activeOnly is false.
func (lm *LeaseManager) List(activeOnly bool) []Lease {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	result := make([]Lease, 0, len(lm.leases))
	for _, l := range lm.leases {
		result = append(result, *l)
	}
	return result
}

// ActiveCount returns the number of active leases.
func (lm *LeaseManager) ActiveCount() int {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return len(lm.leases)
}

// OwnerHoldsScope checks if the given owner holds a lease on the scope.
func (lm *LeaseManager) OwnerHoldsScope(ownerID, scopeType, scopeValue string) bool {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	key := scopeKey(scopeType, scopeValue)
	leaseID, ok := lm.scopes[key]
	if !ok {
		return false
	}
	l, ok := lm.leases[leaseID]
	return ok && l.OwnerID == ownerID
}

func scopeKey(scopeType, scopeValue string) string {
	return scopeType + ":" + scopeValue
}

var generateIDFunc = defaultGenerateID

func defaultGenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateID() string { return generateIDFunc() }
