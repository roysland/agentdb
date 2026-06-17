//go:build !treesitter

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "agentdb-parsers must be built with -tags treesitter (requires CGo and a C compiler)")
	os.Exit(1)
}
