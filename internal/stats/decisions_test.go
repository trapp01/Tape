package stats

import (
	"testing"

	"github.com/trapp01/tape/internal/journal"
)

func TestPassesAndVetoesAreScoredBothWays(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		proposals: []journal.Proposal{
			{ID: 10, Day: "2026-08-03", SetupID: "B1", Status: journal.ProposalPassed, Reason: "no volume"},
			{ID: 11, Day: "2026-08-04", SetupID: "B1", Status: journal.ProposalPassed, Reason: "wide spread"},
			{ID: 12, Day: "2026-08-05", SetupID: "B1", Status: journal.ProposalPassed, Reason: "late"},
			{ID: 13, Day: "2026-08-06", SetupID: "B1", Status: journal.ProposalExpired},
			{ID: 14, Day: "2026-08-07", SetupID: "B1", Status: journal.ProposalRejected},
		},
		outcomes: []journal.ProposalOutcome{
			{ProposalID: 10, Day: "2026-08-03", Filled: true, NetPL: 120, RMultiple: 2},
			{ProposalID: 11, Day: "2026-08-04", Filled: true, NetPL: -80, RMultiple: -1},
			{ProposalID: 12, Day: "2026-08-05", Filled: false},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	p := rep.Proposals
	if p.Passed != 3 || p.Expired != 1 || p.Rejected != 1 {
		t.Fatalf("status counts = %+v", p)
	}
	if p.PassesThatWouldHaveProfited != 1 || !near(p.MissedNetUSD, 120) {
		t.Fatalf("one pass would have paid $120, got %+v", p)
	}
	if !near(p.VetoedLossesAvoidedUSD, 80) {
		t.Fatalf("the losing pass saved $80, got %v", p.VetoedLossesAvoidedUSD)
	}
	if got := p.Counterfactual; got.Replayed != 3 || got.Filled != 2 || got.Wins != 1 || got.Losses != 1 {
		t.Fatalf("counterfactual = %+v", got)
	}
	if !near(p.Counterfactual.NetPL, 40) || !near(p.Counterfactual.AvgRMultiple, 0.5) {
		t.Fatalf("counterfactual totals = %+v", p.Counterfactual)
	}
}

func TestExecutionDragComparesTheReplayToTheFill(t *testing.T) {
	pid := int64(20)
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		trades: []journal.Trade{
			// One entry closed in two parts still belongs to one proposal.
			{ID: 1, ClosedAt: at("2026-08-03", 12), NetPL: 40, EntryOrderID: 20},
			{ID: 2, ClosedAt: at("2026-08-03", 14), NetPL: 20, EntryOrderID: 20},
		},
		orders: map[int64]journal.Order{20: {ID: 20, ProposalID: &pid}},
		proposals: []journal.Proposal{
			{ID: 20, Day: "2026-08-03", SetupID: "B1", Status: journal.ProposalTaken},
			{ID: 21, Day: "2026-08-04", SetupID: "B1", Status: journal.ProposalTaken},
		},
		outcomes: []journal.ProposalOutcome{
			{ProposalID: 20, Day: "2026-08-03", Filled: true, NetPL: 100, RMultiple: 2},
			// A take whose order never traded has no fill to compare against.
			{ProposalID: 21, Day: "2026-08-04", Filled: true, NetPL: 500, RMultiple: 3},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if !near(rep.Proposals.ExecutionDragUSD, 40) {
		t.Fatalf("drag = %v, want $100 replay less the $60 the fills made", rep.Proposals.ExecutionDragUSD)
	}
}

func TestCallAndNoteAccuracyWithPendingAndNoiseBand(t *testing.T) {
	yes, no := true, false
	scored := at("2026-08-03", 16)
	pct := func(v float64) *float64 { return &v }
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		calls: []journal.Call{
			{ID: 1, Day: "2026-08-03", ThresholdPct: 0.4, ScoredAt: &scored, Correct: &yes, ActualPct: pct(1.2)},
			{ID: 2, Day: "2026-08-04", ThresholdPct: 0.5, ScoredAt: &scored, Correct: &yes, ActualPct: pct(-0.9)},
			// Both of these were decided inside the feed's own error.
			{ID: 3, Day: "2026-08-05", ThresholdPct: 0.4, ScoredAt: &scored, Correct: &yes, ActualPct: pct(0.42)},
			{ID: 4, Day: "2026-08-06", ThresholdPct: 0.4, ScoredAt: &scored, Correct: &no, ActualPct: pct(-0.44)},
			{ID: 5, Day: "2026-08-07", ThresholdPct: 0.4},
		},
		notes: []journal.NoteScore{
			{ID: 1, Day: "2026-08-03", Symbol: "NVDA", Bias: "bullish", ThresholdPct: 0.5, ActualPct: 1.4, Correct: true},
			{ID: 2, Day: "2026-08-03", Symbol: "AMD", Bias: "bearish", ThresholdPct: 0.5, ActualPct: -0.8, Correct: true},
			{ID: 3, Day: "2026-08-04", Symbol: "TSLA", Bias: "neutral", ThresholdPct: 0.5, ActualPct: 0.52, Correct: false},
			{ID: 4, Day: "2026-08-04", Symbol: "AAPL", Bias: "bullish", ThresholdPct: 0.5, ActualPct: 0.9, Correct: true},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	c := rep.Calls
	if c.Total != 4 || c.Correct != 3 || c.Pending != 1 || !near(c.Accuracy, 0.75) {
		t.Fatalf("calls = %+v", c)
	}
	if c.WithinNoiseBand != 2 {
		t.Fatalf("both near-threshold calls should be flagged, got %+v", c)
	}
	n := rep.Notes
	if n.Total != 4 || n.Correct != 3 || n.Pending != 0 || !near(n.Accuracy, 0.75) {
		t.Fatalf("notes = %+v", n)
	}
	if n.WithinNoiseBand != 1 {
		t.Fatalf("the 0.52 note sat 0.02 off its threshold, got %+v", n)
	}
	// Five call days, two of which also carry notes.
	if rep.Sessions != 5 {
		t.Fatalf("sessions = %d, want 5", rep.Sessions)
	}
}

func TestRefusalsCountTheFinalThirtyDaysInclusively(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		refusals: []journal.Refusal{
			{ID: 1, Day: "2026-07-31", Rule: "risk cap", At: at("2026-07-31", 9)},
			{ID: 2, Day: "2026-08-01", Rule: "risk cap", At: at("2026-08-01", 9)},
			{ID: 3, Day: "2026-08-15", Rule: "flat by close", At: at("2026-08-15", 14)},
			{ID: 4, Day: "2026-08-30", Rule: "flat by close", At: at("2026-08-30", 14)},
		},
	}
	rep := run(t, src, testWindow("2026-06-01", "2026-08-30"), testGate())

	if rep.Refusals.Total != 4 {
		t.Fatalf("total = %d, want 4", rep.Refusals.Total)
	}
	// The window ends 2026-08-30, so the final thirty days open on 2026-08-01.
	if rep.Refusals.LastMonth != 3 {
		t.Fatalf("last month = %d, want the three from 2026-08-01 on", rep.Refusals.LastMonth)
	}
	if rep.Refusals.ByRule["risk cap"] != 2 || rep.Refusals.ByRule["flat by close"] != 2 {
		t.Fatalf("by rule = %+v", rep.Refusals.ByRule)
	}
	if c := checkByName(t, rep, "refusals last month"); c.Passed {
		t.Fatalf("three breaches must fail a zero-breach gate: %+v", c)
	}

	// A short window narrows the breakdown, never the thirty-day count the gate
	// checks: the two must agree about whether the last month was clean.
	short := run(t, src, testWindow("2026-08-25", "2026-08-30"), testGate())
	if short.Refusals.Total != 1 {
		t.Fatalf("total = %d, want the one breach inside the short window", short.Refusals.Total)
	}
	if short.Refusals.LastMonth != 3 {
		t.Fatalf("last month = %d under a six-day window, want the same 3 the gate reads", short.Refusals.LastMonth)
	}
	if a, b := checkByName(t, rep, "refusals last month"), checkByName(t, short, "refusals last month"); a != b {
		t.Fatalf("the gate check disagrees across windows: %+v vs %+v", a, b)
	}
}
