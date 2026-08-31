package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/trapp01/tape/internal/brief"
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
		Short: "Grade the briefing's calls against what the sessions did",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd, "score")
			if err != nil {
				return err
			}
			defer a.Close()

			ctx := cmd.Context()
			if through == "" {
				through = defaultThroughDay(timeNow())
			} else if _, err := time.Parse(dayLayout, through); err != nil {
				return fmt.Errorf("--through %q is not a %s date", through, dayLayout)
			}

			report, err := scoreCalls(ctx, a, through)
			if err != nil {
				return err
			}
			printScoreReport(a, report, through)
			return printAccuracy(ctx, a)
		},
	}
	cmd.Flags().StringVar(&through, "through", "", "grade calls filed on or before this day (default: the last closed session)")
	return cmd
}

// gradesReady is true once today's session can be read whole. Before the cutoff
// the feed still returns a half-built session, and a call is graded once.
func gradesReady(now time.Time) bool {
	et := now.In(market.Eastern())
	return et.Hour() > gradeHour || (et.Hour() == gradeHour && et.Minute() >= gradeMinute)
}

// defaultThroughDay grades today only once today's session is behind us,
// otherwise the last one that is.
func defaultThroughDay(now time.Time) string {
	if gradesReady(now) {
		return market.SessionDate(now)
	}
	return now.In(market.Eastern()).AddDate(0, 0, -1).Format(market.DayLayout)
}

// scoreCalls grades every call due through the given day against that session's
// own open and close.
func scoreCalls(ctx context.Context, a *app, through string) (brief.ScoreReport, error) {
	feed, err := newMarketFeed(a.cfg)
	if err != nil {
		return brief.ScoreReport{}, err
	}
	return brief.ScoreDue(ctx, a.jnl, feed, a.cfg.Mode, through)
}

func printScoreReport(a *app, report brief.ScoreReport, through string) {
	fmt.Fprintf(a.out, "\nCalls through %s\n", through)
	if len(report.Scored) == 0 && len(report.Skipped) == 0 {
		fmt.Fprintln(a.out, "  nothing left to grade.")
		return
	}

	tw := table(a.out)
	for _, s := range report.Scored {
		mark := "✗"
		if s.Outcome.Correct {
			mark = "✓"
		}
		row(tw, "  "+s.Call.Day, s.Call.Instrument, callPhraseShort(s.Call.Direction, s.Outcome.ThresholdPct),
			"actual "+percent(s.Outcome.ActualPct), a.style.pl(boolSign(s.Outcome.Correct), mark))
	}
	tw.Flush()

	for _, skipped := range report.Skipped {
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
