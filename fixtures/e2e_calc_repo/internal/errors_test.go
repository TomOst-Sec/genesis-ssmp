package internal

import (
	"testing"
)

func TestCalcError(t *testing.T) {
	err := NewCalcError("divide", "division by zero")
	if err.Error() != "divide: division by zero" {
		t.Fatalf("got %q", err.Error())
	}
}

func TestCalcErrorFields(t *testing.T) {
	err := NewCalcError("parse", "unexpected token")
	if err.Op != "parse" {
		t.Fatalf("Op = %q", err.Op)
	}
	if err.Message != "unexpected token" {
		t.Fatalf("Message = %q", err.Message)
	}
}
