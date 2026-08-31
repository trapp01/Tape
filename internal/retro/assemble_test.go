package retro

import (
	"context"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// seed fills the fake journal with one week the review can read: a graded call,
// two graded notes, a taken idea and a passed one with their replays, two closed
// trades, and a pair of guardrail refusals.
func seed(t *testing.T, st *fakeStore) {
	t.Helper()
	loc := mountain(t)
	at := func(day string, hour int) time.Time {
		d, err := time.ParseInLocation(dayLayout, day, loc)
		if err != nil {
			t.Fatalf("parsing %s: %v", day, err)
		}
		return d.Add(time.Duration(hour) * time.Hour)
	}
	scoredAt := at("2026-08-26", 17)
	correct, wrong := true, false
	open, close, actual := 510.0, 512.55, 0.5

	st.briefings = []journal.Briefing{{
		ID: 1, Mode: journal.ModePaper, Day: "2026-08-26", GeneratedAt: at("2026-08-26", 7),
		InputJSON: []byte(`{"regime":{"trend":"uptrend","vol":"low"}}`), OutputJSON: []byte(`{}`),
	}}
	st.calls = []journal.Call{
		{ID: 1, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Instrument: "SPY",
			Direction: "up", ThresholdPct: 0.3, ScoredAt: &scoredAt,
			Open: &open, Close: &close, ActualPct: &actual, Correct: &correct},
		{ID: 2, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-27", Instrument: "SPY",
			Direction: "down", ThresholdPct: 0.3},
	}
	st.notes = []journal.NoteScore{
		{ID: 1, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Symbol: "NVDA",
			Bias: "bullish", ThresholdPct: 0.3, ScoredAt: scoredAt, Open: 120, Close: 121.2, ActualPct: 1.0, Correct: correct},
		{ID: 2, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Symbol: "AAPL",
			Bias: "bearish", ThresholdPct: 0.3, ScoredAt: scoredAt, Open: 226, Close: 227.1, ActualPct: 0.49, Correct: wrong},
	}

	orderID := int64(10)
	st.proposals = []journal.Proposal{
		{ID: 1, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Index: 1, Symbol: "NVDA",
			Side: "long", SetupID: "M1", Entry: 120, Stop: 118, Target: 124, Qty: 12,
			Status: journal.ProposalTaken, OrderID: &orderID},
		{ID: 2, BriefingID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Index: 2, Symbol: "AAPL",
			Side: "long", SetupID: "M1", Entry: 226, Stop: 224, Target: 231, Qty: 11,
			Status: journal.ProposalPassed, Reason: "already extended into the open"},
	}
	st.outcomes = []journal.ProposalOutcome{
		{ID: 1, ProposalID: 1, Mode: journal.ModePaper, Day: "2026-08-26", Filled: true,
			ExitKind: journal.ExitTarget, Qty: 12, GrossPL: 48, Costs: 3, NetPL: 45, RMultiple: 2, ScoredAt: scoredAt},
		{ID: 2, ProposalID: 2, Mode: journal.ModePaper, Day: "2026-08-26", Filled: true,
			ExitKind: journal.ExitStop, Qty: 11, GrossPL: -22, Costs: 3, NetPL: -25, RMultiple: -1, ScoredAt: scoredAt},
	}

	proposalID := int64(1)
	st.orders = map[int64]journal.Order{
		10: {ID: 10, Symbol: "NVDA", Side: "buy", Mode: journal.ModePaper, ProposalID: &proposalID},
	}
	st.trades = []journal.Trade{
		{ID: 1, Symbol: "NVDA", Qty: 12, EntryAvgPrice: 120, ExitAvgPrice: 123.5,
			OpenedAt: at("2026-08-26", 8), ClosedAt: at("2026-08-26", 13),
			GrossPL: 42, Costs: 3, NetPL: 39, EntryOrderID: 10},
		{ID: 2, Symbol: "MSFT", Qty: 5, EntryAvgPrice: 418, ExitAvgPrice: 414,
			OpenedAt: at("2026-08-27", 8), ClosedAt: at("2026-08-27", 12),
			GrossPL: -20, Costs: 2, NetPL: -22},
	}
	st.fills = []journal.Fill{
		{ID: 1, OrderID: 10, Symbol: "NVDA", Side: "buy", Qty: 12, RawPrice: 120, ModeledPrice: 120.06, FilledAt: at("2026-08-26", 8)},
	}
	st.refusals = []journal.Refusal{
		{ID: 1, Mode: journal.ModePaper, Day: "2026-08-26", At: at("2026-08-26", 9), Rule: "max positions", Symbol: "META"},
		{ID: 2, Mode: journal.ModePaper, Day: "2026-08-27", At: at("2026-08-27", 9), Rule: "max positions", Symbol: "AMZN"},
		{ID: 3, Mode: journal.ModePaper, Day: "2026-08-27", At: at("2026-08-27", 10), Rule: "no overspend", Symbol: "GOOGL"},
	}
}

// Two runs over one journal produce the same Input, because a prompt that drifts
// makes two reviews impossible to compare.
func TestAssembleIsDeterministic(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	d := testDeps(t, st, "")

	first, err := Assemble(context.Background(), d)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	second, err := Assemble(context.Background(), d)
	if err != nil {
		t.Fatalf("second Assemble: %v", err)
	}
	_, a := BuildPrompt(first)
	_, b := BuildPrompt(second)
	if a != b {
		t.Fatalf("two assemblies rendered differently:\n--- first ---\n%s\n--- second ---\n%s", a, b)
	}
}

func TestAssembleReadsTheWeek(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	in, err := Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if in.FromDay != "2026-08-22" || in.ToDay != "2026-08-28" {
		t.Fatalf("window = %s .. %s, want a week ending on the review day", in.FromDay, in.ToDay)
	}
	if in.Report.Trades.Count != 2 || in.Report.Calls.Total != 1 || in.Report.Notes.Total != 2 {
		t.Fatalf("report = %+v", in.Report)
	}
	if in.Report.Calls.Pending != 1 {
		t.Fatalf("the ungraded call must read as pending, not wrong: %+v", in.Report.Calls)
	}
}

// Only losers reach the worst list and only winners the best one, so a short
// week never reads as more trades than it holds.
func TestAssembleNamesTheExtremes(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	in, err := Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(in.Worst) != 1 || in.Worst[0].Symbol != "MSFT" || in.Worst[0].NetUSD >= 0 {
		t.Fatalf("worst = %+v", in.Worst)
	}
	if len(in.Best) != 1 || in.Best[0].Symbol != "NVDA" {
		t.Fatalf("best = %+v", in.Best)
	}
	// The NVDA trade opened on a proposal, so it carries that rule and its R.
	nvda := in.Best[0]
	if nvda.SetupID != "M1" || nvda.Day != "2026-08-26" {
		t.Fatalf("the traced trade = %+v", nvda)
	}
	if delta := nvda.RMultiple - 1.75; delta > 0.001 || delta < -0.001 {
		t.Fatalf("R = %v, want (123.50 - 120) / (120 - 118)", nvda.RMultiple)
	}
	if in.Worst[0].SetupID != "human" {
		t.Fatalf("a trade with no proposal behind it is not a playbook rule: %+v", in.Worst[0])
	}
}

// A pass carries the replay that says what the veto cost, which is the only way
// a veto is measurable at all.
func TestAssemblePairsPassesWithTheirReplays(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	in, err := Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(in.Passes) != 1 {
		t.Fatalf("passes = %+v", in.Passes)
	}
	p := in.Passes[0]
	if p.Symbol != "AAPL" || !p.Replayed || p.NetUSD != -25 || p.ExitKind != journal.ExitStop {
		t.Fatalf("pass = %+v", p)
	}
	if p.Reason != "already extended into the open" {
		t.Fatalf("the trader's reason is part of the record: %+v", p)
	}
}

func TestAssembleCountsRefusalsByRule(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	in, err := Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	want := []RefusalLine{{Rule: "max positions", Count: 2}, {Rule: "no overspend", Count: 1}}
	if len(in.Refusals) != len(want) {
		t.Fatalf("refusals = %+v", in.Refusals)
	}
	for i, w := range want {
		if in.Refusals[i] != w {
			t.Fatalf("refusal %d = %+v, want %+v", i, in.Refusals[i], w)
		}
	}
}

// The previous review's summary is shown so the model can see what it already
// said. An archive with no usable reply contributes nothing rather than failing.
func TestAssembleCarriesThePreviousSummary(t *testing.T) {
	st := newFakeStore()
	seed(t, st)
	st.retros = []journal.Retro{{
		ID: 1, Mode: journal.ModePaper, FromDay: "2026-08-15", ToDay: "2026-08-21",
		OutputJSON: []byte(`{"summary":"Eleven trades is not a sample."}`),
	}}

	in, err := Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if in.PreviousSummary != "Eleven trades is not a sample." {
		t.Fatalf("previous summary = %q", in.PreviousSummary)
	}

	st.retros[0].OutputJSON = []byte("the model returned prose")
	in, err = Assemble(context.Background(), testDeps(t, st, ""))
	if err != nil {
		t.Fatalf("Assemble with an unreadable archive: %v", err)
	}
	if in.PreviousSummary != "" {
		t.Fatalf("an unreadable archive contributed %q", in.PreviousSummary)
	}
}
