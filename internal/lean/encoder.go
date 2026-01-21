package lean

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// Encoder is a stateful TSLN encoder that tracks the previous row to compute
// deltas and repeat markers. It produces deterministic output for identical input.
type Encoder struct {
	schema   Schema
	base     time.Time
	prev     []any // previous row values for delta/repeat computation
	buf      bytes.Buffer
	rowCount int
}

// NewEncoder creates a TSLN encoder for the given schema and base timestamp.
func NewEncoder(schema Schema, base time.Time) *Encoder {
	return &Encoder{
		schema: schema,
		base:   base,
	}
}

// WriteHeader writes the TSLN header block to the internal buffer.
func (e *Encoder) WriteHeader() {
	fmt.Fprintf(&e.buf, "# %s\n", Version)

	// Schema line: name:type pairs
	var parts []string
	for _, col := range e.schema.Columns {
		parts = append(parts, fmt.Sprintf("%s:%s", col.Name, col.Type))
	}
	fmt.Fprintf(&e.buf, "# Schema: %s\n", strings.Join(parts, " "))

	// Base timestamp
	fmt.Fprintf(&e.buf, "# Base: %s\n", e.base.UTC().Format(time.RFC3339))

	// Encoding line
	var encodings []string
	for _, col := range e.schema.Columns {
		encodings = append(encodings, string(col.Encoding))
	}
	fmt.Fprintf(&e.buf, "# Encoding: %s\n", strings.Join(encodings, ","))

	e.buf.WriteString("---\n")
}

// EncodeRow encodes one row of values. Values must match the schema column order.
// For timestamp columns, pass a time.Time. For string columns, pass a string.
// For int/float columns, pass int, int64, or float64.
func (e *Encoder) EncodeRow(values []any) error {
	if len(values) != len(e.schema.Columns) {
		return fmt.Errorf("tsln encode: got %d values, schema has %d columns", len(values), len(e.schema.Columns))
	}

	var parts []string
	for i, col := range e.schema.Columns {
		val := values[i]
		var encoded string

		switch col.Type {
		case ColTimestamp:
			ts, ok := val.(time.Time)
			if !ok {
				return fmt.Errorf("tsln encode: column %q expects time.Time, got %T", col.Name, val)
			}
			offset := ts.Sub(e.base).Milliseconds()
			encoded = fmt.Sprintf("%d", offset)

		case ColString:
			s := fmt.Sprintf("%v", val)
			if col.Encoding == EncRepeat && e.prev != nil {
				prevStr := fmt.Sprintf("%v", e.prev[i])
				if s == prevStr {
					encoded = "="
				} else {
					encoded = s
				}
			} else {
				encoded = s
			}

		case ColInt:
			n := toInt64(val)
			if col.Encoding == EncDelta && e.prev != nil {
				prevN := toInt64(e.prev[i])
				delta := n - prevN
				if delta == 0 {
					encoded = "0"
				} else if delta > 0 {
					encoded = fmt.Sprintf("+%d", delta)
				} else {
					encoded = fmt.Sprintf("%d", delta)
				}
			} else {
				encoded = fmt.Sprintf("%d", n)
			}

		case ColFloat:
			f := toFloat64(val)
			if col.Encoding == EncDelta && e.prev != nil {
				prevF := toFloat64(e.prev[i])
				delta := f - prevF
				if delta == 0 {
					encoded = "0"
				} else if delta > 0 {
					encoded = fmt.Sprintf("+%.2f", delta)
				} else {
					encoded = fmt.Sprintf("%.2f", delta)
				}
			} else {
				encoded = fmt.Sprintf("%.2f", f)
			}

		default:
			encoded = fmt.Sprintf("%v", val)
		}

		parts = append(parts, encoded)
	}

	e.buf.WriteString(strings.Join(parts, "|"))
	e.buf.WriteByte('\n')

	// Store current values as previous for next row's delta/repeat
	e.prev = make([]any, len(values))
	copy(e.prev, values)
	e.rowCount++

	return nil
}

// Bytes returns the complete TSLN document as bytes.
func (e *Encoder) Bytes() []byte {
	return e.buf.Bytes()
}

// String returns the complete TSLN document as a string.
func (e *Encoder) String() string {
	return e.buf.String()
}

// EncodeMetricsRow encodes a single MetricsRow as a complete TSLN document
// (header + 1 data row).
func EncodeMetricsRow(row MetricsRow) ([]byte, error) {
	return EncodeMetricsRows([]MetricsRow{row})
}

// EncodeMetricsRows encodes multiple MetricsRow snapshots as a complete TSLN document
// (header + N data rows with delta encoding between consecutive rows).
func EncodeMetricsRows(rows []MetricsRow) ([]byte, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("tsln encode: no rows to encode")
	}

	schema := MetricsSchema()
	base := rows[0].Timestamp
	enc := NewEncoder(schema, base)
	enc.WriteHeader()

	for _, r := range rows {
		if err := enc.EncodeRow(metricsRowToValues(r)); err != nil {
			return nil, err
		}
	}

	return enc.Bytes(), nil
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
