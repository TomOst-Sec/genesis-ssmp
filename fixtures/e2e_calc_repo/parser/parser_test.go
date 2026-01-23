package parser

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	tokens := Tokenize("1 + 2 * 3")
	// Expect: 1, +, 2, *, 3, EOF
	if len(tokens) != 6 {
		t.Fatalf("got %d tokens, want 6", len(tokens))
	}
	if tokens[0].Kind != TokenNumber || tokens[0].Value != "1" {
		t.Errorf("token[0] = %v", tokens[0])
	}
	if tokens[1].Kind != TokenPlus {
		t.Errorf("token[1] = %v", tokens[1])
	}
}

func TestParseSimple(t *testing.T) {
	expr, err := Parse("2 + 3")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	result, err := expr.Eval()
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if result != 5 {
		t.Fatalf("2 + 3 = %v, want 5", result)
	}
}

func TestParsePrecedence(t *testing.T) {
	expr, err := Parse("2 + 3 * 4")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	result, _ := expr.Eval()
	if result != 14 {
		t.Fatalf("2 + 3 * 4 = %v, want 14", result)
	}
}

func TestParseParens(t *testing.T) {
	expr, err := Parse("(2 + 3) * 4")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	result, _ := expr.Eval()
	if result != 20 {
		t.Fatalf("(2 + 3) * 4 = %v, want 20", result)
	}
}

func TestDivisionByZero(t *testing.T) {
	expr, err := Parse("10 / 0")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, err = expr.Eval()
	if err == nil {
		t.Fatal("expected division by zero error")
	}
}
