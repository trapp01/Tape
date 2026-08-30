package main

import (
	"os"

	"github.com/trapp01/tape/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
