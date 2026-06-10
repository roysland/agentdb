package main

import (
	"context"
	"os"

	"github.com/roysland/agentdb/cmd"
)

func main() {
	if err := cmd.Execute(context.Background()); err != nil {
		os.Exit(1)
	}
}
