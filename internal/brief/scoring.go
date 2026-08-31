package brief

import (
	"context"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// finalPrintWindow is how close to the bell the last regular print must land for
// the session to have run out. A thin symbol can miss the last minute or two.
const finalPrintWindow = 5 * time.Minute

// ScoreDeps is everything one scoring pass reads. Sessions grades the call and
// the watchlist notes; Intraday replays the proposals against the path price took.
type ScoreDeps struct {
	Journal  *journal.Store
	Sessions market.SessionProvider
	// Intraday is optional: without it the decided ideas stay unreplayed and the
	// pass reports that rather than guessing at them.
	Intraday market.IntradayProvider
	// Calendar is optional: with it a session is complete once the last print
	// reaches the venue's own close, so half days grade. Without it the fixed
	// 15:55 ET floor stands, and an early close is never gradeable.
	Calendar broker.SessionCalendar
	Costs    costs.Model
	Mode     string
	// DefaultThresholdPct grades a watch note whose briefing filed no call.
	DefaultThresholdPct float64
}

type ScoredCall struct {
	Call    journal.Call
	Outcome Outcome
}

// ScoreReport is what one scoring pass did. Each kind carries its own skip list,
// naming what it could not grade and why; those stay open for the next run.
type ScoreReport struct {
	Scored  []ScoredCall
	Skipped []string
	// Outcomes are the ideas replayed this pass.
	Outcomes        []ScoredOutcome
	OutcomesSkipped []string
	// Notes are the watchlist bias notes graded this pass.
	Notes        []ScoredNote
	NotesSkipped []string
}

// ScoreDue grades everything the record is still owed for sessions on or before
// throughDay: the call of the day, the replay of every decided proposal, and the
// watchlist bias notes. A session still in progress is left alone in all three,
// because each of them is graded once and an early grade would be the permanent one.
func ScoreDue(ctx context.Context, d ScoreDeps, throughDay string) (ScoreReport, error) {
	if d.Journal == nil {
		return ScoreReport{}, fmt.Errorf("brief: scoring %s through %s: no journal configured", d.Mode, throughDay)
	}
	if d.Sessions == nil {
		return ScoreReport{}, fmt.Errorf("brief: scoring %s through %s: no session provider configured", d.Mode, throughDay)
	}

	var report ScoreReport
	if err := scoreCalls(ctx, d, throughDay, &report); err != nil {
		return report, err
	}
	if err := replayProposals(ctx, d, throughDay, &report); err != nil {
		return report, err
	}
	if err := gradeNotes(ctx, d, throughDay, &report); err != nil {
		return report, err
	}
	return report, nil
}

// scoreCalls grades every call filed for a session on or before throughDay that
// nothing has graded yet, against that session's own open and close.
func scoreCalls(ctx context.Context, d ScoreDeps, throughDay string, report *ScoreReport) error {
	due, err := d.Journal.UnscoredCalls(ctx, d.Mode, throughDay)
	if err != nil {
		return err
	}
	for _, c := range due {
		outcome, skip, err := grade(ctx, d, c)
		if err != nil {
			return err
		}
		if skip != "" {
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s on %s: %s", c.Instrument, c.Day, skip))
			continue
		}
		if err := d.Journal.ScoreCall(ctx, c.ID, outcome.Open, outcome.Close, outcome.ActualPct, outcome.Correct, time.Time{}); err != nil {
			return err
		}
		report.Scored = append(report.Scored, ScoredCall{Call: c, Outcome: outcome})
	}
	return nil
}

// grade reads one call's session and scores it. A non-empty skip means the call
// stays open for a later run; the error is reserved for a broken journal.
func grade(ctx context.Context, d ScoreDeps, c journal.Call) (Outcome, string, error) {
	s, skip := sessionFor(ctx, d, c.Instrument, c.Day)
	if skip != "" {
		return Outcome{}, skip, nil
	}
	// The row's threshold is the effective one, fixed when the call was filed.
	outcome, err := Score(callOf(c), s.Open, s.Close, c.ThresholdPct)
	if err != nil {
		return Outcome{}, err.Error(), nil
	}
	return outcome, "", nil
}

// sessionFor reads one session and refuses everything a grade cannot be written
// from. A non-empty reason leaves the record open for a later run.
func sessionFor(ctx context.Context, d ScoreDeps, symbol, day string) (market.Session, string) {
	s, err := d.Sessions.Session(ctx, symbol, day)
	if err != nil {
		return market.Session{}, err.Error()
	}
	if !ranToTheBell(ctx, d, s) {
		return market.Session{}, "session not final yet"
	}
	return s, ""
}

// ranToTheBell judges whether the session finished. The venue calendar decides
// when it answers, so a 13:00 close is a whole day; the provider's fixed floor
// is the fallback when there is no calendar or the venue cannot say.
func ranToTheBell(ctx context.Context, d ScoreDeps, s market.Session) bool {
	if d.Calendar == nil || s.LastBarAt.IsZero() {
		return s.Complete
	}
	_, close, err := d.Calendar.SessionHours(ctx, s.Day)
	if err != nil {
		return s.Complete
	}
	return !s.LastBarAt.Before(close.Add(-finalPrintWindow))
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
