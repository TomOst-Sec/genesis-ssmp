package ops

import "math"

// Pow raises base to the power of exp.
func Pow(base, exp float64) float64 {
	return math.Pow(base, exp)
}

// Sqrt returns the square root of x.
func Sqrt(x float64) float64 {
	return math.Sqrt(x)
}

// Abs returns the absolute value of x.
func Abs(x float64) float64 {
	return math.Abs(x)
}
