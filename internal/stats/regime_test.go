package stats

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/regime"
)

func briefing(id int64, dayStr string, trend regime.Trend, vol regime.Vol, hour int) journal.Briefing {
	raw, err := json.Marshal(brief.Input{Regime: regime.Regime{Trend: trend, Vol: vol}})
	if err != nil {
		panic(err)
	}
	return journal.Briefing{
		ID: id, Mode: journal.ModePaper, Day: dayStr,
		GeneratedAt: at(dayStr, hour), InputJSON: raw,
	}
}

func regimeRow(t *testing.T, rep Report, label string) RegimeStats {
	t.Helper()
	for _, r := range rep.ByRegime {
		if r.Label == label {
			return r
		}
	}
	var have []string
	for _, r := range rep.ByRegime {
		have = append(have, r.Label)
	}
	t.Fatalf("no regime row %q; have %v", label, have)
	return RegimeStats{}
}

func TestByRegimeGroupsTheRecordByTheArchivedMarket(t *testing.T) {
	yes := true
	scored := at("2026-08-10", 16)
	actual := 1.1
	pid := int64(1)
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		briefings: []journal.Briefing{
			briefing(1, "2026-08-03", regime.TrendUp, regime.VolLow, 6),
			briefing(2, "2026-08-04", regime.TrendUp, regime.VolLow, 6),
			briefing(3, "2026-08-05", regime.TrendDown, regime.VolHigh, 6),
		},
		calls: []journal.Call{
			{ID: 1, Day: "2026-08-03", ThresholdPct: 0.4, ScoredAt: &scored, Correct: &yes, ActualPct: &actual},
			{ID: 2, Day: "2026-08-05", ThresholdPct: 0.4},
			// Nobody briefed this day, so it cannot claim a regime.
			{ID: 3, Day: "2026-08-06", ThresholdPct: 0.4},
		},
		notes: []journal.NoteScore{
			{ID: 1, Day: "2026-08-03", Symbol: "NVDA", ThresholdPct: 0.5, ActualPct: 1.2, Correct: true},
			{ID: 2, Day: "2026-08-05", Symbol: "AMD", ThresholdPct: 0.5, ActualPct: -0.1, Correct: false},
		},
		proposals: []journal.Proposal{
			{ID: 1, Day: "2026-08-03", SetupID: "B1", Status: journal.ProposalTaken},
		},
		orders: map[int64]journal.Order{40: {ID: 40, ProposalID: &pid}},
		fills: []journal.Fill{
			{ID: 1, OrderID: 40, FilledAt: at("2026-08-04", 8)},
			{ID: 2, OrderID: 77, FilledAt: at("2026-08-05", 8)},
		},
		trades: []journal.Trade{
			// Opened on the 4th but proposed on the 3rd: the idea's day decides.
			{ID: 1, OpenedAt: at("2026-08-04", 8), ClosedAt: at("2026-08-04", 13), NetPL: 60, EntryOrderID: 40},
			// A hand-placed entry has only its own fill day to go on.
			{ID: 2, OpenedAt: at("2026-08-05", 8), ClosedAt: at("2026-08-05", 13), NetPL: -20, EntryOrderID: 77},
		},
		outcomes: []journal.ProposalOutcome{
			{ProposalID: 1, Day: "2026-08-03", Filled: true, NetPL: 70, RMultiple: 1.4},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if len(rep.ByRegime) != 3 {
		t.Fatalf("want two regimes and the unbriefed day, got %+v", rep.ByRegime)
	}
	for i := 1; i < len(rep.ByRegime); i++ {
		if rep.ByRegime[i-1].Label >= rep.ByRegime[i].Label {
			t.Fatalf("rows must sort by label, got %v", rep.ByRegime)
		}
	}

	up := regimeRow(t, rep, "uptrend, low vol")
	if up.Sessions != 2 {
		t.Fatalf("uptrend sessions = %d, want the 3rd and 4th", up.Sessions)
	}
	if up.Calls.Total != 1 || up.Calls.Correct != 1 || up.Notes.Total != 1 {
		t.Fatalf("uptrend grades = calls %+v notes %+v", up.Calls, up.Notes)
	}
	if up.Trades.Count != 1 || !near(up.Trades.NetPL, 60) {
		t.Fatalf("the proposal's day puts its trade in the uptrend, got %+v", up.Trades)
	}
	if up.Counterfactual.Replayed != 1 || !near(up.Counterfactual.NetPL, 70) {
		t.Fatalf("uptrend counterfactual = %+v", up.Counterfactual)
	}

	down := regimeRow(t, rep, "downtrend, high vol")
	if down.Sessions != 1 || down.Calls.Pending != 1 || down.Trades.Count != 1 {
		t.Fatalf("downtrend row = %+v", down)
	}
	if !near(down.Trades.NetPL, -20) {
		t.Fatalf("the human trade belongs to its fill day, got %+v", down.Trades)
	}

	none := regimeRow(t, rep, noBriefingLabel)
	if none.Sessions != 1 || none.Calls.Pending != 1 {
		t.Fatalf("the unbriefed day = %+v", none)
	}
}

// A hand-placed trade belongs to the venue's session, not the trader's calendar
// day. 23:00 Mountain is already the next session in New York, and the regime
// labels it is bucketed against are Eastern.
func TestAHumanTradeBelongsToItsEasternSession(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		briefings: []journal.Briefing{
			briefing(1, "2026-08-04", regime.TrendUp, regime.VolLow, 6),
			briefing(2, "2026-08-05", regime.TrendDown, regime.VolHigh, 6),
		},
		calls: []journal.Call{
			{ID: 1, Day: "2026-08-04", ThresholdPct: 0.4},
			{ID: 2, Day: "2026-08-05", ThresholdPct: 0.4},
		},
		trades: []journal.Trade{
			// 23:00 Mountain on the 4th is 01:00 Eastern on the 5th.
			{ID: 1, OpenedAt: at("2026-08-04", 23), ClosedAt: at("2026-08-05", 13), NetPL: -20, EntryOrderID: 77},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	down := regimeRow(t, rep, "downtrend, high vol")
	if down.Trades.Count != 1 || !near(down.Trades.NetPL, -20) {
		t.Fatalf("the trade belongs to the 2026-08-05 session, got %+v", down.Trades)
	}
	if up := regimeRow(t, rep, "uptrend, low vol"); up.Trades.Count != 0 {
		t.Fatalf("the trader's calendar day claimed the trade: %+v", up.Trades)
	}
}

func TestByRegimeUsesTheLastBriefingOfADay(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		briefings: []journal.Briefing{
			briefing(1, "2026-08-03", regime.TrendSideways, regime.VolNormal, 6),
			// A forced re-run before the open replaces the earlier read.
			briefing(2, "2026-08-03", regime.TrendUp, regime.VolHigh, 7),
		},
		calls: []journal.Call{{ID: 1, Day: "2026-08-03", ThresholdPct: 0.4}},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if len(rep.ByRegime) != 1 || rep.ByRegime[0].Label != "uptrend, high vol" {
		t.Fatalf("want only the later briefing's label, got %+v", rep.ByRegime)
	}
}

func TestAnUnreadableBriefingArchiveFailsTheReport(t *testing.T) {
	src := &fakeSource{
		ledger:    journal.Ledger{StartingEquity: 5000},
		briefings: []journal.Briefing{{ID: 9, Day: "2026-08-03", InputJSON: []byte("{not json")}},
	}
	_, err := Compute(context.Background(), src, testWindow("2026-08-01", "2026-08-30"), testGate())
	if err == nil {
		t.Fatal("a corrupt archive must be reported, not silently regrouped")
	}
	if !strings.Contains(err.Error(), "2026-08-03") {
		t.Fatalf("error should name the day: %v", err)
	}
}
