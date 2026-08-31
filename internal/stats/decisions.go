package stats

import (
	"math"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// noiseBandPct is how close a move has to sit to the threshold before the grade
// is inside the feed's measurement error rather than a read that was right.
const noiseBandPct = 0.05

// callStats grades the call of the day. Total counts scored calls; a call whose
// session has not been graded yet is pending, not wrong.
func callStats(calls []journal.Call) CallStats {
	var s CallStats
	for _, c := range calls {
		if c.ScoredAt == nil {
			s.Pending++
			continue
		}
		s.Total++
		if c.Correct != nil && *c.Correct {
			s.Correct++
		}
		if c.ActualPct != nil && withinNoiseBand(*c.ActualPct, c.ThresholdPct) {
			s.WithinNoiseBand++
		}
	}
	if s.Total > 0 {
		s.Accuracy = float64(s.Correct) / float64(s.Total)
	}
	return s
}

// noteStats grades the watchlist bias notes. A note row exists only once it is
// scored, so nothing here is pending.
func noteStats(notes []journal.NoteScore) CallStats {
	var s CallStats
	for _, n := range notes {
		s.Total++
		if n.Correct {
			s.Correct++
		}
		if withinNoiseBand(n.ActualPct, n.ThresholdPct) {
			s.WithinNoiseBand++
		}
	}
	if s.Total > 0 {
		s.Accuracy = float64(s.Correct) / float64(s.Total)
	}
	return s
}

// withinNoiseBand is true when the move's size landed within noiseBandPct
// percentage points of the threshold it was graded against.
func withinNoiseBand(actualPct, thresholdPct float64) bool {
	return math.Abs(math.Abs(actualPct)-thresholdPct) <= noiseBandPct
}

// proposalStats counts what became of every idea and what the replays say the
// decisions cost or saved.
//
// MissedNetUSD           = Σ net over passed proposals whose replay netted > 0
// VetoedLossesAvoidedUSD = |Σ net| over passed proposals whose replay netted < 0
// ExecutionDragUSD       = Σ(replay net − actual net) over taken proposals that traded
func proposalStats(rec records) ProposalStats {
	var s ProposalStats
	outByProposal := make(map[int64]journal.ProposalOutcome, len(rec.outcomes))
	for _, o := range rec.outcomes {
		outByProposal[o.ProposalID] = o
	}
	actual := actualNetByProposal(rec)

	for _, p := range rec.proposals {
		switch p.Status {
		case journal.ProposalProposed, journal.ProposalSubmitting:
			s.Proposed++
		case journal.ProposalTaken:
			s.Taken++
		case journal.ProposalPassed:
			s.Passed++
		case journal.ProposalRejected:
			s.Rejected++
		case journal.ProposalExpired:
			s.Expired++
		case journal.ProposalUnfilled:
			s.Unfilled++
		}

		out, scored := outByProposal[p.ID]
		if !scored {
			continue
		}
		switch p.Status {
		case journal.ProposalPassed:
			switch {
			case out.NetPL > 0:
				s.PassesThatWouldHaveProfited++
				s.MissedNetUSD += out.NetPL
			case out.NetPL < 0:
				s.VetoedLossesAvoidedUSD += -out.NetPL
			}
		case journal.ProposalTaken:
			// A take whose order never traded a share has nothing to compare against.
			if net, traded := actual[p.ID]; traded {
				s.ExecutionDragUSD += out.NetPL - net
			}
		}
	}
	s.Counterfactual = counterfactual(rec.outcomes)
	return s
}

// actualNetByProposal sums what each proposal's trades really netted. One entry
// order can close in parts, so a proposal can own more than one trade.
func actualNetByProposal(rec records) map[int64]float64 {
	net := make(map[int64]float64)
	for _, t := range rec.trades {
		pid, ok := rec.proposalByOrder[t.EntryOrderID]
		if !ok {
			continue
		}
		net[pid] += t.NetPL
	}
	return net
}

// refusalStats counts guardrail refusals. Total and ByRule cover the report
// window; LastMonth reads the whole record's final 30 days, inclusive of both
// ends, because that is the span the gate checks and the two must agree.
func refusalStats(window, all []journal.Refusal, w Window) RefusalStats {
	s := RefusalStats{ByRule: make(map[string]int)}
	for _, r := range window {
		s.Total++
		s.ByRule[r.Rule]++
	}
	fromDay := lastMonthDay(w)
	for _, r := range all {
		if r.Day >= fromDay {
			s.LastMonth++
		}
	}
	return s
}

// lastMonthDay is the first day of the window's final 30 days, as an Eastern
// session date so it compares against the journal's day columns.
func lastMonthDay(w Window) string {
	et := w.To.In(market.Eastern())
	end := time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, market.Eastern())
	return end.AddDate(0, 0, -29).Format(dayLayout)
}
