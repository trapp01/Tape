package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/config"
)

func newWatchlistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchlist",
		Short: "Show or edit the symbols the briefing reads",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newWatchlistLsCmd(), newWatchlistAddCmd(), newWatchlistRmCmd())
	return cmd
}

func newWatchlistLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Short:   "List the watchlist",
		Args:    cobra.NoArgs,
		Aliases: []string{"list"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, style := bareStyler(cmd)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			style.banner(out, cfg.Mode, "watchlist")
			printWatchlist(out, cfg)
			return nil
		},
	}
}

func newWatchlistAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add SYM [SYM...]",
		Short: "Add symbols to the watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return editWatchlist(cmd, "watchlist add", func(current []string) []string {
				return upperUnique(append(slices.Clone(current), args...))
			})
		},
	}
}

func newWatchlistRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm SYM [SYM...]",
		Short:   "Remove symbols from the watchlist",
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"remove"},
		RunE: func(cmd *cobra.Command, args []string) error {
			drop := upperUnique(args)
			return editWatchlist(cmd, "watchlist rm", func(current []string) []string {
				kept := make([]string, 0, len(current))
				for _, s := range upperUnique(current) {
					if !slices.Contains(drop, s) {
						kept = append(kept, s)
					}
				}
				return kept
			})
		},
	}
}

// editWatchlist rewrites [brief] watchlist in place, dropping any credential the
// environment supplied so the file never gains a key it did not have.
func editWatchlist(cmd *cobra.Command, headline string, edit func([]string) []string) error {
	out, style := bareStyler(cmd)

	path, err := resolveConfigPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	style.banner(out, cfg.Mode, headline)

	cfg.Brief.Watchlist = edit(cfg.Brief.Watchlist)
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Write(path, cfg.WithoutEnvSecrets()); err != nil {
		return err
	}
	printWatchlist(out, cfg)
	return nil
}

func printWatchlist(out io.Writer, cfg config.Config) {
	if len(cfg.Brief.Watchlist) == 0 {
		fmt.Fprintln(out, "\nwatchlist is empty; add symbols with `tape watchlist add SPY QQQ`.")
		return
	}
	fmt.Fprintf(out, "\n%s\n", strings.Join(cfg.Brief.Watchlist, "  "))
	fmt.Fprintf(out, "%d symbol(s) · indexes %s · regime %s\n",
		len(cfg.Brief.Watchlist), strings.Join(cfg.Brief.IndexSymbols, " "), cfg.Brief.RegimeSymbol)
}

// upperUnique uppercases, drops blanks and repeats, and keeps the order the user
// wrote them in.
func upperUnique(symbols []string) []string {
	seen := make(map[string]bool, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
