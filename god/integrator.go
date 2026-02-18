package god

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxConflictDepth limits how many times a conflict mission can spawn
// another conflict mission. Prevents infinite cascading.
const MaxConflictDepth = 2

// IntegrateRequest bundles an Angel response with the context needed to
// integrate it into the repository.
type IntegrateRequest struct {
	OwnerID        string
	RepoRoot       string
	Response       *AngelResponse
	Mission        Mission
	FileClocks     map[string]int64 // expected file clocks at time of mission dispatch
	ConflictDepth  int              // current conflict recursion depth
	SkipLeaseCheck bool             // skip manifest lease validation (solo mode)
}

// IntegrateResult describes the outcome of an integration attempt.
type IntegrateResult struct {
	Success        bool             `json:"success"`
	FilesModified  []string         `json:"files_modified,omitempty"`
	FilesCreated   []string         `json:"files_created,omitempty"`
	OpsApplied     int              `json:"ops_applied"`
	Diffs          []FileDiff       `json:"diffs,omitempty"`
	ConflictMission *CompileResult  `json:"conflict_mission,omitempty"`
	Error          string           `json:"error,omitempty"`
}

// Integrator applies Angel outputs to the repository, validating leases and
// file clocks through Heaven, and generating conflict hunk missions when
// application fails.
type Integrator struct {
	heaven *HeavenClient
}

// NewIntegrator creates an Integrator backed by the given Heaven client.
func NewIntegrator(heaven *HeavenClient) *Integrator {
	return &Integrator{heaven: heaven}
}

// Integrate validates the manifest, applies the Edit IR, increments file
// clocks, and returns the result. On conflict it generates a conflict hunk
// mission instead of failing outright.
func (ig *Integrator) Integrate(req IntegrateRequest) (*IntegrateResult, error) {
	resp := req.Response
	if resp.OutputType != "edit_ir" || resp.EditIR == nil {
		return nil, fmt.Errorf("integrator: only edit_ir output type is supported, got %q", resp.OutputType)
	}

	// Normalize: strip repoRoot prefix from op paths if Angel output absolute paths.
	// e.g. "/tmp/repo/src/foo.py" → "src/foo.py" when repoRoot is "/tmp/repo"
	repoPrefix := req.RepoRoot
	if !strings.HasSuffix(repoPrefix, "/") {
		repoPrefix += "/"
	}
	for i := range resp.EditIR.Ops {
		if strings.HasPrefix(resp.EditIR.Ops[i].Path, repoPrefix) {
			resp.EditIR.Ops[i].Path = strings.TrimPrefix(resp.EditIR.Ops[i].Path, repoPrefix)
		}
	}
	// Also normalize manifest FilesTouched
	for i := range resp.Manifest.FilesTouched {
		if strings.HasPrefix(resp.Manifest.FilesTouched[i], repoPrefix) {
			resp.Manifest.FilesTouched[i] = strings.TrimPrefix(resp.Manifest.FilesTouched[i], repoPrefix)
		}
	}

	// Step 1: Validate manifest (leases + file clocks) via Heaven
	if !req.SkipLeaseCheck {
		validation, err := ig.heaven.ValidateManifest(
			req.OwnerID,
			req.Mission.MissionID,
			resp.Manifest.SymbolsTouched,
			resp.Manifest.FilesTouched,
			req.FileClocks,
		)
		if err != nil {
			return nil, fmt.Errorf("integrator: validate manifest: %w", err)
		}

		// Step 2: Handle clock drift — attempt simple rebase
		if !validation.Allowed && len(validation.ClockDrift) > 0 && len(validation.MissingLeases) == 0 {
			// Drift detected but leases are held. Try rebase: recompute anchors.
			rebased, rebaseErr := ig.rebaseAnchors(req.RepoRoot, resp.EditIR)
			if rebaseErr != nil {
				// Rebase failed — generate conflict mission
				conflictResult := ig.generateConflictMission(req, resp.EditIR, rebaseErr)
				return conflictResult, nil
			}
			resp.EditIR = rebased
		} else if !validation.Allowed {
			return &IntegrateResult{
				Success: false,
				Error:   fmt.Sprintf("manifest validation failed: %s", validation.Reason),
			}, nil
		}
	}

	// Step 3: Snapshot old file contents for diff generation
	oldContents := ig.snapshotFiles(req.RepoRoot, resp.Manifest.FilesTouched)

	// Step 4: Apply Edit IR
	applyResult, err := ApplyEditIR(req.RepoRoot, resp.EditIR)
	if err != nil {
		// Apply failed — generate conflict hunk mission
		conflictResult := ig.generateConflictMission(req, resp.EditIR, err)
		return conflictResult, nil
	}

	// Step 5: Generate diffs
	var diffs []FileDiff
	allFiles := append(applyResult.FilesModified, applyResult.FilesCreated...)
	for _, f := range allFiles {
		absPath := filepath.Join(req.RepoRoot, f)
		newData, readErr := os.ReadFile(absPath)
		if readErr != nil {
			continue
		}
		oldContent := oldContents[f]
		d := GenerateDiff(f, oldContent, string(newData))
		if d != "" {
			diffs = append(diffs, FileDiff{Path: f, Diff: d})
		}
	}

	// Step 6: Increment file clocks for modified files
	if len(resp.Manifest.FilesTouched) > 0 {
		_, err = ig.heaven.FileClockInc(resp.Manifest.FilesTouched)
		if err != nil {
			return nil, fmt.Errorf("integrator: file clock inc: %w", err)
		}
	}

	// Step 7: Log integration event
	ig.heaven.AppendEvent(map[string]any{
		"type":           "integration_complete",
		"mission_id":     req.Mission.MissionID,
		"files_modified": applyResult.FilesModified,
		"files_created":  applyResult.FilesCreated,
		"ops_applied":    applyResult.OpsApplied,
	})

	return &IntegrateResult{
		Success:       true,
		FilesModified: applyResult.FilesModified,
		FilesCreated:  applyResult.FilesCreated,
		OpsApplied:    applyResult.OpsApplied,
		Diffs:         diffs,
	}, nil
}

// rebaseAnchors attempts to recompute anchor hashes for each op against the
// current file state. This is the "simple rebase" strategy.
// Uses a file cache so N ops on the same file only trigger 1 disk read.
func (ig *Integrator) rebaseAnchors(repoRoot string, ir *EditIR) (*EditIR, error) {
	rebased := &EditIR{Ops: make([]EditOp, len(ir.Ops))}
	copy(rebased.Ops, ir.Ops)

	fileCache := make(map[string][]string) // path -> lines

	for i, op := range rebased.Ops {
		if op.Op == "add_file" {
			continue
		}

		lines, cached := fileCache[op.Path]
		if !cached {
			absPath := filepath.Join(repoRoot, op.Path)
			var err error
			lines, err = readFileLines(absPath)
			if err != nil {
				return nil, fmt.Errorf("rebase: read %s: %w", op.Path, err)
			}
			fileCache[op.Path] = lines
		}

		switch op.Op {
		case "replace_span", "delete_span":
			if len(op.Lines) != 2 {
				return nil, fmt.Errorf("rebase: op %d invalid lines", i)
			}
			if op.Lines[1] > len(lines) {
				return nil, fmt.Errorf("rebase: op %d line %d exceeds file %s (%d lines)", i, op.Lines[1], op.Path, len(lines))
			}
			rebased.Ops[i].AnchorHash = ComputeAnchorHash(lines, op.Lines[0], op.Lines[1])

		case "insert_after_symbol":
			symbolLine := -1
			for j, line := range lines {
				if strings.Contains(line, op.Symbol) {
					symbolLine = j + 1
					break
				}
			}
			if symbolLine < 0 {
				return nil, fmt.Errorf("rebase: symbol %q not found in %s", op.Symbol, op.Path)
			}
			rebased.Ops[i].AnchorHash = ComputeAnchorHash(lines, symbolLine, symbolLine)
		}
	}

	return rebased, nil
}

// generateConflictMission creates a conflict hunk mission from a failed
// integration. It isolates the conflict to the failing op's file region
// and emits an AA-compiled mission for an angel to resolve.
// Respects MaxConflictDepth to prevent infinite cascading.
func (ig *Integrator) generateConflictMission(req IntegrateRequest, ir *EditIR, applyErr error) *IntegrateResult {
	if req.ConflictDepth >= MaxConflictDepth {
		return &IntegrateResult{
			Success: false,
			Error:   fmt.Sprintf("conflict depth limit reached (%d): %v", MaxConflictDepth, applyErr),
		}
	}
	// Find the conflicting file and region from the error
	conflictPath, startLine, endLine := extractConflictRegion(ir, applyErr)

	// Build AA program for conflict resolution
	leases := []Scope{{ScopeType: "file", ScopeValue: conflictPath}}
	needs := []AANeed{}
	if conflictPath != "" && endLine > 0 {
		// Request a slice of the conflict region with surrounding context
		ctxStart := startLine - 10
		if ctxStart < 1 {
			ctxStart = 1
		}
		needs = append(needs, AANeed{
			Kind:  "slice",
			Path:  conflictPath,
			Start: ctxStart,
			N:     (endLine - ctxStart) + 20,
		})
	}

	prog := &AAProgram{
		BaseRev: req.Mission.BaseRev,
		Leases:  leases,
		Needs:   needs,
		Dos: []string{
			fmt.Sprintf("Resolve conflict in %s: %s", conflictPath, applyErr.Error()),
		},
		Return: "edit_ir",
	}

	compiled, err := CompileAA(prog)
	if err != nil {
		// If we can't compile the conflict mission, return the error directly
		return &IntegrateResult{
			Success: false,
			Error:   fmt.Sprintf("conflict (compile failed): %v; original: %v", err, applyErr),
		}
	}

	// Tag the conflict mission
	compiled.Mission.Goal = fmt.Sprintf("[CONFLICT] %s", compiled.Mission.Goal)
	compiled.Mission.TokenBudget = 4000 // smaller budget for hunk resolution
	compiled.Mission.CreatedAt = nowFunc().UTC().Format(time.RFC3339)

	// Log conflict event
	ig.heaven.AppendEvent(map[string]any{
		"type":              "conflict_detected",
		"original_mission":  req.Mission.MissionID,
		"conflict_mission":  compiled.Mission.MissionID,
		"conflict_path":     conflictPath,
		"conflict_error":    applyErr.Error(),
	})

	return &IntegrateResult{
		Success:         false,
		Error:           fmt.Sprintf("conflict in %s: %s", conflictPath, applyErr.Error()),
		ConflictMission: compiled,
	}
}

// extractConflictRegion tries to determine the file and line range from
// the Edit IR and the apply error.
func extractConflictRegion(ir *EditIR, applyErr error) (path string, startLine, endLine int) {
	errStr := applyErr.Error()

	// Try to find which op failed by checking the error prefix "ops[N]"
	for _, op := range ir.Ops {
		if op.Path != "" && strings.Contains(errStr, op.Path) {
			path = op.Path
			if len(op.Lines) == 2 {
				startLine = op.Lines[0]
				endLine = op.Lines[1]
			}
			return
		}
	}

	// Fallback: use the first op with a path
	for _, op := range ir.Ops {
		if op.Path != "" {
			path = op.Path
			if len(op.Lines) == 2 {
				startLine = op.Lines[0]
				endLine = op.Lines[1]
			}
			return
		}
	}

	return "", 0, 0
}

// snapshotFiles reads the current content of files for later diffing.
func (ig *Integrator) snapshotFiles(repoRoot string, paths []string) map[string]string {
	contents := make(map[string]string, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, p))
		if err == nil {
			contents[p] = string(data)
		}
	}
	return contents
}
