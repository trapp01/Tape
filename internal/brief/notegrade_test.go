package brief

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// fileNotesOn archives a briefing whose reply carries the given watchlist notes,
// plus the call the notes are graded next to.
func fileNotesOn(t *testing.T, st *journal.Store, day, reply string, threshold float64) journal.Briefing {
	t.Helper()
	ctx := context.Background()
	b := journal.Briefing{
		Mode: journal.ModePaper, Day: day, Provider: "fake", Model: "fake-model-1",
		InputJSON: []byte(`{}`), OutputJSON: []byte(reply),
	}
	if err := st.InsertBriefing(ctx, &b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	if threshold > 0 {
		c := journal.Call{
			BriefingID: b.ID, Mode: journal.ModePaper, Day: day,
			Instrument: "SPY", Direction: "up", ThresholdPct: threshold, Rationale: "M2",
		}
		if err := st.InsertCall(ctx, &c); err != nil {
			t.Fatalf("InsertCall: %v", err)
		}
	}
	return b
}

// noteSessions gives each symbol a finished session with the given open and close.
func noteSessions(day string, oc map[string][2]float64) *fakeFeed {
	f := &fakeFeed{sessions: map[string]market.Session{}}
	for symbol, v := range oc {
		f.sessions[sessionKey(symbol, day)] = market.Session{
			Symbol: symbol, Day: day, Open: v[0], Close: v[1],
			High: max(v[0], v[1]), Low: min(v[0], v[1]), Complete: true,
		}
	}
	return f
}

const threeNotes = `{"watchlist":[
	{"symbol":"NVDA","bias":"bullish","note":"holds 118.40"},
	{"symbol":"AAPL","bias":"bearish","note":"loses 226"},
	{"symbol":"MSFT","bias":"neutral","note":"chops inside yesterday"}
]}`

// Every note is graded by the same threshold the call was filed under, so the
// two readings of the same morning are measured the same way.
func TestScoreDueGradesTheWatchlistNotes(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28", threeNotes, 0.5)
	// NVDA +1%, AAPL -1%, MSFT +0.1%: bullish right, bearish right, neutral right.
	feed := noteSessions("2026-08-28", map[string][2]float64{
		"NVDA": {100, 101}, "AAPL": {200, 198}, "MSFT": {300, 300.3},
		"SPY": {510, 512.55},
	})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 3 || len(report.NotesSkipped) != 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, n := range report.Notes {
		if !n.Score.Correct {
			t.Fatalf("%s %s should have graded correct: %+v", n.Score.Symbol, n.Score.Bias, n.Score)
		}
		if n.Score.ThresholdPct != 0.5 {
			t.Fatalf("%s graded against %v, want the call's 0.5", n.Score.Symbol, n.Score.ThresholdPct)
		}
		if n.Score.ID == 0 {
			t.Fatalf("%s was reported without a journal id", n.Score.Symbol)
		}
	}

	stored, err := st.NoteScoresInRange(context.Background(), journal.ModePaper, "2026-08-28", "2026-08-28")
	if err != nil {
		t.Fatalf("NoteScoresInRange: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("%d note scores on the record, want 3", len(stored))
	}

	// A note is graded once, so a second pass has nothing left to do.
	second, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second.Notes) != 0 {
		t.Fatalf("second pass regraded %d note(s)", len(second.Notes))
	}
}

// The re-run's own reads, so the grades name which briefing stood.
const threeNotesReversed = `{"watchlist":[
	{"symbol":"NVDA","bias":"bearish","note":"loses 118.40"},
	{"symbol":"AAPL","bias":"bullish","note":"holds 226"},
	{"symbol":"MSFT","bias":"neutral","note":"chops inside yesterday"}
]}`

// A forced re-run archives a second briefing for the session. The day's notes
// are still one set of reads, graded once, from the briefing that stood.
func TestAForcedReRunGradesTheDaysNotesOnce(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28", threeNotes, 0.5)
	newest := fileNotesOn(t, st, "2026-08-28", threeNotesReversed, 0.5)
	feed := noteSessions("2026-08-28", map[string][2]float64{
		"NVDA": {100, 101}, "AAPL": {200, 198}, "MSFT": {300, 300.3},
		"SPY": {510, 512.55},
	})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 3 {
		t.Fatalf("%d notes graded, want the day's three", len(report.Notes))
	}
	for _, n := range report.Notes {
		if n.Score.BriefingID != newest.ID {
			t.Fatalf("%s was graded from briefing #%d, want the re-run #%d",
				n.Score.Symbol, n.Score.BriefingID, newest.ID)
		}
	}
	if n := report.Notes[0]; n.Score.Symbol != "NVDA" || n.Score.Bias != "bearish" {
		t.Fatalf("first grade = %+v, want the re-run's NVDA read", n.Score)
	}

	stored, err := st.NoteScoresInRange(context.Background(), journal.ModePaper, "2026-08-28", "2026-08-28")
	if err != nil {
		t.Fatalf("NoteScoresInRange: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("%d note scores on the record, want 3", len(stored))
	}
}

// A briefing that filed no call is graded against the desk's configured default.
func TestScoreDueFallsBackToTheConfiguredThreshold(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28", `{"watchlist":[{"symbol":"NVDA","bias":"bullish","note":"holds"}]}`, 0)
	feed := noteSessions("2026-08-28", map[string][2]float64{"NVDA": {100, 101}})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 1 || report.Notes[0].Score.ThresholdPct != 0.3 {
		t.Fatalf("report = %+v", report)
	}
}

// twelveNotes is a full watchlist read, the size a real morning produces.
func twelveNotes() (string, []string) {
	symbols := []string{
		"NVDA", "AAPL", "MSFT", "AMZN", "GOOGL", "META",
		"TSLA", "AMD", "AVGO", "NFLX", "SPY", "QQQ",
	}
	notes := make([]string, 0, len(symbols))
	for _, s := range symbols {
		notes = append(notes, `{"symbol":"`+s+`","bias":"bullish","note":"holds"}`)
	}
	return `{"watchlist":[` + strings.Join(notes, ",") + `]}`, symbols
}

// A symbol the venue has no session for is named and left out. The other eleven
// reads still land: one delisted ticker cannot cost the day its whole sample.
func TestOneUngradeableSymbolDoesNotOrphanTheRest(t *testing.T) {
	st := testJournal(t)
	reply, symbols := twelveNotes()
	fileNotesOn(t, st, "2026-08-28", reply, 0.5)

	prices := map[string][2]float64{}
	for _, s := range symbols[:len(symbols)-1] {
		prices[s] = [2]float64{100, 101}
	}
	feed := noteSessions("2026-08-28", prices)
	missing := symbols[len(symbols)-1]

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 11 {
		t.Fatalf("%d notes graded, want the eleven with a session", len(report.Notes))
	}
	if len(report.NotesSkipped) != 1 || !strings.Contains(report.NotesSkipped[0], missing) {
		t.Fatalf("the skip must name %s alone: %v", missing, report.NotesSkipped)
	}

	stored, err := st.NoteScoresInRange(context.Background(), journal.ModePaper, "2026-08-28", "2026-08-28")
	if err != nil {
		t.Fatalf("NoteScoresInRange: %v", err)
	}
	if len(stored) != 11 {
		t.Fatalf("%d note scores on the record, want 11", len(stored))
	}

	// The day is graded once, so the second pass has nothing to add.
	second, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second.Notes) != 0 {
		t.Fatalf("second pass regraded %d note(s)", len(second.Notes))
	}
}

// A bias nothing can grade is named per symbol too, not charged to the day.
func TestAnUngradeableBiasIsNamedAlone(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28",
		`{"watchlist":[{"symbol":"NVDA","bias":"bullish","note":"holds"},{"symbol":"AAPL","bias":"sideways-ish","note":"?"}]}`, 0.5)
	feed := noteSessions("2026-08-28", map[string][2]float64{"NVDA": {100, 101}, "AAPL": {200, 198}})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 1 || report.Notes[0].Score.Symbol != "NVDA" {
		t.Fatalf("report = %+v", report)
	}
	if len(report.NotesSkipped) != 1 || !strings.Contains(report.NotesSkipped[0], "AAPL") {
		t.Fatalf("the skip must name AAPL: %v", report.NotesSkipped)
	}
}

// A briefing archived without a usable reply is named, not retried into a grade.
func TestScoreDueSkipsANoteBriefingWithNoReply(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28", "the model returned prose", 0.5)
	feed := noteSessions("2026-08-28", map[string][2]float64{"SPY": {510, 512.55}})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.NotesSkipped) != 1 || !strings.Contains(report.NotesSkipped[0], "usable reply") {
		t.Fatalf("report = %+v", report)
	}
}

// A briefing that noted nothing grades nothing and files nothing.
func TestScoreDueLeavesANoteLessBriefingAlone(t *testing.T) {
	st := testJournal(t)
	fileNotesOn(t, st, "2026-08-28", `{"watchlist":[]}`, 0.5)
	feed := noteSessions("2026-08-28", map[string][2]float64{"SPY": {510, 512.55}})

	report, err := ScoreDue(context.Background(), callDeps(st, feed), "2026-08-28")
	if err != nil {
		t.Fatalf("ScoreDue: %v", err)
	}
	if len(report.Notes) != 0 || len(report.NotesSkipped) != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestNoteCorrect(t *testing.T) {
	cases := []struct {
		bias      string
		actualPct float64
		want      bool
	}{
		{"bullish", 0.5, true},
		{"bullish", 0.49, false},
		{"bearish", -0.5, true},
		{"bearish", -0.49, false},
		{"neutral", 0.49, true},
		{"neutral", -0.49, true},
		{"neutral", 0.5, false},
	}
	for _, tc := range cases {
		if got := noteCorrect(tc.bias, tc.actualPct, 0.5); got != tc.want {
			t.Errorf("noteCorrect(%q, %v, 0.5) = %v, want %v", tc.bias, tc.actualPct, got, tc.want)
		}
	}
}
