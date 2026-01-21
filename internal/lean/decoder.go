package lean

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Decode parses a complete TSLN document and returns reconstructed rows as
// maps of column_name -> value. Delta and repeat encodings are reversed to
// produce absolute values.
func Decode(data []byte) ([]map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	var schema Schema
	var base time.Time
	var encodings []Encoding
	dataStart := -1

	// Parse header
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "---" {
			dataStart = i + 1
			break
		}
		if !strings.HasPrefix(line, "#") {
			continue
		}
		content := strings.TrimPrefix(line, "#")
		content = strings.TrimSpace(content)

		if strings.HasPrefix(content, "Schema:") {
			schemaStr := strings.TrimPrefix(content, "Schema:")
			schemaStr = strings.TrimSpace(schemaStr)
			parts := strings.Fields(schemaStr)
			for _, p := range parts {
				kv := strings.SplitN(p, ":", 2)
				if len(kv) != 2 {
					return nil, fmt.Errorf("tsln decode: invalid schema field %q", p)
				}
				schema.Columns = append(schema.Columns, Column{
					Name: kv[0],
					Type: ColType(kv[1]),
				})
			}
		} else if strings.HasPrefix(content, "Base:") {
			baseStr := strings.TrimPrefix(content, "Base:")
			baseStr = strings.TrimSpace(baseStr)
			var err error
			base, err = time.Parse(time.RFC3339, baseStr)
			if err != nil {
				return nil, fmt.Errorf("tsln decode: invalid base timestamp: %w", err)
			}
		} else if strings.HasPrefix(content, "Encoding:") {
			encStr := strings.TrimPrefix(content, "Encoding:")
			encStr = strings.TrimSpace(encStr)
			parts := strings.Split(encStr, ",")
			for _, p := range parts {
				encodings = append(encodings, Encoding(strings.TrimSpace(p)))
			}
		}
	}

	if dataStart < 0 {
		return nil, fmt.Errorf("tsln decode: no data separator (---) found")
	}
	if len(schema.Columns) == 0 {
		return nil, fmt.Errorf("tsln decode: empty schema")
	}

	// Apply encodings to schema columns
	if len(encodings) == len(schema.Columns) {
		for i := range schema.Columns {
			schema.Columns[i].Encoding = encodings[i]
		}
	}

	// Decode data rows
	var rows []map[string]any
	var prev []any

	for _, line := range lines[dataStart:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) != len(schema.Columns) {
			return nil, fmt.Errorf("tsln decode: row has %d fields, schema has %d columns", len(parts), len(schema.Columns))
		}

		row := make(map[string]any, len(schema.Columns))
		current := make([]any, len(schema.Columns))

		for i, col := range schema.Columns {
			val := parts[i]

			switch col.Type {
			case ColTimestamp:
				offsetMS, err := strconv.ParseInt(val, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("tsln decode: column %q invalid timestamp offset %q: %w", col.Name, val, err)
				}
				ts := base.Add(time.Duration(offsetMS) * time.Millisecond)
				row[col.Name] = ts
				current[i] = ts

			case ColString:
				if col.Encoding == EncRepeat && val == "=" && prev != nil {
					row[col.Name] = prev[i]
					current[i] = prev[i]
				} else {
					row[col.Name] = val
					current[i] = val
				}

			case ColInt:
				decoded, err := decodeDeltaInt(val, col.Encoding, prev, i)
				if err != nil {
					return nil, fmt.Errorf("tsln decode: column %q: %w", col.Name, err)
				}
				row[col.Name] = decoded
				current[i] = decoded

			case ColFloat:
				decoded, err := decodeDeltaFloat(val, col.Encoding, prev, i)
				if err != nil {
					return nil, fmt.Errorf("tsln decode: column %q: %w", col.Name, err)
				}
				row[col.Name] = decoded
				current[i] = decoded
			}
		}

		rows = append(rows, row)
		prev = current
	}

	return rows, nil
}

func decodeDeltaInt(val string, enc Encoding, prev []any, idx int) (int64, error) {
	if enc == EncDelta && prev != nil && (strings.HasPrefix(val, "+") || strings.HasPrefix(val, "-")) {
		delta, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid delta int %q: %w", val, err)
		}
		prevVal, _ := prev[idx].(int64)
		return prevVal + delta, nil
	}
	// Absolute value (first row or raw encoding)
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid int %q: %w", val, err)
	}
	return n, nil
}

func decodeDeltaFloat(val string, enc Encoding, prev []any, idx int) (float64, error) {
	if enc == EncDelta && prev != nil && (strings.HasPrefix(val, "+") || strings.HasPrefix(val, "-")) {
		delta, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid delta float %q: %w", val, err)
		}
		prevVal, _ := prev[idx].(float64)
		return prevVal + delta, nil
	}
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid float %q: %w", val, err)
	}
	return n, nil
}
