// Package main is the Roundpen CLI (roundpen).
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: roundpen <command>\n")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println("roundpen 0.0.0-dev")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(2)
	}
}
