package lean

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var testBase = time.Date(2026, 2, 8, 10, 0, 0, 0, time.UTC)

func sampleRow(offsetSec int, status string, pf, pfSize, retries, rejects, conflicts, testFail, tokIn, tokOut, turns int, elapsedMS int64, phaseTx int) MetricsRow {
	return MetricsRow{
		Timestamp:        testBase.Add(time.Duration(offsetSec) * time.Second),
		Status:           status,
		PFCount:          pf,
		PFResponseSize:   pfSize,
		Retries:          retries,
		Rejects:          rejects,
		Conflicts:        conflicts,
		TestFailures:     testFail,
		TokensIn:         tokIn,
		TokensOut:        tokOut,
		Turns:            turns,
		ElapsedMS:        elapsedMS,
		PhaseTransitions: phaseTx,
	}
}

func TestEncoderSingleRow(t *testing.T) {
	row := sampleRow(0, "active", 5, 4200, 0, 0, 0, 0, 1200, 300, 1, 1500, 0)
	data, err := EncodeMetricsRow(row)
	if err != nil {
		t.Fatal(err)
	}

	output := string(data)
	t.Logf("Single row TSLN output (%d bytes):\n%s", len(data), output)

	// Verify header present
	if !strings.Contains(output, "# TSLN/2.0") {
		t.Error("missing version header")
	}
	if !strings.Contains(output, "# Schema:") {
		t.Error("missing schema header")
	}
	if !strings.Contains(output, "---") {
		t.Error("missing data separator")
	}

	// Verify data row contains pipe-delimited values
	lines := strings.Split(strings.TrimSpace(output), "\n")
	dataLine := lines[len(lines)-1]
	parts := strings.Split(dataLine, "|")
	if len(parts) != 13 {
		t.Errorf("expected 13 pipe-delimited fields, got %d: %q", len(parts), dataLine)
	}

	// First value should be 0 (offset from base)
	if parts[0] != "0" {
		t.Errorf("expected timestamp offset 0, got %q", parts[0])
	}
	// Second value should be "active"
	if parts[1] != "active" {
		t.Errorf("expected status 'active', got %q", parts[1])
	}
}

func TestEncoderMultiRow(t *testing.T) {
	rows := []MetricsRow{
		sampleRow(0, "active", 5, 4200, 0, 0, 0, 0, 1200, 300, 1, 1500, 0),
		sampleRow(1, "active", 8, 7000, 0, 0, 0, 0, 2000, 450, 2, 2700, 0),
		sampleRow(2, "active", 10, 8900, 1, 0, 0, 0, 2600, 550, 3, 3800, 0),
		sampleRow(3, "thrashing", 14, 12100, 2, 2, 0, 1, 3500, 750, 4, 5200, 1),
	}

	data, err := EncodeMetricsRows(rows)
	if err != nil {
		t.Fatal(err)
	}

	output := string(data)
	t.Logf("Multi-row TSLN output (%d bytes):\n%s", len(data), output)

	// Parse data lines (after ---)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var dataLines []string
	inData := false
	for _, line := range lines {
		if line == "---" {
			inData = true
			continue
		}
		if inData && line != "" {
			dataLines = append(dataLines, line)
		}
	}

	if len(dataLines) != 4 {
		t.Fatalf("expected 4 data rows, got %d", len(dataLines))
	}

	// Row 2 should have repeat marker for status
	row2Parts := strings.Split(dataLines[1], "|")
	if row2Parts[1] != "=" {
		t.Errorf("expected repeat marker '=' for unchanged status, got %q", row2Parts[1])
	}

	// Row 2 pf_count should be delta: +3
	if row2Parts[2] != "+3" {
		t.Errorf("expected delta '+3' for pf_count, got %q", row2Parts[2])
	}

	// Row 4 should have "thrashing" (status changed)
	row4Parts := strings.Split(dataLines[3], "|")
	if row4Parts[1] != "thrashing" {
		t.Errorf("expected 'thrashing' status change, got %q", row4Parts[1])
	}
}

func TestRoundTrip(t *testing.T) {
	rows := []MetricsRow{
		sampleRow(0, "active", 5, 4200, 0, 0, 0, 0, 1200, 300, 1, 1500, 0),
		sampleRow(1, "active", 8, 7000, 0, 0, 0, 0, 2000, 450, 2, 2700, 0),
		sampleRow(2, "thrashing", 14, 12100, 2, 2, 0, 1, 3500, 750, 4, 5200, 1),
	}

	encoded, err := EncodeMetricsRows(rows)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode error: %v\nencoded:\n%s", err, encoded)
	}

	if len(decoded) != len(rows) {
		t.Fatalf("expected %d rows, decoded %d", len(rows), len(decoded))
	}

	schema := MetricsSchema()
	for i, row := range rows {
		d := decoded[i]
		vals := metricsRowToValues(row)

		for j, col := range schema.Columns {
			switch col.Type {
			case ColTimestamp:
				expected := vals[j].(time.Time)
				actual := d[col.Name].(time.Time)
				if !expected.Equal(actual) {
					t.Errorf("row %d col %q: expected %v, got %v", i, col.Name, expected, actual)
				}
			case ColString:
				expected := vals[j].(string)
				actual := d[col.Name].(string)
				if expected != actual {
					t.Errorf("row %d col %q: expected %q, got %q", i, col.Name, expected, actual)
				}
			case ColInt:
				expected := toInt64(vals[j])
				actual := d[col.Name].(int64)
				if expected != actual {
					t.Errorf("row %d col %q: expected %d, got %d", i, col.Name, expected, actual)
				}
			}
		}
	}
}

func TestDeterminism(t *testing.T) {
	rows := []MetricsRow{
		sampleRow(0, "active", 5, 4200, 0, 0, 0, 0, 1200, 300, 1, 1500, 0),
		sampleRow(1, "active", 8, 7000, 0, 0, 0, 0, 2000, 450, 2, 2700, 0),
	}

	data1, err := EncodeMetricsRows(rows)
	if err != nil {
		t.Fatal(err)
	}

	data2, err := EncodeMetricsRows(rows)
	if err != nil {
		t.Fatal(err)
	}

	if string(data1) != string(data2) {
		t.Error("non-deterministic output: two encodes of same input differ")
		t.Logf("encode 1:\n%s", data1)
		t.Logf("encode 2:\n%s", data2)
	}
}

// TestTokenSavingsVsJSON is the gate test: TSLN must use less than 40% of JSON bytes
// for a realistic MissionMetrics payload.
func TestTokenSavingsVsJSON(t *testing.T) {
	// Simulate a realistic thrashing mission with 10 snapshots
	rows := make([]MetricsRow, 10)
	for i := range rows {
		rows[i] = MetricsRow{
			Timestamp:        testBase.Add(time.Duration(i) * time.Second),
			Status:           "active",
			PFCount:          5 + i*3,
			PFResponseSize:   4200 + i*2800,
			Retries:          i / 3,
			Rejects:          i / 5,
			Conflicts:        0,
			TestFailures:     i / 4,
			TokensIn:         1200 + i*800,
			TokensOut:        300 + i*150,
			Turns:            i + 1,
			ElapsedMS:        int64(1500 + i*1200),
			PhaseTransitions: i / 6,
		}
	}
	// Last row switches to thrashing
	rows[9].Status = "thrashing"

	// JSON encoding: array of metric objects (simulating what would be in event log)
	type jsonMetrics struct {
		MissionID      string `json:"mission_id"`
		Status         string `json:"status"`
		PFCount        int    `json:"pf_count"`
		PFResponseSize int    `json:"pf_response_size_total"`
		Retries        int    `json:"retries"`
		Rejects        int    `json:"rejects"`
		Conflicts      int    `json:"conflicts"`
		TestFailures   int    `json:"test_failures"`
		TokensIn       int    `json:"tokens_in"`
		TokensOut      int    `json:"tokens_out"`
		Turns          int    `json:"turns"`
		ElapsedMS      int64  `json:"elapsed_ms"`
		PhaseTx        int    `json:"phase_transitions"`
		StartedAt      string `json:"started_at"`
	}

	var jsonRows []jsonMetrics
	for _, r := range rows {
		jsonRows = append(jsonRows, jsonMetrics{
			MissionID:      "mission-abc-123",
			Status:         r.Status,
			PFCount:        r.PFCount,
			PFResponseSize: r.PFResponseSize,
			Retries:        r.Retries,
			Rejects:        r.Rejects,
			Conflicts:      r.Conflicts,
			TestFailures:   r.TestFailures,
			TokensIn:       r.TokensIn,
			TokensOut:      r.TokensOut,
			Turns:          r.Turns,
			ElapsedMS:      r.ElapsedMS,
			PhaseTx:        r.PhaseTransitions,
			StartedAt:      r.Timestamp.Format(time.RFC3339),
		})
	}

	jsonData, _ := json.Marshal(jsonRows)
	jsonBytes := len(jsonData)

	tslnData, err := EncodeMetricsRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	tslnBytes := len(tslnData)

	ratio := float64(tslnBytes) / float64(jsonBytes)
	reduction := (1.0 - ratio) * 100

	t.Logf("JSON:  %d bytes (~%d tokens)", jsonBytes, jsonBytes/4)
	t.Logf("TSLN:  %d bytes (~%d tokens)", tslnBytes, tslnBytes/4)
	t.Logf("Ratio: %.1f%% (%.1f%% reduction)", ratio*100, reduction)

	if ratio >= 0.40 {
		t.Errorf("GATE FAIL: TSLN is %.1f%% of JSON (must be < 40%%)", ratio*100)
	}

	// Also verify round-trip fidelity
	decoded, err := Decode(tslnData)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(decoded) != len(rows) {
		t.Fatalf("decoded %d rows, expected %d", len(decoded), len(rows))
	}
}

func TestSingleRowSavings(t *testing.T) {
	// Even a single row should show savings over JSON
	row := sampleRow(0, "active", 5, 4200, 0, 0, 0, 0, 1200, 300, 1, 1500, 0)

	type jsonMetrics struct {
		MissionID      string `json:"mission_id"`
		Status         string `json:"status"`
		PFCount        int    `json:"pf_count"`
		PFResponseSize int    `json:"pf_response_size_total"`
		Retries        int    `json:"retries"`
		Rejects        int    `json:"rejects"`
		Conflicts      int    `json:"conflicts"`
		TestFailures   int    `json:"test_failures"`
		TokensIn       int    `json:"tokens_in"`
		TokensOut      int    `json:"tokens_out"`
		Turns          int    `json:"turns"`
		ElapsedMS      int64  `json:"elapsed_ms"`
		PhaseTx        int    `json:"phase_transitions"`
		StartedAt      string `json:"started_at"`
	}

	jsonObj := jsonMetrics{
		MissionID:      "mission-abc-123",
		Status:         row.Status,
		PFCount:        row.PFCount,
		PFResponseSize: row.PFResponseSize,
		TokensIn:       row.TokensIn,
		TokensOut:      row.TokensOut,
		Turns:          row.Turns,
		ElapsedMS:      row.ElapsedMS,
		StartedAt:      row.Timestamp.Format(time.RFC3339),
	}

	jsonData, _ := json.Marshal(jsonObj)
	tslnData, err := EncodeMetricsRow(row)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Single row — JSON: %d bytes, TSLN: %d bytes (%.0f%% reduction)",
		len(jsonData), len(tslnData), (1.0-float64(len(tslnData))/float64(len(jsonData)))*100)
}

func TestDecodeInvalidHeader(t *testing.T) {
	_, err := Decode([]byte("garbage data without header"))
	if err == nil {
		t.Error("expected error for invalid TSLN data")
	}
}

func TestDecodeCorruptedRow(t *testing.T) {
	bad := `# TSLN/2.0
# Schema: t:t i:val
# Base: 2026-02-08T10:00:00Z
# Encoding: d,d
---
0|100
not_a_number|200
`
	_, err := Decode([]byte(bad))
	if err == nil {
		t.Error("expected error for corrupted row")
	}
}

func TestEmptyInput(t *testing.T) {
	_, err := EncodeMetricsRows(nil)
	if err == nil {
		t.Error("expected error for empty input")
	}

	_, err = EncodeMetricsRows([]MetricsRow{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}
