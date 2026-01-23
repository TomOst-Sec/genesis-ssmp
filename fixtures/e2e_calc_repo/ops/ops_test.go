package ops

import (
	"math"
	"testing"
)

func TestEvaluate(t *testing.T) {
	result, err := Evaluate("2 + 3 * 4")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result != 14 {
		t.Fatalf("got %v, want 14", result)
	}
}

func TestPow(t *testing.T) {
	if Pow(2, 10) != 1024 {
		t.Fatalf("Pow(2,10) = %v", Pow(2, 10))
	}
}

func TestSqrt(t *testing.T) {
	if Sqrt(16) != 4 {
		t.Fatalf("Sqrt(16) = %v", Sqrt(16))
	}
}

func TestAbs(t *testing.T) {
	if Abs(-5) != 5 {
		t.Fatalf("Abs(-5) = %v", Abs(-5))
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{3.14, "3.14"},
		{42.0, "42"},
		{math.Inf(1), "Infinity"},
		{math.NaN(), "NaN"},
	}
	for _, tc := range cases {
		got := Format(tc.in)
		if got != tc.want {
			t.Errorf("Format(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
