package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/market"
)

const (
	// accuracyWindowDays is the trailing window the headline accuracy covers.
	accuracyWindowDays = 30
	// meaningfulCalls is where a hit rate stops being noise. Under it, the number
	// is printed with the warning it deserves.
	meaningfulCalls = 60
	// gradeHour and gradeMinute are when the day's session can be read whole: the
	// 16:00 ET bell plus the free feed's 15-minute REST delay.
	gradeHour, gradeMinute = 16, 30
	// gradeLater is what a command says when it is asked to grade too early.
	gradeLater = "call grades after 16:30 ET; run `tape score` later"
)

func newScoreCmd() *cobra.Command {
	var through string

	cmd := &cobra.Command{
		Use:   "score",
		Short: "Grade the calls, replay the ideas, and grade the watchlist notes",
		Long: "score settles everything the record is owed for the sessions that have closed:\n" +
			"the call of the day, the replay of every idea taken or passed on, and the bias\n" +
			"note on each watchlist symbol. Each of them is graded once.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "score")
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			if through == "" {
				through = defaultThroughDay(timeNow())
			} else if err := checkThrough(through, timeNow()); err != nil {
				return err
			}

			report, err := scoreDue(ctx, a, through)
			if err != nil {
				return err
			}
			printScoreReport(a, report, through)
			return printAccuracy(ctx, a)
		},
	}
	cmd.Flags().StringVar(&through, "through", "", "grade sessions on or before this day (default: the last closed session)")
	return cmd
}

// gradesReady is true once today's session can be read whole. Before the cutoff
// the feed still returns a half-built session, and a grade is written once.
func gradesReady(now time.Time) bool {
	et := now.In(market.Eastern())
	return et.Hour() > gradeHour || (et.Hour() == gradeHour && et.Minute() >= gradeMinute)
}

// checkThrough refuses a day the venue has not finished. --through chooses the
// last session to grade; it is not a way past the cutoff, because a grade
// written from a half-built session is the permanent one.
func checkThrough(through string, now time.Time) error {
	if _, err := time.Parse(dayLayout, through); err != nil {
		return fmt.Errorf("--through %q is not a %s date", through, dayLayout)
	}
	if !gradesReady(now) && through >= market.SessionDate(now) {
		return fmt.Errorf("--through %s covers a session that is still running; %s", through, gradeLater)
	}
	return nil
}

// defaultThroughDay grades today only once today's session is behind us,
// otherwise the last one that is.
func defaultThroughDay(now time.Time) string {
	if gradesReady(now) {
		return market.SessionDate(now)
	}
	return now.In(market.Eastern()).AddDate(0, 0, -1).Format(market.DayLayout)
}

// scoreDue runs one scoring pass over everything due through the given day: the
// calls, the proposal replays, and the watchlist notes.
func scoreDue(ctx context.Context, a *app, through string) (brief.ScoreReport, error) {
	feed, err := newMarketFeed(a.cfg)
	if err != nil {
		return brief.ScoreReport{}, err
	}
	return brief.ScoreDue(ctx, brief.ScoreDeps{
		Journal:             a.jnl,
		Sessions:            feed,
		Intraday:            feed,
		Calendar:            sessionCalendar(a),
		Costs:               costModel(a.cfg),
		Mode:                a.cfg.Mode,
		DefaultThresholdPct: a.cfg.Brief.CallThresholdPct,
	}, through)
}

// sessionCalendar is the venue's own trading calendar when the adapter has one,
// so a half day grades against the bell it rang. Nil leaves the fixed floor.
func sessionCalendar(a *app) broker.SessionCalendar {
	cal, ok := a.broker.(broker.SessionCalendar)
	if !ok {
		return nil
	}
	return cal
}

func printScoreReport(a *app, report brief.ScoreReport, through string) {
	printCalls(a, report, through)
	printReplays(a, report, through)
	printNotes(a, report, through)
}

func printCalls(a *app, report brief.ScoreReport, through string) {
	fmt.Fprintf(a.out, "\nCalls through %s\n", through)
	if len(report.Scored) == 0 && len(report.Skipped) == 0 {
		fmt.Fprintln(a.out, "  nothing left to grade.")
		return
	}

	tw := table(a.out)
	for _, s := range report.Scored {
		row(tw, "  "+s.Call.Day, s.Call.Instrument, callPhraseShort(s.Call.Direction, s.Outcome.ThresholdPct),
			"actual "+percent(s.Outcome.ActualPct), a.style.pl(boolSign(s.Outcome.Correct), mark(s.Outcome.Correct)))
	}
	tw.Flush()

	for _, skipped := range report.Skipped {
		fmt.Fprintf(a.out, "  not graded: %s\n", skipped)
	}
}

// printReplays shows what each decided idea would have done at its own levels,
// which is what makes a pass measurable rather than an opinion.
func printReplays(a *app, report brief.ScoreReport, through string) {
	if len(report.Outcomes) == 0 && len(report.OutcomesSkipped) == 0 {
		return
	}
	fmt.Fprintf(a.out, "\nReplays through %s\n", through)

	tw := table(a.out)
	for _, o := range report.Outcomes {
		net := "-"
		if o.Outcome.Filled {
			net = fmt.Sprintf("%s  %+.2fR", signedMoney(o.Outcome.NetPL), o.Outcome.RMultiple)
		}
		row(tw, "  "+o.Proposal.Day, fmt.Sprintf("#%d %s", o.Proposal.Index, o.Proposal.Symbol),
			o.Proposal.SetupID, o.Proposal.Status, exitPhrase(o.Outcome), a.style.pl(o.Outcome.NetPL, net))
	}
	tw.Flush()

	for _, skipped := range report.OutcomesSkipped {
		fmt.Fprintf(a.out, "  not replayed: %s\n", skipped)
	}
}

func printNotes(a *app, report brief.ScoreReport, through string) {
	if len(report.Notes) == 0 && len(report.NotesSkipped) == 0 {
		return
	}
	fmt.Fprintf(a.out, "\nNotes through %s\n", through)

	tw := table(a.out)
	for _, n := range report.Notes {
		row(tw, "  "+n.Score.Day, n.Score.Symbol, n.Score.Bias,
			"actual "+percent(n.Score.ActualPct), a.style.pl(boolSign(n.Score.Correct), mark(n.Score.Correct)))
	}
	tw.Flush()

	for _, skipped := range report.NotesSkipped {
		fmt.Fprintf(a.out, "  not graded: %s\n", skipped)
	}
}

func printAccuracy(ctx context.Context, a *app) error {
	correct, total, err := brief.Accuracy(ctx, a.jnl, a.cfg.Mode, accuracyWindowDays, timeNow(), a.loc)
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Fprintf(a.out, "\nno calls graded in the last %d days.\n", accuracyWindowDays)
		return nil
	}

	fmt.Fprintf(a.out, "\nlast %d days: %d/%d (%.0f%%)\n", accuracyWindowDays, correct, total, float64(correct)/float64(total)*100)

	// The warning is measured against the whole record, not the window: a good
	// month is still a month.
	_, allTime, err := a.jnl.CallAccuracy(ctx, a.cfg.Mode, "", "")
	if err != nil {
		return err
	}
	if allTime < meaningfulCalls {
		fmt.Fprintln(a.out, a.style.dim(fmt.Sprintf(
			"%d calls graded in all; this needs 3+ months to mean anything.", allTime)))
	}
	return nil
}

// boolSign turns a verdict into a sign the colour helper understands.
func boolSign(correct bool) float64 {
	if correct {
		return 1
	}
	return -1
}

func mark(correct bool) string {
	if correct {
		return "✓"
	}
	return "✗"
}
