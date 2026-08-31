package journal

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const noteScoreColumns = `id, briefing_id, mode, day, symbol, bias, threshold_pct,
	scored_at, open, close, actual_pct, correct`

// InsertNoteScores writes a briefing's graded watchlist notes in one transaction
// and fills in the IDs. A symbol already graded for that briefing fails the whole
// batch: a note is graded once.
func (s *Store) InsertNoteScores(ctx context.Context, ns []*NoteScore) error {
	if len(ns) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i, n := range ns {
		if n == nil {
			return fmt.Errorf("journal: insert note scores: note %d is nil", i)
		}
		if err := validateNoteScore(n); err != nil {
			return err
		}
		if n.ScoredAt.IsZero() {
			n.ScoredAt = now
		}
	}

	const insert = `INSERT INTO note_scores (
		briefing_id, mode, day, symbol, bias, threshold_pct,
		scored_at, open, close, actual_pct, correct
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: insert note scores for briefing %d: %w", ns[0].BriefingID, err)
	}
	defer tx.Rollback()

	ids := make([]int64, len(ns))
	for i, n := range ns {
		res, err := tx.ExecContext(ctx, insert,
			n.BriefingID, n.Mode, n.Day, n.Symbol, n.Bias, n.ThresholdPct,
			formatTime(n.ScoredAt), n.Open, n.Close, n.ActualPct, boolToInt(n.Correct))
		if err != nil {
			return fmt.Errorf("journal: insert note score %s for briefing %d: %w", n.Symbol, n.BriefingID, err)
		}
		if ids[i], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("journal: insert note score %s for briefing %d: reading id: %w", n.Symbol, n.BriefingID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: insert note scores for briefing %d: %w", ns[0].BriefingID, err)
	}
	for i, n := range ns {
		n.ID = ids[i]
	}
	return nil
}

func validateNoteScore(n *NoteScore) error {
	if n.BriefingID <= 0 {
		return errors.New("journal: insert note score: briefing id is not set")
	}
	if n.Mode != ModePaper && n.Mode != ModeLive {
		return fmt.Errorf("journal: insert note score for briefing %d: mode must be %q or %q, got %q",
			n.BriefingID, ModePaper, ModeLive, n.Mode)
	}
	if err := validateDay(n.Day); err != nil {
		return fmt.Errorf("journal: insert note score for briefing %d: %w", n.BriefingID, err)
	}
	if n.Symbol == "" {
		return fmt.Errorf("journal: insert note score for briefing %d: symbol is empty", n.BriefingID)
	}
	if n.Bias == "" {
		return fmt.Errorf("journal: insert note score %s for briefing %d: bias is empty", n.Symbol, n.BriefingID)
	}
	if n.ThresholdPct <= 0 {
		return fmt.Errorf("journal: insert note score %s for briefing %d: threshold must be positive, got %v",
			n.Symbol, n.BriefingID, n.ThresholdPct)
	}
	return nil
}

// NoteScoresInRange returns graded watchlist notes in [fromDay, toDay], oldest
// first. An empty mode covers both paper and live.
func (s *Store) NoteScoresInRange(ctx context.Context, mode, fromDay, toDay string) ([]NoteScore, error) {
	what := fmt.Sprintf("note scores for %s %s..%s", mode, fromDay, toDay)
	if err := validateDayRange(what, fromDay, toDay); err != nil {
		return nil, err
	}
	query, args := dayRangeQuery(`SELECT `+noteScoreColumns+` FROM note_scores`, mode, fromDay, toDay, `day, id`)
	return queryList(ctx, s, what, query, args, scanNoteScore)
}

// UnscoredNoteBriefings returns the briefing that stood for each session on or
// before throughDay whose notes nothing has graded yet, oldest first. A forced
// re-run archives a second briefing for the day; the newest is the one whose
// reads the session is judged on. An empty mode covers both.
func (s *Store) UnscoredNoteBriefings(ctx context.Context, mode, throughDay string) ([]Briefing, error) {
	if err := validateDay(throughDay); err != nil {
		return nil, fmt.Errorf("journal: unscored note briefings: %w", err)
	}
	query := `SELECT ` + briefingColumns + ` FROM briefings b
		WHERE b.day <= ?
		  AND NOT EXISTS (SELECT 1 FROM note_scores n WHERE n.mode = b.mode AND n.day = b.day)
		  AND b.id = (SELECT b2.id FROM briefings b2
		              WHERE b2.mode = b.mode AND b2.day = b.day
		              ORDER BY b2.generated_at DESC, b2.id DESC LIMIT 1)`
	args := []any{throughDay}
	if mode != "" {
		query += ` AND b.mode = ?`
		args = append(args, mode)
	}
	query += ` ORDER BY b.day, b.id`

	what := fmt.Sprintf("unscored note briefings for mode %q", mode)
	return queryList(ctx, s, what, query, args, scanBriefing)
}

func scanNoteScore(sc scanner) (NoteScore, error) {
	var (
		n        NoteScore
		correct  int64
		scoredAt string
	)
	err := sc.Scan(&n.ID, &n.BriefingID, &n.Mode, &n.Day, &n.Symbol, &n.Bias, &n.ThresholdPct,
		&scoredAt, &n.Open, &n.Close, &n.ActualPct, &correct)
	if err != nil {
		return NoteScore{}, err
	}
	n.Correct = correct == 1
	if n.ScoredAt, err = parseTime(scoredAt); err != nil {
		return NoteScore{}, err
	}
	return n, nil
}
