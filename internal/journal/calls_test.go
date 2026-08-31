package journal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestInsertAndFetchCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b, c := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)
	if c.ID == 0 {
		t.Fatal("InsertCall left ID at zero")
	}

	got, err := s.CallByBriefing(ctx, b.ID)
	if err != nil {
		t.Fatalf("CallByBriefing: %v", err)
	}
	if got.Instrument != "SPY" || got.Direction != "up" || got.ThresholdPct != 0.3 {
		t.Errorf("call = %s %s %v", got.Instrument, got.Direction, got.ThresholdPct)
	}
	if got.ScoredAt != nil || got.Correct != nil || got.Open != nil || got.Close != nil || got.ActualPct != nil {
		t.Errorf("a fresh call carries a score: %+v", got)
	}

	if _, err := s.CallByBriefing(ctx, b.ID+99); !errors.Is(err, ErrNotFound) {
		t.Errorf("CallByBriefing missing: %v, want ErrNotFound", err)
	}
}

func TestOneCallPerBriefing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b, _ := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)

	second := &Call{
		BriefingID: b.ID,
		Mode:       ModePaper,
		Day:        "2026-08-24",
		Instrument: "QQQ",
		Direction:  "down",
	}
	if err := s.InsertCall(ctx, second); err == nil {
		t.Fatal("a second call on one briefing succeeded, want an error")
	}
}

// A call can be swapped while it is unscored, keeping its id so the briefing
// that replaced it is the one on the row.
func TestReplaceCallSwapsAnUnscoredCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	_, first := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)

	second := newBriefing(ModePaper, "2026-08-24", at.Add(time.Hour))
	if err := s.InsertBriefing(ctx, second); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	swap := &Call{
		BriefingID: second.ID, Mode: ModePaper, Day: "2026-08-24",
		Instrument: "QQQ", Direction: "down", ThresholdPct: 0.5, Rationale: "R1 fade",
	}
	if err := s.ReplaceCall(ctx, first.ID, swap); err != nil {
		t.Fatalf("ReplaceCall: %v", err)
	}
	if swap.ID != first.ID {
		t.Fatalf("ReplaceCall set id %d, want the row it rewrote %d", swap.ID, first.ID)
	}

	got, err := s.CallByDay(ctx, ModePaper, "2026-08-24")
	if err != nil {
		t.Fatalf("CallByDay: %v", err)
	}
	if got.Instrument != "QQQ" || got.Direction != "down" || got.ThresholdPct != 0.5 || got.BriefingID != second.ID {
		t.Fatalf("call = %+v, want the replacement", got)
	}

	// The day still carries exactly one call.
	if _, err := s.CallByBriefing(ctx, first.BriefingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the first briefing must no longer own a call, got %v", err)
	}
}

// A graded call is the record. Replacing it would rewrite what was predicted
// after the session already answered it.
func TestReplaceCallRefusesAGradedCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	_, c := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)
	if err := s.ScoreCall(ctx, c.ID, 510, 512.55, 0.5, true, at.Add(8*time.Hour)); err != nil {
		t.Fatalf("ScoreCall: %v", err)
	}

	swap := &Call{
		BriefingID: c.BriefingID, Mode: ModePaper, Day: "2026-08-24",
		Instrument: "QQQ", Direction: "down", ThresholdPct: 0.5, Rationale: "R1",
	}
	err := s.ReplaceCall(ctx, c.ID, swap)
	if err == nil {
		t.Fatal("replacing a graded call succeeded")
	}
	if !strings.Contains(err.Error(), "already scored") {
		t.Fatalf("refusal %q does not say the call is already scored", err)
	}

	if err := s.ReplaceCall(ctx, c.ID+99, swap); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replacing a call that does not exist: %v, want ErrNotFound", err)
	}
}

func TestInsertCallRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b := newBriefing(ModePaper, "2026-08-24", at)
	if err := s.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}

	tests := []struct {
		name string
		call Call
	}{
		{"no briefing", Call{Mode: ModePaper, Day: "2026-08-24", Instrument: "SPY", Direction: "up"}},
		{"bad mode", Call{BriefingID: b.ID, Mode: "sim", Day: "2026-08-24", Instrument: "SPY", Direction: "up"}},
		{"no day", Call{BriefingID: b.ID, Mode: ModePaper, Instrument: "SPY", Direction: "up"}},
		{"no instrument", Call{BriefingID: b.ID, Mode: ModePaper, Day: "2026-08-24", Direction: "up"}},
		{"no direction", Call{BriefingID: b.ID, Mode: ModePaper, Day: "2026-08-24", Instrument: "SPY"}},
		{"negative threshold", Call{BriefingID: b.ID, Mode: ModePaper, Day: "2026-08-24", Instrument: "SPY", Direction: "up", ThresholdPct: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.call
			if err := s.InsertCall(ctx, &c); err == nil {
				t.Fatal("InsertCall succeeded, want an error")
			}
		})
	}
	if err := s.InsertCall(ctx, nil); err == nil {
		t.Error("InsertCall(nil) succeeded, want an error")
	}
}

func TestUnscoredCallsStopAtThroughDay(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 20, 12, 52, 0, 0, time.UTC)
	_, monday := insertBriefingWithCall(t, s, ModePaper, "2026-08-20", "SPY", "up", at)
	insertBriefingWithCall(t, s, ModePaper, "2026-08-21", "QQQ", "down", at.AddDate(0, 0, 1))
	insertBriefingWithCall(t, s, ModePaper, "2026-08-25", "SPY", "flat", at.AddDate(0, 0, 5))
	insertBriefingWithCall(t, s, ModeLive, "2026-08-21", "IWM", "up", at.AddDate(0, 0, 1))

	through, err := s.UnscoredCalls(ctx, ModePaper, "2026-08-21")
	if err != nil {
		t.Fatalf("UnscoredCalls: %v", err)
	}
	if len(through) != 2 {
		t.Fatalf("got %d unscored paper calls through 2026-08-21, want 2", len(through))
	}
	if through[0].Day != "2026-08-20" || through[1].Day != "2026-08-21" {
		t.Errorf("order = %s, %s, want oldest first", through[0].Day, through[1].Day)
	}

	scoredAt := time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC)
	if err := s.ScoreCall(ctx, monday.ID, 500, 505, 1.0, true, scoredAt); err != nil {
		t.Fatalf("ScoreCall: %v", err)
	}
	after, err := s.UnscoredCalls(ctx, ModePaper, "2026-08-21")
	if err != nil {
		t.Fatalf("UnscoredCalls after scoring: %v", err)
	}
	if len(after) != 1 || after[0].Day != "2026-08-21" {
		t.Errorf("got %d unscored calls after scoring Monday, want the 21st only", len(after))
	}

	both, err := s.UnscoredCalls(ctx, "", "2026-08-21")
	if err != nil {
		t.Fatalf("UnscoredCalls both modes: %v", err)
	}
	if len(both) != 2 {
		t.Errorf("got %d unscored calls across modes, want 2", len(both))
	}

	if _, err := s.UnscoredCalls(ctx, ModePaper, "not-a-day"); err == nil {
		t.Error("UnscoredCalls accepted a malformed day")
	}
}

func TestScoreCallWritesTheGradeOnce(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b, c := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)
	scoredAt := time.Date(2026, 8, 24, 22, 5, 0, 0, time.UTC)

	if err := s.ScoreCall(ctx, c.ID, 500, 505, 1.0, true, scoredAt); err != nil {
		t.Fatalf("ScoreCall: %v", err)
	}
	got, err := s.CallByBriefing(ctx, b.ID)
	if err != nil {
		t.Fatalf("CallByBriefing: %v", err)
	}
	if got.ScoredAt == nil || !got.ScoredAt.Equal(scoredAt) {
		t.Errorf("ScoredAt = %v, want %v", got.ScoredAt, scoredAt)
	}
	if got.Open == nil || *got.Open != 500 || got.Close == nil || *got.Close != 505 {
		t.Errorf("open/close = %v/%v, want 500/505", got.Open, got.Close)
	}
	if got.ActualPct == nil || *got.ActualPct != 1.0 {
		t.Errorf("ActualPct = %v, want 1", got.ActualPct)
	}
	if got.Correct == nil || !*got.Correct {
		t.Errorf("Correct = %v, want true", got.Correct)
	}

	err = s.ScoreCall(ctx, c.ID, 500, 495, -1.0, false, scoredAt.Add(time.Hour))
	if err == nil {
		t.Fatal("re-scoring succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "already scored") {
		t.Errorf("refusal %q does not say the call is already scored", err)
	}
	again, err := s.CallByBriefing(ctx, b.ID)
	if err != nil {
		t.Fatalf("CallByBriefing after the refusal: %v", err)
	}
	if again.Correct == nil || !*again.Correct {
		t.Error("the refused re-score changed the grade")
	}

	if err := s.ScoreCall(ctx, c.ID+99, 1, 1, 0, false, scoredAt); !errors.Is(err, ErrNotFound) {
		t.Errorf("ScoreCall on a missing call: %v, want ErrNotFound", err)
	}
	if err := s.ScoreCall(ctx, 0, 1, 1, 0, false, scoredAt); err == nil {
		t.Error("ScoreCall accepted a zero id")
	}
}

func TestScoreCallRecordsAWrongCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b, c := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)
	if err := s.ScoreCall(ctx, c.ID, 500, 495, -1.0, false, at.Add(10*time.Hour)); err != nil {
		t.Fatalf("ScoreCall: %v", err)
	}
	got, err := s.CallByBriefing(ctx, b.ID)
	if err != nil {
		t.Fatalf("CallByBriefing: %v", err)
	}
	if got.Correct == nil || *got.Correct {
		t.Errorf("Correct = %v, want false", got.Correct)
	}
}

func TestCallAccuracy(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 20, 12, 52, 0, 0, time.UTC)
	type row struct {
		mode    string
		day     string
		correct bool
		score   bool
	}
	rows := []row{
		{ModePaper, "2026-08-20", true, true},
		{ModePaper, "2026-08-21", false, true},
		{ModePaper, "2026-08-24", true, true},
		{ModePaper, "2026-08-25", false, false},
		{ModeLive, "2026-08-21", true, true},
	}
	for i, r := range rows {
		_, c := insertBriefingWithCall(t, s, r.mode, r.day, "SPY", "up", at.AddDate(0, 0, i))
		if !r.score {
			continue
		}
		if err := s.ScoreCall(ctx, c.ID, 500, 505, 1.0, r.correct, at.AddDate(0, 0, i).Add(10*time.Hour)); err != nil {
			t.Fatalf("ScoreCall %s %s: %v", r.mode, r.day, err)
		}
	}

	correct, total, err := s.CallAccuracy(ctx, ModePaper, "", "")
	if err != nil {
		t.Fatalf("CallAccuracy: %v", err)
	}
	if correct != 2 || total != 3 {
		t.Errorf("paper accuracy = %d/%d, want 2/3 (the unscored call is not counted)", correct, total)
	}

	correct, total, err = s.CallAccuracy(ctx, ModePaper, "2026-08-21", "2026-08-24")
	if err != nil {
		t.Fatalf("CallAccuracy windowed: %v", err)
	}
	if correct != 1 || total != 2 {
		t.Errorf("windowed accuracy = %d/%d, want 1/2", correct, total)
	}

	correct, total, err = s.CallAccuracy(ctx, ModeLive, "", "")
	if err != nil {
		t.Fatalf("CallAccuracy live: %v", err)
	}
	if correct != 1 || total != 1 {
		t.Errorf("live accuracy = %d/%d, want 1/1", correct, total)
	}

	correct, total, err = s.CallAccuracy(ctx, "", "", "")
	if err != nil {
		t.Fatalf("CallAccuracy both: %v", err)
	}
	if correct != 3 || total != 4 {
		t.Errorf("accuracy across modes = %d/%d, want 3/4", correct, total)
	}

	if _, _, err := s.CallAccuracy(ctx, ModePaper, "2026-8-1", ""); err == nil {
		t.Error("CallAccuracy accepted a malformed from day")
	}
	if _, _, err := s.CallAccuracy(ctx, ModePaper, "", "nope"); err == nil {
		t.Error("CallAccuracy accepted a malformed to day")
	}
}

func TestCallAccuracyIsEmptyWithNoCalls(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	correct, total, err := s.CallAccuracy(ctx, ModePaper, "", "")
	if err != nil {
		t.Fatalf("CallAccuracy: %v", err)
	}
	if correct != 0 || total != 0 {
		t.Errorf("accuracy = %d/%d, want 0/0", correct, total)
	}
}

// A day carries one call, so CallByDay is how the briefing knows not to file a
// second one over a re-run.
func TestCallByDayFindsTheDaysCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 20, 12, 52, 0, 0, time.UTC)
	_, filed := insertBriefingWithCall(t, s, ModePaper, "2026-08-20", "SPY", "up", at)
	insertBriefingWithCall(t, s, ModeLive, "2026-08-20", "QQQ", "down", at)

	got, err := s.CallByDay(ctx, ModePaper, "2026-08-20")
	if err != nil {
		t.Fatalf("CallByDay: %v", err)
	}
	if got.ID != filed.ID || got.Instrument != "SPY" {
		t.Fatalf("call = %+v, want the paper one #%d", got, filed.ID)
	}

	if _, err := s.CallByDay(ctx, ModePaper, "2026-08-21"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a day with no call must be ErrNotFound, got %v", err)
	}
	if _, err := s.CallByDay(ctx, ModePaper, "20 Aug 2026"); err == nil {
		t.Fatal("a malformed day must be refused")
	}
}
