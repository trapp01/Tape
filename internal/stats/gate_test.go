package stats

import (
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// qualifyingRecord is a synthetic three-month paper run that clears every
// threshold: 55 sessions, 240 trades at profit factor 1.5, worst drawdown 3.8%,
// and no guardrail breach in the final month.
//
// The trades repeat a six-trade block of +150, +150, -100, -100, +150, -100, so
// wins total 1.5x losses and the deepest fall is $200 off a $5,300 peak.
func qualifyingRecord() *fakeSource {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		orders: map[int64]journal.Order{},
		refusals: []journal.Refusal{
			// Well outside the final thirty days, so the gate's last month is clean.
			{ID: 1, Day: "2026-07-15", Rule: "risk cap", At: at("2026-07-15", 9)},
		},
	}
	sessions := make([]string, 0, 55)
	for i := range 55 {
		sessions = append(sessions, day("2026-05-22").AddDate(0, 0, i).Format(dayLayout))
	}
	block := []float64{150, 150, -100, -100, 150, -100}
	closed := day("2026-06-01")
	for i := range 240 {
		id := int64(i + 1)
		pid := id
		d := sessions[i%len(sessions)]
		src.proposals = append(src.proposals, journal.Proposal{
			ID: pid, Day: d, Symbol: "SPY", SetupID: "B1", Status: journal.ProposalTaken,
		})
		src.orders[id] = journal.Order{ID: id, ProposalID: &pid, Source: journal.SourceProposal}
		net := block[i%len(block)]
		src.trades = append(src.trades, journal.Trade{
			ID: id, Symbol: "SPY", Qty: 10,
			OpenedAt: closed, ClosedAt: closed, NetPL: net, GrossPL: net + 2, Costs: 2,
			EntryOrderID: id, ExitOrderID: id + 1000,
		})
		closed = closed.Add(time.Hour)
	}
	return src
}

func TestEveryGateCheckPassesOnAQualifyingRecord(t *testing.T) {
	rep := run(t, qualifyingRecord(), testWindow("2026-05-22", "2026-08-30"), testGate())

	if rep.Sessions != 55 {
		t.Fatalf("sessions = %d, want 55", rep.Sessions)
	}
	if rep.Trades.Count != 240 || !near(rep.Trades.ProfitFactor, 1.5) {
		t.Fatalf("trades = %+v, want 240 at profit factor 1.5", rep.Trades)
	}
	if !near(rep.Trades.ExpectancyUSD, 25) {
		t.Fatalf("expectancy = %v, want 25", rep.Trades.ExpectancyUSD)
	}
	if got := rep.Equity.MaxDrawdownPct; got < 3.7 || got > 3.8 {
		t.Fatalf("max drawdown = %v%%, want the 200/5300 fall", got)
	}
	if !near(rep.Significance.NullWinRate, 0.4) {
		t.Fatalf("null win rate = %v, want 100/(150+100)", rep.Significance.NullWinRate)
	}
	if rep.Significance.ExpectancyCI95Low <= 0 {
		t.Fatalf("expectancy lower bound = %v, want it above zero", rep.Significance.ExpectancyCI95Low)
	}

	for _, c := range rep.GateChecks {
		if !c.Passed {
			t.Errorf("check %q failed: actual %q, needed %q", c.Name, c.Actual, c.Needed)
		}
		if strings.Contains(c.Actual, "since") {
			t.Errorf("check %q says %q with no playbook change in the window", c.Name, c.Actual)
		}
	}
	if !rep.GateOpen {
		t.Fatalf("gate should be open; checks: %+v", rep.GateChecks)
	}
	if rep.GateResetAt != nil {
		t.Fatalf("no playbook version exists, so nothing reset the window: %v", rep.GateResetAt)
	}
	if !rep.GateWindowFrom.Equal(day("2026-05-22")) {
		t.Fatalf("gate window starts at %v, want the report window", rep.GateWindowFrom)
	}
}

func TestGateChecksCoverEveryThreshold(t *testing.T) {
	rep := run(t, qualifyingRecord(), testWindow("2026-05-22", "2026-08-30"), testGate())
	want := []string{
		"months covered", "sessions", "trades", "expectancy",
		"expectancy lower bound", "profit factor", "max drawdown",
		"null pass rate", "refusals last month", "setups identified",
	}
	got := checkNames(rep)
	if len(got) != len(want) {
		t.Fatalf("gate checks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("check %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAPlaybookChangeResetsTheGateWindow(t *testing.T) {
	reset := at("2026-08-01", 18)
	src := &fakeSource{
		ledger:   journal.Ledger{StartingEquity: 5000},
		playbook: &journal.PlaybookVersion{ID: 3, CreatedAt: reset, SHA256: "abc"},
		calls: []journal.Call{
			{ID: 1, Day: "2026-06-10"}, {ID: 2, Day: "2026-08-05"}, {ID: 3, Day: "2026-08-06"},
		},
		trades: []journal.Trade{
			{ID: 1, ClosedAt: at("2026-06-10", 13), NetPL: 100},
			{ID: 2, ClosedAt: at("2026-06-11", 13), NetPL: 100},
			{ID: 3, ClosedAt: at("2026-07-01", 13), NetPL: 100},
			{ID: 4, ClosedAt: at("2026-07-02", 13), NetPL: 100},
			{ID: 5, ClosedAt: at("2026-08-05", 13), NetPL: 10},
			{ID: 6, ClosedAt: at("2026-08-06", 13), NetPL: -5},
		},
	}
	rep := run(t, src, testWindow("2026-05-22", "2026-08-30"), testGate())

	// The descriptive half still covers the whole window.
	if rep.Trades.Count != 6 || !near(rep.Trades.NetPL, 405) {
		t.Fatalf("descriptive trades = %+v, want all six", rep.Trades)
	}
	if rep.Sessions != 3 {
		t.Fatalf("sessions = %d, want all three call days", rep.Sessions)
	}
	if rep.GateResetAt == nil || !rep.GateResetAt.Equal(reset) {
		t.Fatalf("gate reset = %v, want %v", rep.GateResetAt, reset)
	}
	if !rep.GateWindowFrom.Equal(reset) {
		t.Fatalf("gate window from = %v, want %v", rep.GateWindowFrom, reset)
	}

	// The gate half reads only the two sessions since the rules changed.
	if c := checkByName(t, rep, "trades"); c.Actual != "2 since 2026-08-01" {
		t.Fatalf("trades check = %+v, want the post-reset count", c)
	}
	if c := checkByName(t, rep, "sessions"); c.Actual != "2 since 2026-08-01" {
		t.Fatalf("sessions check = %+v", c)
	}
	if c := checkByName(t, rep, "months covered"); c.Passed {
		t.Fatalf("a one-month record cannot clear three months: %+v", c)
	}
	for _, c := range rep.GateChecks {
		if !strings.HasSuffix(c.Actual, "since 2026-08-01") {
			t.Errorf("check %q should name the reset it was measured from, got %q", c.Name, c.Actual)
		}
	}
}

func TestAPlaybookChangeBeforeTheWindowLeavesItAlone(t *testing.T) {
	src := qualifyingRecord()
	before := at("2026-01-04", 9)
	src.playbook = &journal.PlaybookVersion{ID: 1, CreatedAt: before}
	rep := run(t, src, testWindow("2026-05-22", "2026-08-30"), testGate())

	if rep.GateResetAt != nil {
		t.Fatalf("a change older than the window resets nothing, got %v", rep.GateResetAt)
	}
	if !rep.GateOpen {
		t.Fatalf("gate should still be open; checks: %+v", rep.GateChecks)
	}
}

func TestSignificanceNeedsBothSidesOfTheRecord(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		trades: []journal.Trade{
			{ID: 1, ClosedAt: at("2026-08-03", 13), NetPL: 100},
			{ID: 2, ClosedAt: at("2026-08-04", 13), NetPL: 120},
			{ID: 3, ClosedAt: at("2026-08-05", 13), NetPL: -60},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if rep.Significance != (Significance{}) {
		t.Fatalf("one loss is not a null to test against, got %+v", rep.Significance)
	}
	for _, name := range []string{"null pass rate", "expectancy lower bound"} {
		c := checkByName(t, rep, name)
		if c.Passed || c.Actual != insufficient {
			t.Fatalf("check %q = %+v, want a failed %q", name, c, insufficient)
		}
	}
}

func TestASmallEdgeDoesNotClearTheNullPassRate(t *testing.T) {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}}
	block := []float64{150, 150, -100, -100, 150, -100}
	closed := at("2026-08-03", 10)
	for i := range 24 {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(i + 1), ClosedAt: closed, NetPL: block[i%len(block)],
		})
		closed = closed.Add(time.Hour)
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if !near(rep.Trades.ProfitFactor, 1.5) {
		t.Fatalf("profit factor = %v, want 1.5", rep.Trades.ProfitFactor)
	}
	// Two dozen trades at this shape are well inside what a coin flip produces.
	if rep.Significance.NullPassRate <= 0.05 {
		t.Fatalf("null pass rate = %v, expected a small sample to be easy to fake", rep.Significance.NullPassRate)
	}
	if c := checkByName(t, rep, "null pass rate"); c.Passed {
		t.Fatalf("check should fail: %+v", c)
	}
	if c := checkByName(t, rep, "profit factor"); !c.Passed {
		t.Fatalf("the ratio itself still clears: %+v", c)
	}
}

func TestSignificanceIsDeterministic(t *testing.T) {
	w, g := testWindow("2026-05-22", "2026-08-30"), testGate()
	first := run(t, qualifyingRecord(), w, g)
	second := run(t, qualifyingRecord(), w, g)

	if first.Significance != second.Significance {
		t.Fatalf("two runs disagreed: %+v vs %+v", first.Significance, second.Significance)
	}
	if first.Significance.Paths != sigPaths {
		t.Fatalf("paths = %d, want %d", first.Significance.Paths, sigPaths)
	}
}

func TestSetupsIdentifiedNeedsTenTradesAndAnEdge(t *testing.T) {
	pid := func(n int64) *int64 { return &n }
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}, orders: map[int64]journal.Order{}}
	add := func(setup string, n int, net float64) {
		base := int64(len(src.proposals)) + 1
		for i := range n {
			id := base + int64(i)
			src.proposals = append(src.proposals, journal.Proposal{
				ID: id, Day: "2026-08-03", SetupID: setup, Status: journal.ProposalTaken,
			})
			src.orders[id] = journal.Order{ID: id, ProposalID: pid(id)}
			src.trades = append(src.trades, journal.Trade{
				ID: id, ClosedAt: at("2026-08-03", 10).Add(time.Duration(id) * time.Minute),
				NetPL: net, EntryOrderID: id,
			})
		}
	}
	add("B1", 10, 20)  // enough trades, positive
	add("R2", 12, -15) // enough trades, negative
	add("M3", 4, 90)   // profitable but too few to have shown anything
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	c := checkByName(t, rep, "setups identified")
	if !c.Passed || c.Actual != "1" {
		t.Fatalf("only B1 qualifies: %+v", c)
	}
	if c.Needed != "1+ (10 trades, +E)" {
		t.Fatalf("needed = %q", c.Needed)
	}
}

func TestHumanTradesNeverCountAsAnIdentifiedSetup(t *testing.T) {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}}
	for i := range 20 {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(i + 1), ClosedAt: at("2026-08-03", 10).Add(time.Duration(i) * time.Minute),
			NetPL: 30, EntryOrderID: int64(i + 1),
		})
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if c := checkByName(t, rep, "setups identified"); c.Passed || c.Actual != "0" {
		t.Fatalf("hand-placed trades identify no playbook rule: %+v", c)
	}
}

func TestMonthsCheckWantsBothASpanAndARecord(t *testing.T) {
	// The window is three months wide but every session sits in its last week.
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		calls:  []journal.Call{{ID: 1, Day: "2026-08-24"}, {ID: 2, Day: "2026-08-25"}},
	}
	rep := run(t, src, testWindow("2026-05-22", "2026-08-30"), testGate())

	c := checkByName(t, rep, "months covered")
	if c.Passed {
		t.Fatalf("a one-week record cannot cover three months: %+v", c)
	}
	if c.Actual != "0.2 mo" {
		t.Fatalf("actual = %q, want the binding six-day record", c.Actual)
	}
}
