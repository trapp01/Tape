package brief

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/trapp01/tape/internal/counterfactual"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// maxScaleDeviation is how far the fill bar's open may sit from the proposed
// entry before the two are priced on different scales rather than moving.
const maxScaleDeviation = 0.25

// ScoredOutcome is one decided idea next to what its replay says it would have
// done at its own levels.
type ScoredOutcome struct {
	Proposal journal.Proposal
	Outcome  journal.ProposalOutcome
}

// replayProposals grades every decided idea nothing has replayed yet against the
// path its session actually took. A pass, an expiry, a rejection and a take all
// replay the same way, which is what makes a veto measurable.
func replayProposals(ctx context.Context, d ScoreDeps, throughDay string, report *ScoreReport) error {
	due, err := d.Journal.UnscoredProposals(ctx, d.Mode, throughDay)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}
	if d.Intraday == nil {
		report.OutcomesSkipped = append(report.OutcomesSkipped,
			fmt.Sprintf("%d idea(s): no intraday data source configured", len(due)))
		return nil
	}

	for _, p := range due {
		out, skip, err := replay(ctx, d, p)
		if err != nil {
			return err
		}
		if skip != "" {
			report.OutcomesSkipped = append(report.OutcomesSkipped,
				fmt.Sprintf("#%d %s on %s: %s", p.Index, p.Symbol, p.Day, skip))
			continue
		}
		report.Outcomes = append(report.Outcomes, ScoredOutcome{Proposal: p, Outcome: out})
	}
	return nil
}

// replay simulates one idea and files the outcome. A non-empty skip leaves the
// idea unreplayed; the error is reserved for a broken journal.
func replay(ctx context.Context, d ScoreDeps, p journal.Proposal) (journal.ProposalOutcome, string, error) {
	if _, skip := sessionFor(ctx, d, p.Symbol, p.Day); skip != "" {
		return journal.ProposalOutcome{}, skip, nil
	}
	bars, err := d.Intraday.SessionBars(ctx, p.Symbol, p.Day)
	if err != nil {
		return journal.ProposalOutcome{}, err.Error(), nil
	}
	// Levels that cannot describe a long trade, and a session with no prints, are
	// reported rather than filed: a wrong replay would be the permanent one.
	out, err := counterfactual.Simulate(p, bars, d.Costs, counterfactual.DefaultRules())
	if err != nil {
		return journal.ProposalOutcome{}, err.Error(), nil
	}
	if skip := checkScale(p, bars, out); skip != "" {
		return journal.ProposalOutcome{}, skip, nil
	}
	if err := d.Journal.InsertProposalOutcome(ctx, &out); err != nil {
		if errors.Is(err, journal.ErrAlreadyScored) {
			return journal.ProposalOutcome{}, "already replayed", nil
		}
		return journal.ProposalOutcome{}, "", err
	}
	return out, "", nil
}

// checkScale refuses a replay whose bars are priced on a different scale from
// the proposal. The feed returns split-adjusted prices, so a split between the
// session and the replay makes every level fiction.
func checkScale(p journal.Proposal, bars []market.Bar, out journal.ProposalOutcome) string {
	if !out.Filled || out.FilledAt == nil || p.Entry <= 0 {
		return ""
	}
	open, ok := barOpenAt(bars, *out.FilledAt)
	if !ok {
		return ""
	}
	if dev := math.Abs(open-p.Entry) / p.Entry; dev > maxScaleDeviation {
		return fmt.Sprintf("the fill bar opened at %.2f, %.0f%% from the proposed entry %.2f; the feed's prices look adjusted",
			open, dev*100, p.Entry)
	}
	return ""
}

// barOpenAt finds the open of the bar the replay filled on.
func barOpenAt(bars []market.Bar, at time.Time) (float64, bool) {
	for _, b := range bars {
		if b.Time.Equal(at) {
			return b.Open, true
		}
	}
	return 0, false
}
