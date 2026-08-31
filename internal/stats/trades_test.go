package stats

import (
	"math"
	"testing"

	"github.com/trapp01/tape/internal/journal"
)

// sixTrades nets +180 on 3 wins of 100/200/30 and 3 losses of 50/80/20.
func sixTrades() []journal.Trade {
	nets := []struct {
		day string
		net float64
	}{
		{"2026-08-03", 100}, {"2026-08-04", -50}, {"2026-08-05", 200},
		{"2026-08-06", -80}, {"2026-08-07", 30}, {"2026-08-10", -20},
	}
	out := make([]journal.Trade, 0, len(nets))
	for i, n := range nets {
		out = append(out, journal.Trade{
			ID: int64(i + 1), Symbol: "SPY", Qty: 10,
			OpenedAt: at(n.day, 8), ClosedAt: at(n.day, 13),
			GrossPL: n.net + 2, Costs: 2, NetPL: n.net,
			EntryOrderID: int64(10 + i), ExitOrderID: int64(50 + i),
		})
	}
	return out
}

func TestSixTradesMatchTheHandComputedStats(t *testing.T) {
	src := &fakeSource{trades: sixTrades(), ledger: journal.Ledger{StartingEquity: 5000}}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	s := rep.Trades
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"count", float64(s.Count), 6},
		{"wins", float64(s.Wins), 3},
		{"losses", float64(s.Losses), 3},
		{"win rate", s.WinRate, 0.5},
		{"avg win", s.AvgWinUSD, 110},
		{"avg loss", s.AvgLossUSD, -50},
		{"expectancy", s.ExpectancyUSD, 30},
		{"profit factor", s.ProfitFactor, 330.0 / 150.0},
		{"gross", s.GrossPL, 192},
		{"costs", s.Costs, 12},
		{"net", s.NetPL, 180},
	} {
		if !near(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	// The curve runs 5100, 5050, 5250, 5170, 5200, 5180; the worst fall is 80
	// off the 5250 peak.
	e := rep.Equity
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"start", e.StartingEquity, 5000},
		{"end", e.EndingEquity, 5180},
		{"return", e.ReturnPct, 3.6},
		{"max dd usd", e.MaxDrawdownUSD, 80},
		{"max dd pct", e.MaxDrawdownPct, 80.0 / 5250.0 * 100},
	} {
		if !near(c.got, c.want) {
			t.Errorf("equity %s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestProfitFactorWithNoLosingTrade(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		trades: []journal.Trade{
			{ID: 1, ClosedAt: at("2026-08-03", 13), NetPL: 50, GrossPL: 51, Costs: 1},
			{ID: 2, ClosedAt: at("2026-08-04", 13), NetPL: 75, GrossPL: 76, Costs: 1},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	s := rep.Trades
	if s.Wins != 2 || s.Losses != 0 || !near(s.WinRate, 1) || !near(s.AvgLossUSD, 0) {
		t.Fatalf("unexpected win/loss split: %+v", s)
	}
	// No divisor exists, so the field carries gross profit instead of a ratio.
	if !near(s.ProfitFactor, 125) {
		t.Fatalf("profit factor = %v, want the 125 gross profit", s.ProfitFactor)
	}
	if math.IsInf(s.ProfitFactor, 0) || math.IsNaN(s.ProfitFactor) {
		t.Fatal("profit factor must stay finite")
	}
	if c := checkByName(t, rep, "profit factor"); !c.Passed || c.Actual != "no losses" {
		t.Fatalf("a record that never lost should clear the ratio: %+v", c)
	}
	if !near(rep.Equity.MaxDrawdownPct, 0) {
		t.Fatalf("a record that never fell has no drawdown, got %v", rep.Equity.MaxDrawdownPct)
	}
}

func TestSetupAttributionIncludesHumanTrades(t *testing.T) {
	pid1, pid2 := int64(1), int64(2)
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		trades: []journal.Trade{
			{ID: 1, ClosedAt: at("2026-08-03", 13), NetPL: 100, EntryOrderID: 10},
			{ID: 2, ClosedAt: at("2026-08-04", 13), NetPL: -40, EntryOrderID: 11},
			{ID: 3, ClosedAt: at("2026-08-05", 13), NetPL: 20, EntryOrderID: 12},
			// An entry order the journal no longer has cannot be attributed.
			{ID: 4, ClosedAt: at("2026-08-06", 13), NetPL: -10, EntryOrderID: 99},
		},
		orders: map[int64]journal.Order{
			10: {ID: 10, ProposalID: &pid1, Source: journal.SourceProposal},
			11: {ID: 11, ProposalID: &pid2, Source: journal.SourceProposal},
			12: {ID: 12, Source: journal.SourceHuman},
		},
		proposals: []journal.Proposal{
			{ID: 1, Day: "2026-08-03", SetupID: "B1", Status: journal.ProposalTaken},
			{ID: 2, Day: "2026-08-04", SetupID: "B1", Status: journal.ProposalTaken},
			{ID: 3, Day: "2026-08-05", SetupID: "R2", Status: journal.ProposalPassed},
		},
		outcomes: []journal.ProposalOutcome{
			{ProposalID: 1, Day: "2026-08-03", Filled: true, NetPL: 90, RMultiple: 1.5},
			{ProposalID: 3, Day: "2026-08-05", Filled: true, NetPL: -30, RMultiple: -1, Ambiguous: true},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if len(rep.BySetup) != 3 {
		t.Fatalf("want B1, R2 and human rows, got %+v", rep.BySetup)
	}
	if rep.BySetup[0].SetupID != "B1" || rep.BySetup[1].SetupID != "R2" || rep.BySetup[2].SetupID != SetupHuman {
		t.Fatalf("rows must sort by setup id, got %v %v %v",
			rep.BySetup[0].SetupID, rep.BySetup[1].SetupID, rep.BySetup[2].SetupID)
	}
	if got := rep.BySetup[0].Trades; got.Count != 2 || !near(got.NetPL, 60) {
		t.Fatalf("B1 should hold both proposal trades, got %+v", got)
	}
	// R2 was never taken, so it exists only as a replay.
	if got := rep.BySetup[1]; got.Trades.Count != 0 || got.Counterfactual.Replayed != 1 {
		t.Fatalf("R2 should be replay-only, got %+v", got)
	}
	if got := rep.BySetup[2].Trades; got.Count != 2 || !near(got.NetPL, 10) {
		t.Fatalf("human trades: the untraceable entry belongs here too, got %+v", got)
	}
	if got := rep.Proposals.Counterfactual; got.Replayed != 2 || got.Filled != 2 ||
		got.Wins != 1 || got.Losses != 1 || !near(got.NetPL, 60) || !near(got.AvgRMultiple, 0.25) {
		t.Fatalf("overall counterfactual = %+v", got)
	}
	if rep.Proposals.Counterfactual.Ambiguous != 1 {
		t.Fatalf("the stop-first replay must be counted, got %+v", rep.Proposals.Counterfactual)
	}
}
