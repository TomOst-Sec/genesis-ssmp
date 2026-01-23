package cli

import "fmt"

// PrintHelp prints usage information.
func PrintHelp() {
	fmt.Println("e2e-calc: a simple calculator")
	fmt.Println()
	fmt.Println("Usage: e2e-calc <expression>")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  e2e-calc 2 + 3")
	fmt.Println("  e2e-calc \"(4 + 5) * 2\"")
}
