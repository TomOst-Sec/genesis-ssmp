package cli

import (
	"testing"
)

func TestRunHelp(t *testing.T) {
	err := Run([]string{"help"})
	if err != nil {
		t.Fatalf("Run(help): %v", err)
	}
}

func TestRunExpression(t *testing.T) {
	err := Run([]string{"2", "+", "3"})
	if err != nil {
		t.Fatalf("Run(2+3): %v", err)
	}
}

func TestRunEmpty(t *testing.T) {
	err := Run([]string{})
	if err != nil {
		t.Fatalf("Run(empty): %v", err)
	}
}

func TestRunInvalid(t *testing.T) {
	err := Run([]string{"abc"})
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}
