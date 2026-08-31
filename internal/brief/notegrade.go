package brief

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/trapp01/tape/internal/journal"
)

// ScoredNote is one graded watchlist bias note, next to the read that earned it.
type ScoredNote struct {
	Score journal.NoteScore
	Note  string
}

// gradeNotes grades the watchlist notes of every session nothing has graded yet.
// Twelve notes a day give the model's read a sample the single call of the day
// cannot, so one symbol nobody can grade is named and left out rather than
// costing the session all twelve.
func gradeNotes(ctx context.Context, d ScoreDeps, throughDay string, report *ScoreReport) error {
	due, err := d.Journal.UnscoredNoteBriefings(ctx, d.Mode, throughDay)
	if err != nil {
		return err
	}
	for _, b := range due {
		graded, skipped, skip, err := gradeBriefing(ctx, d, b)
		if err != nil {
			return err
		}
		if skip != "" {
			skipped = append(skipped, skip)
		}
		for _, why := range skipped {
			report.NotesSkipped = append(report.NotesSkipped,
				fmt.Sprintf("briefing #%d on %s: %s", b.ID, b.Day, why))
		}
		// A briefing that noted nothing is re-checked next run, which costs one
		// query and keeps the journal free of a row that means "there was nothing".
		if len(graded) == 0 {
			continue
		}
		rows := make([]*journal.NoteScore, len(graded))
		for i := range graded {
			rows[i] = &graded[i].Score
		}
		if err := d.Journal.InsertNoteScores(ctx, rows); err != nil {
			return err
		}
		report.Notes = append(report.Notes, graded...)
	}
	return nil
}

// gradeBriefing grades each of one briefing's notes against the session it was
// written for. skipped names the symbols that could not be graded; a non-empty
// skip is about the briefing itself and leaves every note on it open.
func gradeBriefing(ctx context.Context, d ScoreDeps, b journal.Briefing) ([]ScoredNote, []string, string, error) {
	var out Output
	if err := json.Unmarshal(b.OutputJSON, &out); err != nil {
		return nil, nil, "archived without a usable reply", nil
	}
	notes := dedupe(out.Watchlist)
	if len(notes) == 0 {
		return nil, nil, "", nil
	}
	threshold, err := noteThreshold(ctx, d, b)
	if err != nil {
		return nil, nil, "", err
	}
	if threshold <= 0 {
		return nil, nil, fmt.Sprintf("a threshold of %v decides nothing", threshold), nil
	}

	graded := make([]ScoredNote, 0, len(notes))
	var skipped []string
	for _, n := range notes {
		score, why := gradeNote(ctx, d, b, n, threshold)
		if why != "" {
			skipped = append(skipped, why)
			continue
		}
		graded = append(graded, score)
	}
	return graded, skipped, "", nil
}

// gradeNote settles one symbol's read against its own session. A non-empty
// reason names why this symbol could not be graded; the rest of the day still is.
func gradeNote(ctx context.Context, d ScoreDeps, b journal.Briefing, n WatchNote, threshold float64) (ScoredNote, string) {
	if !slices.Contains(biases, n.Bias) {
		return ScoredNote{}, fmt.Sprintf("%s carries no gradeable bias (%q)", n.Symbol, n.Bias)
	}
	s, skip := sessionFor(ctx, d, n.Symbol, b.Day)
	if skip != "" {
		return ScoredNote{}, fmt.Sprintf("%s: %s", n.Symbol, skip)
	}
	if s.Open <= 0 {
		return ScoredNote{}, fmt.Sprintf("%s has no open to measure from", n.Symbol)
	}
	actual := (s.Close - s.Open) / s.Open * 100
	return ScoredNote{
		Note: n.Note,
		Score: journal.NoteScore{
			BriefingID: b.ID, Mode: b.Mode, Day: b.Day,
			Symbol: n.Symbol, Bias: n.Bias, ThresholdPct: threshold,
			Open: s.Open, Close: s.Close, ActualPct: actual,
			Correct: noteCorrect(n.Bias, actual, threshold),
		},
	}, ""
}

// dedupe keeps the first note per symbol. One symbol is graded once per briefing,
// so a repeated read cannot count twice.
func dedupe(notes []WatchNote) []WatchNote {
	out := make([]WatchNote, 0, len(notes))
	seen := make(map[string]bool, len(notes))
	for _, n := range notes {
		if n.Symbol == "" || seen[n.Symbol] {
			continue
		}
		seen[n.Symbol] = true
		out = append(out, n)
	}
	return out
}

// noteCorrect grades one read: a direction has to clear the threshold, and
// neutral has to stay inside it.
func noteCorrect(bias string, actualPct, thresholdPct float64) bool {
	switch bias {
	case "bullish":
		return actualPct >= thresholdPct
	case "bearish":
		return actualPct <= -thresholdPct
	default:
		return math.Abs(actualPct) < thresholdPct
	}
}

// noteThreshold is the bar the briefing's own call was filed under, so a note and
// the call it sat next to are settled by the same size of move.
func noteThreshold(ctx context.Context, d ScoreDeps, b journal.Briefing) (float64, error) {
	c, err := d.Journal.CallByBriefing(ctx, b.ID)
	if errors.Is(err, journal.ErrNotFound) {
		return d.DefaultThresholdPct, nil
	}
	if err != nil {
		return 0, err
	}
	return c.ThresholdPct, nil
}
