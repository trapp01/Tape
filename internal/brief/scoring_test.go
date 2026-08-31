package brief

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// fileCallOn archives a briefing and its call so there is something to grade.
func fileCallOn(t *testing.T, st *journal.Store, day, instrument, direction string, threshold float64) journal.Call {
	t.Helper()
	ctx := context.Background()
	b := journal.Briefing{
		Mode: journal.ModePaper, Day: day, Provider: "fake", Model: "fake-model-1",
		InputJSON: []byte(`{}`), OutputJSON: []byte(`{}`),
	}
	if err := st.InsertBriefing(ctx, &b); err != nil {
		t.Fatalf("InsertBriefing %s: %v", day, err)
	}
	c := journal.Call{
		BriefingID: b.ID, Mode: journal.ModePaper, Day: day,
		Instrument: instrument, Direction: direction, ThresholdPct: threshold,
		Rationale: "M2",
	}
	if err := st.InsertCall(ctx, &c); err != nil {
		t.Fatalf("InsertCall %s: %v", day, err)
	}
	return c
}

// sessionFeed carries one finished SPY session per day.
func sessionFeed(days map[string][2]float64) *fakeFeed {
	f := &fakeFeed{sessions: map[string]market.Session{}}
	for day, oc := range days {
		f.sessions[sessionKey("SPY", day)] = market.Session{
			Symbol: "SPY", Day: day,
			Open: oc[0], High: max(oc[0], oc[1]), Low: min(oc[0], oc[1]), Close: oc[1],
			Volume: 90_000_000, Complete: true,
		}
	}
	return f
}

// The call is graded against its own session. A stale or next-day session would
// put a number in the record that the call was never about.
func TestScoreDueGradesTheCallsOwnSession(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-31", "SPY", "up", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {400, 404}, "2026-08-31": {500, 510}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-31")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if got := report.Scored[0].Outcome; got.Open != 500 || got.Close != 510 {
		t.Fatalf("graded against open/close %v/%v, want the 2026-08-31 session 500/510", got.Open, got.Close)
	}
	if len(feed.asked) != 1 || feed.asked[0] != sessionKey("SPY", "2026-08-31") {
		t.Fatalf("the scorer asked for %v, want only the call's own session", feed.asked)
	}
}

// A Friday call is gradeable: nothing about the last session of the week makes
// its own session unreachable.
func TestScoreDueGradesAFridayCall(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "up", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 512.55}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 1 || len(report.Skipped) != 0 {
		t.Fatalf("a Friday call must grade, report = %+v", report)
	}
}

func TestScoreDueGradesAgainstTheSession(t *testing.T) {
	st := testJournal(t)
	call := fileCallOn(t, st, "2026-08-28", "SPY", "up", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 512.55}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 1 || len(report.Skipped) != 0 {
		t.Fatalf("report = %+v", report)
	}
	got := report.Scored[0].Outcome
	if !got.Correct || got.Open != 510 || got.Close != 512.55 {
		t.Fatalf("outcome = %+v", got)
	}
	if delta := got.ActualPct - 0.5; delta > 0.001 || delta < -0.001 {
		t.Fatalf("actual = %v%%, want 0.5%%", got.ActualPct)
	}

	stored, err := st.CallByBriefing(context.Background(), call.BriefingID)
	if err != nil {
		t.Fatalf("CallByBriefing: %v", err)
	}
	if stored.ScoredAt == nil || stored.Correct == nil || !*stored.Correct {
		t.Fatalf("the call was not graded on the row: %+v", stored)
	}
}

// A session still in progress grades nothing: the close it would be graded
// against has not printed.
func TestScoreDueSkipsAnIncompleteSession(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "up", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 512.55}})
	running := feed.sessions[sessionKey("SPY", "2026-08-28")]
	running.Complete = false
	feed.sessions[sessionKey("SPY", "2026-08-28")] = running

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Skipped[0], "session not final yet") {
		t.Fatalf("skip reason = %q", report.Skipped[0])
	}

	due, err := st.UnscoredCalls(context.Background(), journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredCalls: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("the call must stay open for the next run, %d due", len(due))
	}
}

// Without the session the call stays open rather than being guessed at.
func TestScoreDueSkipsAMissingSession(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "up", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-27": {510, 512.55}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Skipped[0], "no prints for SPY on 2026-08-28") {
		t.Fatalf("skip reason = %q", report.Skipped[0])
	}

	due, err := st.UnscoredCalls(context.Background(), journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredCalls: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("the call must stay open for the next run, %d due", len(due))
	}
}

// A call is graded once, so a second pass has nothing left to do.
func TestScoreDueNeverRescores(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "down", 0.3)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 512.55}})

	if _, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	second, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second.Scored) != 0 || len(second.Skipped) != 0 {
		t.Fatalf("second pass did work it should not have: %+v", second)
	}
}

// The threshold on the row is the one the call was filed under. A stored zero is
// not a licence to grade against something else.
func TestScoreDueUsesTheStoredThreshold(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "up", 0.8)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 511.02}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	got := report.Scored[0].Outcome
	if got.ThresholdPct != 0.8 {
		t.Fatalf("threshold = %v, want the stored 0.8", got.ThresholdPct)
	}
	if got.Correct {
		t.Fatalf("a 0.2%% move does not clear a 0.8%% bar: %+v", got)
	}
}

// A row that predates the effective-threshold rule carries zero, which grades
// nothing: under it "up" and "down" would both be correct.
func TestScoreDueRefusesAStoredZeroThreshold(t *testing.T) {
	st := testJournal(t)
	fileCallOn(t, st, "2026-08-28", "SPY", "up", 0)
	feed := sessionFeed(map[string][2]float64{"2026-08-28": {510, 511.02}})

	report, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Scored) != 0 || len(report.Skipped) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Skipped[0], "threshold") {
		t.Fatalf("skip reason = %q", report.Skipped[0])
	}
}

func TestAccuracyCountsTheTrailingWindow(t *testing.T) {
	loc := mountain(t)
	st := testJournal(t)
	for _, day := range []string{"2026-08-26", "2026-08-27", "2026-08-28"} {
		fileCallOn(t, st, day, "SPY", "up", 0.3)
		close := 512.55
		if day == "2026-08-27" {
			close = 509.0
		}
		feed := sessionFeed(map[string][2]float64{day: {510, close}})
		if _, err := ScoreDue(context.Background(), st, feed, journal.ModePaper, day); err != nil {
			t.Fatalf("scoring %s: %v", day, err)
		}
	}

	now := time.Date(2026, 8, 28, 16, 0, 0, 0, loc)
	correct, total, err := Accuracy(context.Background(), st, journal.ModePaper, 30, now, loc)
	if err != nil {
		t.Fatalf("Accuracy: %v", err)
	}
	if correct != 2 || total != 3 {
		t.Fatalf("accuracy = %d/%d, want 2/3", correct, total)
	}

	// A one-day window sees only today's call.
	correct, total, err = Accuracy(context.Background(), st, journal.ModePaper, 1, now, loc)
	if err != nil {
		t.Fatalf("Accuracy: %v", err)
	}
	if correct != 1 || total != 1 {
		t.Fatalf("one-day accuracy = %d/%d, want 1/1", correct, total)
	}
}
