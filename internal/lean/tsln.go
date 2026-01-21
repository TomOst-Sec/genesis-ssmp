// Package lean implements a minimal TSLN (Time Series Lean Notation) encoder/decoder
// for Genesis telemetry data. TSLN reduces token cost by 50-70% vs JSON for numeric
// time-series streams via schema-first headers, delta encoding, and repeat markers.
//
// TSLN format based on https://github.com/turboline-ai/tsln (open spec).
package lean

import (
	"os"
	"time"
)

// Version is the TSLN format version emitted in headers.
const Version = "TSLN/2.0"

// Encoding specifies how a column's values are compressed across rows.
type Encoding string

const (
	EncDelta  Encoding = "d" // differential: +125, -50 (relative to previous row)
	EncRepeat Encoding = "r" // repeat marker: = if unchanged, literal otherwise
	EncRaw    Encoding = "w" // raw: always emit literal value
)

// ColType identifies the data type of a schema column.
type ColType string

const (
	ColTimestamp ColType = "t" // unix millis offset from base
	ColString    ColType = "s" // short categorical string
	ColInt       ColType = "i" // integer
	ColFloat     ColType = "f" // float
)

// Column defines one field in a TSLN schema.
type Column struct {
	Name     string
	Type     ColType
	Encoding Encoding
}

// Schema defines the column layout for a TSLN document.
type Schema struct {
	Columns []Column
}

// MetricsRow is the lean-native representation of a MissionMetrics snapshot.
// god.MissionMetrics is converted to this before encoding to avoid import cycles.
type MetricsRow struct {
	Timestamp        time.Time
	MissionID        string
	Status           string
	PFCount          int
	PFResponseSize   int
	Retries          int
	Rejects          int
	Conflicts        int
	TestFailures     int
	TokensIn         int
	TokensOut        int
	Turns            int
	ElapsedMS        int64
	PhaseTransitions int
}

// MetricsSchema returns the canonical TSLN schema for MissionMetrics.
func MetricsSchema() Schema {
	return Schema{
		Columns: []Column{
			{Name: "t", Type: ColTimestamp, Encoding: EncDelta},
			{Name: "status", Type: ColString, Encoding: EncRepeat},
			{Name: "pf_count", Type: ColInt, Encoding: EncDelta},
			{Name: "pf_resp_size", Type: ColInt, Encoding: EncDelta},
			{Name: "retries", Type: ColInt, Encoding: EncDelta},
			{Name: "rejects", Type: ColInt, Encoding: EncDelta},
			{Name: "conflicts", Type: ColInt, Encoding: EncDelta},
			{Name: "test_fail", Type: ColInt, Encoding: EncDelta},
			{Name: "tokens_in", Type: ColInt, Encoding: EncDelta},
			{Name: "tokens_out", Type: ColInt, Encoding: EncDelta},
			{Name: "turns", Type: ColInt, Encoding: EncDelta},
			{Name: "elapsed_ms", Type: ColInt, Encoding: EncDelta},
			{Name: "phase_tx", Type: ColInt, Encoding: EncDelta},
		},
	}
}

// metricsRowToValues converts a MetricsRow to an ordered slice of values
// matching MetricsSchema() column order.
func metricsRowToValues(r MetricsRow) []any {
	return []any{
		r.Timestamp,
		r.Status,
		r.PFCount,
		r.PFResponseSize,
		r.Retries,
		r.Rejects,
		r.Conflicts,
		r.TestFailures,
		r.TokensIn,
		r.TokensOut,
		r.Turns,
		int(r.ElapsedMS),
		r.PhaseTransitions,
	}
}

// Enabled returns true if TSLN lean encoding is active.
// Controlled by GENESIS_LEAN=1 environment variable.
func Enabled() bool {
	return os.Getenv("GENESIS_LEAN") == "1"
}
