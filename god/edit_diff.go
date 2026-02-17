package god

import (
	"fmt"
	"strings"
)

const diffContext = 3

// DiffResult holds unified diffs for all files modified by an Edit IR apply.
type DiffResult struct {
	Diffs []FileDiff `json:"diffs"`
}

// FileDiff is a unified diff for a single file.
type FileDiff struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
}

// GenerateDiff produces a unified diff string between old and new content
// of a file, with up to 3 lines of context.
func GenerateDiff(path string, oldContent, newContent string) string {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	return unifiedDiff("a/"+path, "b/"+path, oldLines, newLines)
}

// richDiffOp represents a single line-level diff operation carrying its text.
type richDiffOp struct {
	kind byte // '=', '-', '+'
	text string
}

type hunk struct {
	aStart int
	aCount int
	bStart int
	bCount int
	lines  []string
}

func unifiedDiff(pathA, pathB string, a, b []string) string {
	ops := computeDiff(a, b)

	hasChanges := false
	for _, op := range ops {
		if op.kind != '=' {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return ""
	}

	hunks := buildHunks(ops)
	if len(hunks) == 0 {
		return ""
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n", pathA)
	fmt.Fprintf(&out, "+++ %s\n", pathB)

	for _, h := range hunks {
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", h.aStart+1, h.aCount, h.bStart+1, h.bCount)
		for _, line := range h.lines {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}

	return out.String()
}

// computeDiff computes a line-level diff between a and b using an O(NM) LCS
// algorithm, returning rich ops that carry their line text.
func computeDiff(a, b []string) []richDiffOp {
	n, m := len(a), len(b)

	// Build LCS table
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	// Trace back to produce edit operations
	var ops []richDiffOp
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, richDiffOp{'=', a[i]})
			i++
			j++
		} else if lcs[i+1][j] >= lcs[i][j+1] {
			ops = append(ops, richDiffOp{'-', a[i]})
			i++
		} else {
			ops = append(ops, richDiffOp{'+', b[j]})
			j++
		}
	}
	for i < n {
		ops = append(ops, richDiffOp{'-', a[i]})
		i++
	}
	for j < m {
		ops = append(ops, richDiffOp{'+', b[j]})
		j++
	}

	return ops
}

// buildHunks groups diff operations into hunks with context lines.
func buildHunks(ops []richDiffOp) []hunk {
	type changeRange struct{ start, end int }
	var changes []changeRange
	i := 0
	for i < len(ops) {
		if ops[i].kind != '=' {
			start := i
			for i < len(ops) && ops[i].kind != '=' {
				i++
			}
			changes = append(changes, changeRange{start, i})
		} else {
			i++
		}
	}
	if len(changes) == 0 {
		return nil
	}

	// Merge nearby changes (within 2*context equal lines)
	var groups []changeRange
	current := changes[0]
	for i := 1; i < len(changes); i++ {
		if changes[i].start-current.end <= 2*diffContext {
			current.end = changes[i].end
		} else {
			groups = append(groups, current)
			current = changes[i]
		}
	}
	groups = append(groups, current)

	var hunks []hunk
	for _, g := range groups {
		ctxStart := g.start - diffContext
		if ctxStart < 0 {
			ctxStart = 0
		}
		ctxEnd := g.end + diffContext
		if ctxEnd > len(ops) {
			ctxEnd = len(ops)
		}

		// Compute starting line numbers by scanning ops before the hunk
		aStart, bStart := 0, 0
		for j := 0; j < ctxStart; j++ {
			switch ops[j].kind {
			case '=':
				aStart++
				bStart++
			case '-':
				aStart++
			case '+':
				bStart++
			}
		}

		aCount, bCount := 0, 0
		var lines []string
		for j := ctxStart; j < ctxEnd; j++ {
			switch ops[j].kind {
			case '=':
				lines = append(lines, " "+ops[j].text)
				aCount++
				bCount++
			case '-':
				lines = append(lines, "-"+ops[j].text)
				aCount++
			case '+':
				lines = append(lines, "+"+ops[j].text)
				bCount++
			}
		}

		hunks = append(hunks, hunk{
			aStart: aStart,
			aCount: aCount,
			bStart: bStart,
			bCount: bCount,
			lines:  lines,
		})
	}

	return hunks
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
