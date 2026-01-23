package cli

import (
	"fmt"
	"strings"

	"github.com/genesis-ssmp/e2e-calc/ops"
)

// Run executes the CLI with the given arguments.
func Run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		PrintHelp()
		return nil
	}

	expr := strings.Join(args, " ")
	result, err := ops.Evaluate(expr)
	if err != nil {
		return fmt.Errorf("evaluation failed: %w", err)
	}

	fmt.Println(ops.Format(result))
	return nil
}
