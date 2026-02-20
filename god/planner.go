package god

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"crypto/rand"
	"encoding/hex"
)

// Planner creates mission DAGs by querying Heaven's IR index.
type Planner struct {
	heaven *HeavenClient
}

// NewPlanner creates a Planner backed by the given Heaven client.
func NewPlanner(heaven *HeavenClient) *Planner {
	return &Planner{heaven: heaven}
}

// Plan takes a task description and repo path, queries Heaven IR,
// decomposes the work into missions, acquires leases, and returns a DAG.
func (p *Planner) Plan(taskDesc, repoPath string) (*MissionDAG, error) {
	// Step 1: build/refresh the IR index
	if _, err := p.heaven.IRBuild(repoPath); err != nil {
		return nil, fmt.Errorf("ir build: %w", err)
	}

	// Step 2: search for relevant symbols based on the task description
	keywords := extractKeywords(taskDesc)
	symbols := p.gatherSymbols(keywords)

	// Step 3: group symbols into mission buckets by file directory
	buckets := groupByBucket(symbols)

	// Step 4: create mission DAG
	planID := genID()
	dag := NewMissionDAG(planID, taskDesc, repoPath)
	now := nowFunc().UTC().Format(time.RFC3339)

	// Create an "analysis" root mission (no deps)
	analysisMission := Mission{
		MissionID:   genID(),
		Goal:        fmt.Sprintf("Analyze codebase for: %s", taskDesc),
		BaseRev:     "HEAD",
		Scopes:      []Scope{},
		LeaseIDs:    []string{},
		Tasks:       []string{"analyze"},
		TokenBudget: 4000,
		CreatedAt:   now,
	}
	dag.AddNode(DAGNode{Mission: analysisMission, DependsOn: []string{}})

	// Create one mission per bucket, all depending on the analysis mission
	for bucketName, syms := range buckets {
		scopes := make([]Scope, 0, len(syms))
		seen := make(map[string]bool)
		for _, s := range syms {
			key := s.Kind + ":" + s.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			scopes = append(scopes, Scope{
				ScopeType:  scopeTypeFor(s.Kind),
				ScopeValue: s.Name,
			})
		}

		missionID := genID()

		// Acquire leases from Heaven
		leaseResult, err := p.heaven.LeaseAcquire("god-"+planID, missionID, scopes)
		if err != nil {
			return nil, fmt.Errorf("lease acquire for %s: %w", bucketName, err)
		}

		leaseIDs := make([]string, len(leaseResult.Acquired))
		for i, l := range leaseResult.Acquired {
			leaseIDs[i] = l.LeaseID
		}

		mission := Mission{
			MissionID:   missionID,
			Goal:        fmt.Sprintf("Implement %s bucket: %s", bucketName, taskDesc),
			BaseRev:     "HEAD",
			Scopes:      scopes,
			LeaseIDs:    leaseIDs,
			Tasks:       []string{bucketName},
			TokenBudget: 8000,
			CreatedAt:   now,
		}
		dag.AddNode(DAGNode{
			Mission:   mission,
			DependsOn: []string{analysisMission.MissionID},
		})

		// Log MISSION_CREATED event
		p.heaven.AppendEvent(map[string]any{
			"type":       "mission_created",
			"mission_id": missionID,
			"plan_id":    planID,
			"goal":       mission.Goal,
			"scopes":     scopes,
			"lease_ids":  leaseIDs,
		})
	}

	// If no symbols found, create a single fallback mission
	if len(buckets) == 0 {
		missionID := genID()
		mission := Mission{
			MissionID:   missionID,
			Goal:        taskDesc,
			BaseRev:     "HEAD",
			Scopes:      []Scope{},
			LeaseIDs:    []string{},
			Tasks:       []string{"default"},
			TokenBudget: 8000,
			CreatedAt:   now,
		}
		dag.AddNode(DAGNode{
			Mission:   mission,
			DependsOn: []string{analysisMission.MissionID},
		})

		p.heaven.AppendEvent(map[string]any{
			"type":       "mission_created",
			"mission_id": missionID,
			"plan_id":    planID,
			"goal":       mission.Goal,
		})
	}

	return dag, nil
}

// gatherSymbols queries Heaven for symbols matching the keywords.
func (p *Planner) gatherSymbols(keywords []string) []SymbolResult {
	var all []SymbolResult
	seen := make(map[int64]bool)
	for _, kw := range keywords {
		syms, err := p.heaven.IRSearch(kw, 10)
		if err != nil {
			continue
		}
		for _, s := range syms {
			if !seen[s.ID] {
				seen[s.ID] = true
				all = append(all, s)
			}
		}
	}
	return all
}

// groupByBucket groups symbols into buckets by their parent directory.
// Returns at most 3 buckets for MVP.
func groupByBucket(symbols []SymbolResult) map[string][]SymbolResult {
	dirMap := make(map[string][]SymbolResult)
	for _, s := range symbols {
		dir := filepath.Dir(s.Path)
		base := filepath.Base(dir)
		if base == "." || base == "" {
			base = "root"
		}
		dirMap[base] = append(dirMap[base], s)
	}

	// If more than 3 buckets, merge smallest into "misc"
	if len(dirMap) > 3 {
		type entry struct {
			name string
			syms []SymbolResult
		}
		var entries []entry
		for k, v := range dirMap {
			entries = append(entries, entry{k, v})
		}
		sort.Slice(entries, func(i, j int) bool {
			return len(entries[i].syms) > len(entries[j].syms)
		})

		result := make(map[string][]SymbolResult)
		for i, e := range entries {
			if i < 2 {
				result[e.name] = e.syms
			} else {
				result["misc"] = append(result["misc"], e.syms...)
			}
		}
		return result
	}

	return dirMap
}

// extractKeywords splits a task description into searchable keywords.
func extractKeywords(task string) []string {
	// Remove common stop words and short words
	stop := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "in": true,
		"to": true, "for": true, "of": true, "and": true, "or": true,
		"with": true, "that": true, "this": true, "it": true, "on": true,
		"at": true, "by": true, "from": true, "as": true, "be": true,
		"add": true, "fix": true, "update": true, "implement": true,
		"change": true, "modify": true, "create": true, "make": true,
	}

	words := strings.Fields(strings.ToLower(task))
	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}") // strip punctuation
		if len(w) < 3 || stop[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
	return keywords
}

// scopeTypeFor maps symbol kinds to lease scope types.
func scopeTypeFor(kind string) string {
	switch kind {
	case "function", "method", "constant", "variable", "type", "interface", "enum":
		return "symbol"
	case "class":
		return "symbol"
	default:
		return "symbol"
	}
}

var idFunc = defaultGenID

func defaultGenID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func genID() string { return idFunc() }
