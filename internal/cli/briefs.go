package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

func newBriefsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "briefs",
		Short: "List archived briefings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newRecordApp(cmd, "briefs")
			if err != nil {
				return err
			}
			defer a.Close()

			rows, err := a.jnl.ListBriefings(cmd.Context(), a.cfg.Mode, limit)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.out, "\nno briefings yet; run `tape brief`.")
				return nil
			}

			fmt.Fprintln(a.out)
			tw := table(a.out)
			row(tw, "ID", "DAY", "MODEL", "CALL", "SCORED")
			for _, b := range rows {
				call, err := a.jnl.CallByBriefing(cmd.Context(), b.ID)
				if err != nil && !errors.Is(err, journal.ErrNotFound) {
					return err
				}
				row(tw, "#"+strconv.FormatInt(b.ID, 10), b.Day, b.Model, callSummary(a, err == nil, call), scoreCell(a, err == nil, call))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "how many briefings to list, newest first")
	cmd.AddCommand(newBriefsShowCmd())
	return cmd
}

func newBriefsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id|today>",
		Short: "Re-render an archived briefing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newRecordApp(cmd, "briefs show "+args[0])
			if err != nil {
				return err
			}
			defer a.Close()

			b, err := findBriefing(cmd.Context(), a, args[0])
			if err != nil {
				return err
			}
			res, err := brief.FromArchive(cmd.Context(), a.jnl, b)
			if err != nil {
				return err
			}
			renderBriefing(a, res, true)
			return nil
		},
	}
}

// findBriefing takes a journal id or "today", which is the one the reader means
// when they do not have an id in front of them.
func findBriefing(ctx context.Context, a *app, ref string) (journal.Briefing, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if strings.EqualFold(ref, "today") {
		day := market.SessionDate(timeNow())
		b, err := a.jnl.LatestBriefing(ctx, a.cfg.Mode, day)
		if errors.Is(err, journal.ErrNotFound) {
			return journal.Briefing{}, fmt.Errorf("no briefing archived for %s; run `tape brief`", day)
		}
		return b, err
	}
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return journal.Briefing{}, fmt.Errorf("%q is not a briefing id or \"today\"", ref)
	}
	return a.jnl.BriefingByID(ctx, id)
}

// callSummary is the one-line form of a call for the list, matching how the
// scorer reads it.
func callSummary(a *app, filed bool, c journal.Call) string {
	if !filed {
		return a.style.dim("none")
	}
	return c.Instrument + " " + callPhraseShort(c.Direction, c.ThresholdPct)
}

func callPhraseShort(direction string, threshold float64) string {
	switch direction {
	case string(brief.DirFlat):
		return fmt.Sprintf("flat ±%.2g%%", threshold)
	case string(brief.DirDown):
		return fmt.Sprintf("down ≥%.2g%%", threshold)
	default:
		return fmt.Sprintf("up ≥%.2g%%", threshold)
	}
}

func scoreCell(a *app, filed bool, c journal.Call) string {
	if !filed || c.Correct == nil || c.ActualPct == nil {
		return "-"
	}
	mark := "✗"
	if *c.Correct {
		mark = "✓"
	}
	return a.style.pl(*c.ActualPct, mark+" "+percent(*c.ActualPct))
}
