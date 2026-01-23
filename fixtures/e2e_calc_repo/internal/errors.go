package internal

import "fmt"

// CalcError represents a calculator-specific error.
type CalcError struct {
	Op      string
	Message string
}

func (e *CalcError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

// NewCalcError creates a new CalcError.
func NewCalcError(op, message string) *CalcError {
	return &CalcError{Op: op, Message: message}
}
