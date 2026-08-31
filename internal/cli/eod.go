package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/trading"
)

func newEODCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eod",
		Short: "Flatten everything and recap the day",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "end of day")
			if err != nil {
				return err
			}
			defer a.Close()

			return runEOD(cmd.Context(), a)
		},
	}
}

// runEOD ends the day flat and recaps it. A slate that will not close is a
// problem to report, never a reason to leave a position on overnight.
func runEOD(ctx context.Context, a *app) error {
	day := a.slateDay(ctx)
	var problems []string
	if err := expireSlate(ctx, a, day); err != nil {
		problems = append(problems, err.Error())
	}

	rep, err := a.engine.Flatten(ctx)
	if err != nil {
		return err
	}
	rep.Problems = append(problems, rep.Problems...)
	if err := markUnfilled(ctx, a, day); err != nil {
		rep.Problems = append(rep.Problems, err.Error())
	}

	recap, err := a.jnl.DayRecap(ctx, a.engine.Today(), a.loc, a.cfg.Mode)
	if err != nil {
		return err
	}
	refusals, err := a.jnl.RefusalCount(ctx, a.cfg.Mode, day, day)
	if err != nil {
		return err
	}

	printFlatten(a, rep)
	if err := printRecap(a, recap, refusals); err != nil {
		return err
	}
	scoreToday(ctx, a)
	return flattenVerdict(a, rep)
}

// expireSlate closes out the ideas nobody decided. An undecided proposal is not
// a proposal that can be taken tomorrow; it is one the session ran out on.
var expireSlate = func(ctx context.Context, a *app, day string) error {
	n, err := a.jnl.ExpireOpenProposals(ctx, a.cfg.Mode, day, timeNow().UTC())
	if err != nil {
		return err
	}
	if n > 0 {
		fmt.Fprintf(a.out, "\nexpired %d open proposal(s)\n", n)
	}
	return nil
}

// markUnfilled runs after the venue has been cancelled and flattened, so a take
// whose order died without trading a share stops reading as a position.
func markUnfilled(ctx context.Context, a *app, day string) error {
	ids, err := a.jnl.UnfilledForDay(ctx, a.cfg.Mode, day, timeNow().UTC())
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		fmt.Fprintf(a.out, "\n%d taken proposal(s) never filled\n", len(ids))
	}
	return nil
}

func printFlatten(a *app, rep trading.FlattenReport) {
	fmt.Fprintln(a.out, "\nFlatten")
	tw := table(a.out)
	pair(tw, "orders cancelled", strconv.Itoa(len(rep.Canceled)))
	pair(tw, "positions closed", strconv.Itoa(len(rep.Orders)))
	pair(tw, "fills recorded", strconv.Itoa(len(rep.Fills)))
	tw.Flush()

	for _, o := range rep.Orders {
		fmt.Fprintf(a.out, "  closed %s %d %s (journal #%d, %s)\n", o.Side, o.Qty, o.Symbol, o.ID, o.Status)
	}
}

func printRecap(a *app, recap journal.DayRecap, refusals int) error {
	fmt.Fprintf(a.out, "\nRecap %s\n", recap.Day.Format("2006-01-02"))
	tw := table(a.out)
	pair(tw, "orders", strconv.Itoa(recap.OrdersCount))
	pair(tw, "trades closed", strconv.Itoa(len(recap.Trades)))
	pair(tw, "wins / losses", fmt.Sprintf("%d / %d", recap.Wins, recap.Losses))
	pair(tw, "refusals today", strconv.Itoa(refusals))
	pair(tw, "gross", a.style.pl(recap.GrossPL, signedMoney(recap.GrossPL)))
	pair(tw, "costs on closed trades", money(recap.Costs))
	pair(tw, "costs on today's fills", money(recap.FillCosts))
	pair(tw, "net", a.style.pl(recap.NetPL, signedMoney(recap.NetPL)))
	return tw.Flush()
}

// scoreToday settles the day now that the session is over: the call, the replays,
// and the notes. It cannot fail the command — eod exists to end the day flat, and
// an ungraded record is a line to read, not a reason to stop.
func scoreToday(ctx context.Context, a *app) {
	now := timeNow()
	if !gradesReady(now) {
		fmt.Fprintf(a.out, "\nCall\n  %s.\n", gradeLater)
		return
	}
	through := defaultThroughDay(now)
	report, err := scoreDue(ctx, a, through)
	if err != nil {
		fmt.Fprintf(a.out, "\nCall\n  not graded: %v\n", err)
		return
	}
	printScoreReport(a, report, through)
}

// flattenVerdict fails the command when the day did not end flat, because a
// position carried overnight is the thing eod exists to prevent.
func flattenVerdict(a *app, rep trading.FlattenReport) error {
	if len(rep.Problems) == 0 && len(rep.StillOpen) == 0 {
		fmt.Fprintln(a.out, "\nflat.")
		return nil
	}

	fmt.Fprintln(a.out, "\nNOT FLAT")
	for _, p := range rep.Problems {
		fmt.Fprintf(a.out, "  problem: %s\n", p)
	}
	for _, p := range rep.StillOpen {
		fmt.Fprintf(a.out, "  still open: %d %s at %s\n", p.Qty, p.Symbol, price(p.CurrentPrice))
	}
	return fmt.Errorf("eod did not finish: %d position(s) still open, %d problem(s); close them at the broker",
		len(rep.StillOpen), len(rep.Problems))
}
