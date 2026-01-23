package ops

import (
	"fmt"
	"math"
	"strings"
)

// Format formats a float64 result for display.
func Format(value float64) string {
	if math.IsInf(value, 0) {
		return "Infinity"
	}
	if math.IsNaN(value) {
		return "NaN"
	}
	s := fmt.Sprintf("%.10f", value)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
