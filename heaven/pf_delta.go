package heaven

import (
	"fmt"
	"sync"
)

// ShardVersion tracks the content hash of a shard previously served to a mission.
type ShardVersion struct {
	BlobID string `json:"blob_id"`
	Key    string `json:"key"`
}

// DeltaTracker maintains per-mission shard version history.
// When a shard is requested again with the same content, an "unchanged"
// sentinel is returned instead of the full content.
type DeltaTracker struct {
	mu       sync.Mutex
	missions map[string]map[string]ShardVersion // mission_id -> key -> version
}

// NewDeltaTracker creates a new delta tracker.
func NewDeltaTracker() *DeltaTracker {
	return &DeltaTracker{
		missions: make(map[string]map[string]ShardVersion),
	}
}

// CheckAndUpdate checks each shard against the mission's version history.
// Shards with unchanged BlobIDs are replaced with "unchanged" sentinels.
// Returns the (potentially modified) shards and the count of unchanged hits.
func (dt *DeltaTracker) CheckAndUpdate(missionID string, command string, args PFArgs, shards []Shard) ([]Shard, int) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	if dt.missions[missionID] == nil {
		dt.missions[missionID] = make(map[string]ShardVersion)
	}
	versions := dt.missions[missionID]

	result := make([]Shard, 0, len(shards))
	deltaHits := 0

	for _, s := range shards {
		key := shardKey(s.Kind, command, args)
		prev, exists := versions[key]

		if exists && prev.BlobID == s.BlobID {
			// Content unchanged — return sentinel
			deltaHits++
			result = append(result, Shard{
				Kind:   "unchanged",
				BlobID: s.BlobID,
				Meta: map[string]any{
					"original_kind": s.Kind,
					"key":           key,
				},
			})
		} else {
			// New or changed — serve full shard and record
			versions[key] = ShardVersion{BlobID: s.BlobID, Key: key}
			result = append(result, s)
		}
	}

	return result, deltaHits
}

// shardKey generates a deterministic key for a shard based on its kind and args.
func shardKey(kind, command string, args PFArgs) string {
	switch command {
	case "PF_SYMDEF":
		return "symdef:" + args.Symbol
	case "PF_CALLERS":
		return "callers:" + args.Symbol
	case "PF_SLICE":
		return fmt.Sprintf("slice:%s:%d:%d", args.Path, args.StartLine, args.N)
	case "PF_SEARCH":
		return "search:" + args.Query
	case "PF_TESTS":
		return "tests:" + args.Symbol
	default:
		return kind + ":" + command + ":" + args.MissionID
	}
}
