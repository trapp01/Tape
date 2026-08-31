package brief

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// filePassedProposal archives a briefing, files one idea, and records the pass,
// which is the state the counterfactual exists to grade.
func filePassedProposal(t *testing.T, st *journal.Store, day, symbol string, entry, stop, target float64) journal.Proposal {
	t.Helper()
	ctx := context.Background()
	b := journal.Briefing{
		Mode: journal.ModePaper, Day: day, Provider: "fake", Model: "fake-model-1",
		InputJSON: []byte(`{}`), OutputJSON: []byte(`{}`),
	}
	if err := st.InsertBriefing(ctx, &b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	p := &journal.Proposal{
		BriefingID: b.ID, Mode: journal.ModePaper, Day: day, Index: 1,
		Symbol: symbol, Side: "long", SetupID: "M2",
		Entry: entry, Stop: stop, Target: target, Qty: 10, RiskUSD: 20,
		Thesis: "M2 continuation", Invalidation: "loses the prior high", Confidence: "medium",
	}
	if err := st.InsertProposals(ctx, []*journal.Proposal{p}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	if err := st.DecideProposal(ctx, p.ID, journal.ProposalPassed, "too extended", nil, time.Time{}); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}
	return *p
}

// minuteBars is a rising session: the entry fills on the first bar and the target
// prints later, so a replay has a whole round trip to price.
func minuteBars(from float64) []market.Bar {
	start := time.Date(2026, 8, 28, 9, 30, 0, 0, market.Eastern())
	bars := make([]market.Bar, 0, 6)
	price := from
	for i := range 6 {
		bars = append(bars, market.Bar{
			Time: start.Add(time.Duration(i) * time.Minute),
			Open: price, High: price + 0.6, Low: price - 0.2, Close: price + 0.4,
		})
		price += 0.5
	}
	return bars
}

// replayDeps is a scoring pass with the intraday feed wired, so every kind runs.
func replayDeps(st *journal.Store, feed *fakeFeed) ScoreDeps {
	return ScoreDeps{
		Journal: st, Sessions: feed, Intraday: feed, Costs: costs.Default(),
		Mode: journal.ModePaper, DefaultThresholdPct: 0.3,
	}
}

// A pass is graded against what the session did, which is the whole point of
// journalling the ideas nobody took.
func TestScoreDueReplaysAPassedProposal(t *testing.T) {
	st := testJournal(t)
	p := filePassedProposal(t, st, "2026-08-28", "NVDA", 120, 118, 123)
	feed := sessionFeed(map[string][2]float64{})
	feed.sessions[sessionKey("NVDA", "2026-08-28")] = market.Session{
		Symbol: "NVDA", Day: "2026-08-28", Open: 120, High: 124, Low: 119.5, Close: 123.4, Complete: true,
	}
	feed.minutes = map[string][]market.Bar{sessionKey("NVDA", "2026-08-28"): minuteBars(120)}

	report, err := ScoreDue(context.Background(), replayDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Outcomes) != 1 || len(report.OutcomesSkipped) != 0 {
		t.Fatalf("report = %+v", report)
	}
	got := report.Outcomes[0]
	if got.Proposal.ID != p.ID || !got.Outcome.Filled || got.Outcome.ExitKind != journal.ExitTarget {
		t.Fatalf("outcome = %+v", got.Outcome)
	}
	if got.Outcome.NetPL <= 0 {
		t.Fatalf("a target hit must net positive, got %v", got.Outcome.NetPL)
	}

	stored, err := st.OutcomeByProposal(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("OutcomeByProposal: %v", err)
	}
	if stored.ScoredAt.IsZero() {
		t.Fatalf("the replay was filed without a stamp: %+v", stored)
	}

	// An idea is replayed once, so a second pass has nothing left to do.
	second, err := ScoreDue(context.Background(), replayDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second.Outcomes) != 0 || len(second.OutcomesSkipped) != 0 {
		t.Fatalf("second pass = %+v", second)
	}
}

// A split between the session and the replay leaves the feed's adjusted prices
// nowhere near the levels the proposal was written at. Replaying that would file
// a fictitious round trip, and an idea is replayed once.
func TestScoreDueSkipsAReplayAgainstAdjustedPrices(t *testing.T) {
	st := testJournal(t)
	p := filePassedProposal(t, st, "2026-08-28", "NVDA", 120, 118, 123)
	feed := sessionFeed(map[string][2]float64{})
	feed.sessions[sessionKey("NVDA", "2026-08-28")] = market.Session{
		Symbol: "NVDA", Day: "2026-08-28", Open: 12, High: 12.4, Low: 11.95, Close: 12.34, Complete: true,
	}
	// A ten-for-one split: the same session, priced a tenth of what was proposed.
	feed.minutes = map[string][]market.Bar{sessionKey("NVDA", "2026-08-28"): minuteBars(12)}

	report, err := ScoreDue(context.Background(), replayDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Outcomes) != 0 || len(report.OutcomesSkipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	for _, want := range []string{"NVDA", "adjusted"} {
		if !strings.Contains(report.OutcomesSkipped[0], want) {
			t.Fatalf("skip reason %q does not mention %q", report.OutcomesSkipped[0], want)
		}
	}
	if _, err := st.OutcomeByProposal(context.Background(), p.ID); err == nil {
		t.Fatal("a replay against adjusted prices was filed")
	}
}

// A session with no prints leaves the idea unreplayed and says so.
func TestScoreDueSkipsAReplayWithNoBars(t *testing.T) {
	st := testJournal(t)
	filePassedProposal(t, st, "2026-08-28", "NVDA", 120, 118, 123)
	feed := sessionFeed(map[string][2]float64{})
	feed.sessions[sessionKey("NVDA", "2026-08-28")] = market.Session{
		Symbol: "NVDA", Day: "2026-08-28", Open: 120, Close: 123.4, Complete: true,
	}

	report, err := ScoreDue(context.Background(), replayDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Outcomes) != 0 || len(report.OutcomesSkipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.OutcomesSkipped[0], "no session bars") {
		t.Fatalf("skip reason = %q", report.OutcomesSkipped[0])
	}

	due, err := st.UnscoredProposals(context.Background(), journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredProposals: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("the idea must stay open for the next run, %d due", len(due))
	}
}

// A session still running replays nothing: the path the idea is graded against
// has not finished printing.
func TestScoreDueSkipsAReplayOnAnUnfinishedSession(t *testing.T) {
	st := testJournal(t)
	filePassedProposal(t, st, "2026-08-28", "NVDA", 120, 118, 123)
	feed := sessionFeed(map[string][2]float64{})
	feed.sessions[sessionKey("NVDA", "2026-08-28")] = market.Session{
		Symbol: "NVDA", Day: "2026-08-28", Open: 120, Close: 121, Complete: false,
	}
	feed.minutes = map[string][]market.Bar{sessionKey("NVDA", "2026-08-28"): minuteBars(120)}

	report, err := ScoreDue(context.Background(), replayDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Outcomes) != 0 || !strings.Contains(report.OutcomesSkipped[0], "session not final yet") {
		t.Fatalf("report = %+v", report)
	}
}

// Without an intraday source the pass reports the backlog rather than pretending
// there is nothing to replay.
func TestScoreDueReportsReplaysItCannotRun(t *testing.T) {
	st := testJournal(t)
	filePassedProposal(t, st, "2026-08-28", "NVDA", 120, 118, 123)
	feed := sessionFeed(map[string][2]float64{})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.OutcomesSkipped) != 1 || !strings.Contains(report.OutcomesSkipped[0], "no intraday data source") {
		t.Fatalf("report = %+v", report)
	}
}
