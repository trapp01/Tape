package brief

import (
	"context"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

type ScoredCall struct {
	Call    journal.Call
	Outcome Outcome
}

// ScoreReport is what one scoring pass did. Skipped names each call it could not
// grade and why; those stay unscored for the next run.
type ScoreReport struct {
	Scored  []ScoredCall
	Skipped []string
}

// ScoreDue grades every call filed for a session on or before throughDay that
// nothing has graded yet, against that session's own open and close. A session
// still in progress is left alone: a call is graded once, so an early grade
// would be the permanent one.
func ScoreDue(ctx context.Context, jnl *journal.Store, sessions market.SessionProvider, mode, throughDay string) (ScoreReport, error) {
	if jnl == nil {
		return ScoreReport{}, fmt.Errorf("brief: scoring %s through %s: no journal configured", mode, throughDay)
	}
	if sessions == nil {
		return ScoreReport{}, fmt.Errorf("brief: scoring %s through %s: no session provider configured", mode, throughDay)
	}

	due, err := jnl.UnscoredCalls(ctx, mode, throughDay)
	if err != nil {
		return ScoreReport{}, err
	}

	var report ScoreReport
	for _, c := range due {
		outcome, skip, err := grade(ctx, sessions, c)
		if err != nil {
			return report, err
		}
		if skip != "" {
			report.Skipped = append(report.Skipped, skip)
			continue
		}
		if err := jnl.ScoreCall(ctx, c.ID, outcome.Open, outcome.Close, outcome.ActualPct, outcome.Correct, time.Time{}); err != nil {
			return report, err
		}
		report.Scored = append(report.Scored, ScoredCall{Call: c, Outcome: outcome})
	}
	return report, nil
}

// grade reads one call's session and scores it. A non-empty skip means the call
// stays open for a later run; the error is reserved for a broken journal.
func grade(ctx context.Context, sessions market.SessionProvider, c journal.Call) (Outcome, string, error) {
	s, err := sessions.Session(ctx, c.Instrument, c.Day)
	if err != nil {
		return Outcome{}, fmt.Sprintf("%s on %s: %v", c.Instrument, c.Day, err), nil
	}
	if !s.Complete {
		return Outcome{}, fmt.Sprintf("%s on %s: session not final yet", c.Instrument, c.Day), nil
	}
	// The row's threshold is the effective one, fixed when the call was filed.
	outcome, err := Score(callOf(c), s.Open, s.Close, c.ThresholdPct)
	if err != nil {
		return Outcome{}, err.Error(), nil
	}
	return outcome, "", nil
}

// callOf rebuilds the scored shape from the journal row. The threshold is not
// copied: the row carries it, and the scorer takes it from there.
func callOf(c journal.Call) Call {
	return Call{Instrument: c.Instrument, Direction: Direction(c.Direction), Rationale: c.Rationale}
}

// Accuracy counts the scored calls in the trailing window ending today. It is a
// count, not a verdict: a few dozen calls cannot separate skill from a coin.
func Accuracy(ctx context.Context, jnl *journal.Store, mode string, days int, now time.Time, loc *time.Location) (correct, total int, err error) {
	if jnl == nil {
		return 0, 0, fmt.Errorf("brief: call accuracy for %s: no journal configured", mode)
	}
	if loc == nil {
		loc = time.UTC
	}
	if days < 1 {
		days = 1
	}
	to := now.In(loc)
	from := to.AddDate(0, 0, -(days - 1))
	return jnl.CallAccuracy(ctx, mode, from.Format(dayLayout), to.Format(dayLayout))
}
