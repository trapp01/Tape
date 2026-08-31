package journal

import (
	"context"
	"testing"
	"time"
)

var noteScoreTime = time.Date(2026, 8, 28, 20, 45, 0, 0, time.UTC)

func newNoteScore(briefingID int64, mode, day, symbol, bias string, correct bool) *NoteScore {
	return &NoteScore{
		BriefingID:   briefingID,
		Mode:         mode,
		Day:          day,
		Symbol:       symbol,
		Bias:         bias,
		ThresholdPct: 0.3,
		ScoredAt:     noteScoreTime,
		Open:         128.40,
		Close:        129.60,
		ActualPct:    0.93,
		Correct:      correct,
	}
}

func TestInsertNoteScoresRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)

	batch := []*NoteScore{
		newNoteScore(briefingID, ModePaper, proposalDay, "NVDA", "bullish", true),
		newNoteScore(briefingID, ModePaper, proposalDay, "AAPL", "neutral", false),
	}
	if err := s.InsertNoteScores(ctx, batch); err != nil {
		t.Fatalf("InsertNoteScores: %v", err)
	}
	for i, n := range batch {
		if n.ID == 0 {
			t.Fatalf("note score %d was not given an id", i)
		}
	}

	got, err := s.NoteScoresInRange(ctx, ModePaper, proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("NoteScoresInRange: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d note scores, want 2", len(got))
	}
	first := got[0]
	if first.Symbol != "NVDA" || first.Bias != "bullish" || !first.Correct {
		t.Errorf("identity did not survive: %+v", first)
	}
	closeTo(t, "ThresholdPct", first.ThresholdPct, 0.3)
	closeTo(t, "Open", first.Open, 128.40)
	closeTo(t, "Close", first.Close, 129.60)
	closeTo(t, "ActualPct", first.ActualPct, 0.93)
	if !first.ScoredAt.Equal(noteScoreTime) {
		t.Errorf("ScoredAt = %v, want %v", first.ScoredAt, noteScoreTime)
	}
	if got[1].Correct {
		t.Error("the neutral note came back correct")
	}
}

func TestInsertNoteScoresEmptyIsNoOp(t *testing.T) {
	if err := newStore(t).InsertNoteScores(context.Background(), nil); err != nil {
		t.Fatalf("InsertNoteScores(nil): %v", err)
	}
}

func TestInsertNoteScoresStampsScoredAt(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)

	n := newNoteScore(briefingID, ModePaper, proposalDay, "NVDA", "bullish", true)
	n.ScoredAt = time.Time{}
	if err := s.InsertNoteScores(ctx, []*NoteScore{n}); err != nil {
		t.Fatalf("InsertNoteScores: %v", err)
	}
	if n.ScoredAt.IsZero() {
		t.Error("ScoredAt was not stamped")
	}
}

func TestInsertNoteScoresRefusesDuplicateSymbol(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)

	if err := s.InsertNoteScores(ctx, []*NoteScore{newNoteScore(briefingID, ModePaper, proposalDay, "NVDA", "bullish", true)}); err != nil {
		t.Fatalf("first InsertNoteScores: %v", err)
	}
	err := s.InsertNoteScores(ctx, []*NoteScore{newNoteScore(briefingID, ModePaper, proposalDay, "NVDA", "bearish", false)})
	if err == nil {
		t.Fatal("a second grade for the same briefing and symbol was accepted, want an error")
	}

	got, err := s.NoteScoresInRange(ctx, ModePaper, proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("NoteScoresInRange: %v", err)
	}
	if len(got) != 1 || got[0].Bias != "bullish" {
		t.Fatalf("note scores = %+v, want only the first grade", got)
	}
}

func TestInsertNoteScoresRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)

	cases := map[string]func(*NoteScore){
		"no briefing":    func(n *NoteScore) { n.BriefingID = 0 },
		"bad mode":       func(n *NoteScore) { n.Mode = "sim" },
		"bad day":        func(n *NoteScore) { n.Day = "yesterday" },
		"no symbol":      func(n *NoteScore) { n.Symbol = "" },
		"no bias":        func(n *NoteScore) { n.Bias = "" },
		"zero threshold": func(n *NoteScore) { n.ThresholdPct = 0 },
	}
	for name, invalidate := range cases {
		n := newNoteScore(briefingID, ModePaper, proposalDay, "NVDA", "bullish", true)
		invalidate(n)
		if err := s.InsertNoteScores(ctx, []*NoteScore{n}); err == nil {
			t.Errorf("InsertNoteScores accepted %s, want an error", name)
		}
	}
	if err := s.InsertNoteScores(ctx, []*NoteScore{nil}); err == nil {
		t.Error("InsertNoteScores accepted a nil note, want an error")
	}
}

func TestNoteScoresInRangeKeepsModesApart(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for _, mode := range []string{ModePaper, ModeLive} {
		briefingID := seedBriefing(t, s, mode, proposalDay)
		if err := s.InsertNoteScores(ctx, []*NoteScore{newNoteScore(briefingID, mode, proposalDay, "NVDA", "bullish", true)}); err != nil {
			t.Fatalf("InsertNoteScores %s: %v", mode, err)
		}
	}

	paper, err := s.NoteScoresInRange(ctx, ModePaper, proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("NoteScoresInRange paper: %v", err)
	}
	if len(paper) != 1 || paper[0].Mode != ModePaper {
		t.Fatalf("paper = %+v, want one paper row", paper)
	}
	both, err := s.NoteScoresInRange(ctx, "", proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("NoteScoresInRange both: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("got %d note scores for both modes, want 2", len(both))
	}
}

func TestUnscoredNoteBriefings(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	older := seedBriefing(t, s, ModePaper, "2026-08-27")
	newer := seedBriefing(t, s, ModePaper, "2026-08-28")
	seedBriefing(t, s, ModeLive, "2026-08-27")

	got, err := s.UnscoredNoteBriefings(ctx, ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings: %v", err)
	}
	if len(got) != 2 || got[0].ID != older || got[1].ID != newer {
		t.Fatalf("got %+v, want both paper briefings oldest first", got)
	}

	// A graded briefing drops off the list.
	if err := s.InsertNoteScores(ctx, []*NoteScore{newNoteScore(older, ModePaper, "2026-08-27", "NVDA", "bullish", true)}); err != nil {
		t.Fatalf("InsertNoteScores: %v", err)
	}
	got, err = s.UnscoredNoteBriefings(ctx, ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings after scoring: %v", err)
	}
	if len(got) != 1 || got[0].ID != newer {
		t.Fatalf("got %+v, want only the ungraded briefing", got)
	}

	// throughDay is inclusive and stops the list.
	got, err = s.UnscoredNoteBriefings(ctx, ModePaper, "2026-08-27")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings through 2026-08-27: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing: the only 08-27 paper briefing is graded", got)
	}

	both, err := s.UnscoredNoteBriefings(ctx, "", "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings both: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("got %d ungraded briefings for both modes, want 2", len(both))
	}
}

// A forced re-run leaves two briefings on one session. Only the one that stood
// is offered for grading, and grading it closes the day for the other.
func TestUnscoredNoteBriefingsTakesTheNewestPerSession(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedBriefing(t, s, ModePaper, "2026-08-28")
	newest := seedBriefing(t, s, ModePaper, "2026-08-28")

	got, err := s.UnscoredNoteBriefings(ctx, ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings: %v", err)
	}
	if len(got) != 1 || got[0].ID != newest {
		t.Fatalf("got %+v, want only the re-run #%d", got, newest)
	}

	if err := s.InsertNoteScores(ctx, []*NoteScore{
		newNoteScore(newest, ModePaper, "2026-08-28", "NVDA", "bullish", true),
	}); err != nil {
		t.Fatalf("InsertNoteScores: %v", err)
	}
	got, err = s.UnscoredNoteBriefings(ctx, ModePaper, "2026-08-28")
	if err != nil {
		t.Fatalf("UnscoredNoteBriefings after scoring: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want nothing: the session's notes are graded", got)
	}
}

// One grade per symbol per session, whichever briefing named it.
func TestNoteScoresAreUniquePerSessionAndSymbol(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first := seedBriefing(t, s, ModePaper, "2026-08-28")
	second := seedBriefing(t, s, ModePaper, "2026-08-28")

	if err := s.InsertNoteScores(ctx, []*NoteScore{
		newNoteScore(first, ModePaper, "2026-08-28", "NVDA", "bullish", true),
	}); err != nil {
		t.Fatalf("InsertNoteScores: %v", err)
	}
	err := s.InsertNoteScores(ctx, []*NoteScore{
		newNoteScore(second, ModePaper, "2026-08-28", "NVDA", "bearish", false),
	})
	if err == nil {
		t.Fatal("a second grade of NVDA on the same session was accepted")
	}
}

func TestUnscoredNoteBriefingsRejectsBadDay(t *testing.T) {
	if _, err := newStore(t).UnscoredNoteBriefings(context.Background(), ModePaper, "nope"); err == nil {
		t.Fatal("UnscoredNoteBriefings accepted a malformed day, want an error")
	}
}
