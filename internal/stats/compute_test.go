package stats

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

func TestEmptyWindowReportsZerosAndAClosedGate(t *testing.T) {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}}
	rep := run(t, src, testWindow("2026-06-01", "2026-08-30"), testGate())

	if rep.Sessions != 0 || rep.Trades.Count != 0 || len(rep.BySetup) != 0 {
		t.Fatalf("expected an empty report, got %+v", rep)
	}
	if rep.Calls.Total != 0 || rep.Notes.Total != 0 || rep.Proposals.Taken != 0 {
		t.Fatalf("expected no graded work, got calls %+v notes %+v", rep.Calls, rep.Notes)
	}
	if rep.Refusals.Total != 0 || rep.Refusals.ByRule == nil {
		t.Fatalf("ByRule should be an empty map, got %+v", rep.Refusals)
	}
	if rep.Equity.StartingEquity != 5000 || rep.Equity.EndingEquity != 5000 {
		t.Fatalf("equity should stand still, got %+v", rep.Equity)
	}
	if rep.GateOpen {
		t.Fatal("an empty record must not open the gate")
	}
	for _, f := range []float64{
		rep.Trades.WinRate, rep.Trades.ExpectancyUSD, rep.Trades.ProfitFactor,
		rep.Calls.Accuracy, rep.Notes.Accuracy, rep.Equity.ReturnPct,
		rep.Equity.MaxDrawdownPct, rep.Significance.NullWinRate,
		rep.Significance.NullPassRate, rep.Significance.ExpectancyCI95Low,
	} {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			t.Fatalf("empty report produced %v", f)
		}
	}
	// Only the ceilings are vacuously satisfied by an empty record; every floor fails.
	ceilings := map[string]bool{"max drawdown": true, "refusals last month": true}
	for _, c := range rep.GateChecks {
		if c.Passed && !ceilings[c.Name] {
			t.Fatalf("check %q passed on an empty record: %+v", c.Name, c)
		}
	}
}

func TestSessionsCountEveryKindOfDayOnce(t *testing.T) {
	src := &fakeSource{
		ledger:    journal.Ledger{StartingEquity: 5000},
		proposals: []journal.Proposal{{ID: 1, Day: "2026-08-03"}, {ID: 2, Day: "2026-08-03"}},
		calls:     []journal.Call{{ID: 1, Day: "2026-08-03"}, {ID: 2, Day: "2026-08-04"}},
		fills: []journal.Fill{
			{ID: 1, FilledAt: at("2026-08-04", 10)},
			// 23:30 Mountain is 01:30 Eastern, so this fill belongs to the next
			// session, the way the journal's day columns are written.
			{ID: 2, FilledAt: at("2026-08-05", 23).Add(30 * time.Minute)},
		},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if rep.Sessions != 3 {
		t.Fatalf("sessions = %d, want 3 distinct days", rep.Sessions)
	}
	days := sessionDays(fetched(t, src, testWindow("2026-08-01", "2026-08-30")))
	if !days["2026-08-06"] || days["2026-08-05"] {
		t.Fatalf("the late fill belongs to 2026-08-06 Eastern, got %v", days)
	}
}

// fetched reads the whole record the way a report does, so a test can look at
// the day set the sections are cut from.
func fetched(t *testing.T, src Source, w Window) records {
	t.Helper()
	to := startOfDay(w.To, w.Loc).AddDate(0, 0, 1)
	rec, err := fetch(context.Background(), src, w.Mode, to, startOfDay(w.To, w.Loc).Format(dayLayout))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return rec
}

func TestMonthsCoveredCountsCalendarMonths(t *testing.T) {
	// Two calendar months, 59 days: a 30-day month would call this 1.97.
	got := months(day("2026-01-31"), day("2026-03-31"))
	if got < 1.999 || got > 2.001 {
		t.Fatalf("months = %v, want 2 calendar months", got)
	}
	if got := months(time.Time{}, day("2026-03-31")); got != 0 {
		t.Fatalf("months from no start = %v, want 0", got)
	}
}

// wholeRecordFixture is three months of winners followed by four losses inside
// the trailing window: the shape that made a windowed equity curve lie.
func wholeRecordFixture() *fakeSource {
	src := &fakeSource{ledger: journal.Ledger{StartingEquity: 5000}}
	closed := at("2026-06-01", 10)
	for i := range 30 {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(i + 1), ClosedAt: closed, NetPL: 100, GrossPL: 102, Costs: 2,
		})
		closed = closed.AddDate(0, 0, 2)
	}
	closed = at("2026-08-24", 10)
	for i := range 4 {
		src.trades = append(src.trades, journal.Trade{
			ID: int64(31 + i), ClosedAt: closed, NetPL: -260, GrossPL: -258, Costs: 2,
		})
		closed = closed.AddDate(0, 0, 1)
	}
	return src
}

func TestEquityReadsTheWholeRecordWhateverTheWindow(t *testing.T) {
	month := run(t, wholeRecordFixture(), testWindow("2026-08-01", "2026-08-30"), testGate())

	if !near(month.Equity.EndingEquity, 6960) {
		t.Fatalf("ending equity = %v, want the whole record's 6960", month.Equity.EndingEquity)
	}
	if got := month.Equity.MaxDrawdownPct; got < 12.99 || got > 13.01 {
		t.Fatalf("max drawdown = %v%%, want the 1040/8000 fall", got)
	}
	if !near(month.Equity.MaxDrawdownUSD, 1040) {
		t.Fatalf("max drawdown = %v, want 1040", month.Equity.MaxDrawdownUSD)
	}
	// The descriptive half still covers the window, and says so in its own fields.
	if month.Trades.Count != 4 || !near(month.Trades.NetPL, -1040) {
		t.Fatalf("window trades = %+v, want the four August losses", month.Trades)
	}
	if !near(month.Equity.WindowNetPL, -1040) {
		t.Fatalf("window net = %v, want -1040", month.Equity.WindowNetPL)
	}
	if got := month.Equity.WindowReturnPct; got < -13.01 || got > -12.99 {
		t.Fatalf("window return = %v%%, want -1040 against the 8000 it opened at", got)
	}
}

func TestStatsAndGateAgreeOnTheAccountAndTheGate(t *testing.T) {
	month := run(t, wholeRecordFixture(), testWindow("2026-08-01", "2026-08-30"), testGate())
	whole := run(t, wholeRecordFixture(), testWindow("2006-08-30", "2026-08-30"), testGate())

	// The Window fields describe the slice each report covers; everything else is
	// the account, and the account cannot depend on how you asked to see it.
	account := func(e EquityStats) EquityStats {
		e.WindowNetPL, e.WindowReturnPct = 0, 0
		return e
	}
	if account(month.Equity) != account(whole.Equity) {
		t.Fatalf("equity disagrees:\n  --month %+v\n  gate    %+v", month.Equity, whole.Equity)
	}
	if month.Significance != whole.Significance {
		t.Fatalf("significance disagrees:\n  --month %+v\n  gate    %+v", month.Significance, whole.Significance)
	}
	if month.GateOpen != whole.GateOpen {
		t.Fatalf("gate open = %v under --month, %v over the whole record", month.GateOpen, whole.GateOpen)
	}
	if len(month.GateChecks) != len(whole.GateChecks) {
		t.Fatalf("check counts differ: %v vs %v", checkNames(month), checkNames(whole))
	}
	for i, c := range month.GateChecks {
		if c != whole.GateChecks[i] {
			t.Errorf("check %q disagrees:\n  --month %+v\n  gate    %+v", c.Name, c, whole.GateChecks[i])
		}
	}
}

func TestWindowExcludesRecordsOutsideIt(t *testing.T) {
	src := &fakeSource{
		ledger: journal.Ledger{StartingEquity: 5000},
		trades: []journal.Trade{
			{ID: 1, ClosedAt: at("2026-07-31", 13), NetPL: 999},
			{ID: 2, ClosedAt: at("2026-08-03", 13), NetPL: 10},
			{ID: 3, ClosedAt: at("2026-08-30", 23), NetPL: 5},
			{ID: 4, ClosedAt: at("2026-08-31", 10), NetPL: -999},
		},
		calls: []journal.Call{{ID: 1, Day: "2026-07-31"}, {ID: 2, Day: "2026-08-03"}},
	}
	rep := run(t, src, testWindow("2026-08-01", "2026-08-30"), testGate())

	if rep.Trades.Count != 2 || !near(rep.Trades.NetPL, 15) {
		t.Fatalf("only the two in-window trades count, got %+v", rep.Trades)
	}
	if rep.Sessions != 1 {
		t.Fatalf("sessions = %d, want only 2026-08-03", rep.Sessions)
	}
}
