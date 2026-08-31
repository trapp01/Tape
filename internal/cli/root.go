// Package cli wires cobra commands to the trading, journal, and LLM packages.
// Commands stay thin: parse flags, call one function, print the result.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Set at build time via -ldflags "-X github.com/trapp01/tape/internal/cli.version=...".
var version = "dev"

var cfgPath string

// Execute runs the root command and prints any error to stderr.
func Execute() error {
	root := NewRootCmd()
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tape:", err)
	}
	return err
}

// NewRootCmd builds the command tree. Subcommands are registered here explicitly
// so the full surface is visible in one place.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tape",
		Short:         "A trading copilot for the terminal",
		Long:          "tape reads the market every morning, proposes trades you confirm or veto,\njournals every decision, and grades itself against what actually happened.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file (default $TAPE_HOME/config.toml, then ~/.tape/config.toml)")
	root.AddCommand(
		newInitCmd(),
		newStatusCmd(),
		newBriefCmd(),
		newBriefsCmd(),
		newScoreCmd(),
		newWatchlistCmd(),
		newPlaybookCmd(),
		newBuyCmd(),
		newSellCmd(),
		newPosCmd(),
		newOrdersCmd(),
		newWatchCmd(),
		newEODCmd(),
		newModeCmd(),
		newLLMCmd(),
		newVersionCmd(),
	)
	return root
}
