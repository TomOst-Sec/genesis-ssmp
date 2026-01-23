package ops

import "github.com/genesis-ssmp/e2e-calc/parser"

// Evaluate parses and evaluates an expression string.
func Evaluate(input string) (float64, error) {
	expr, err := parser.Parse(input)
	if err != nil {
		return 0, err
	}
	return expr.Eval()
}
