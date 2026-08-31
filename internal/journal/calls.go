package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const callColumns = `id, briefing_id, mode, day, instrument, direction,
	threshold_pct, rationale, scored_at, open, close, actual_pct, correct`

// InsertCall writes the briefing's call of the day and fills in its ID. A
// briefing carries one call; a second insert against the same briefing fails.
func (s *Store) InsertCall(ctx context.Context, c *Call) error {
	if c == nil {
		return errors.New("journal: insert call: nil call")
	}
	if err := validateCall(c); err != nil {
		return err
	}

	const insert = `INSERT INTO calls (
		briefing_id, mode, day, instrument, direction, threshold_pct, rationale
	) VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, insert,
		c.BriefingID, c.Mode, c.Day, c.Instrument, c.Direction, c.ThresholdPct, c.Rationale)
	if err != nil {
		return fmt.Errorf("journal: insert call %s %s for briefing %d: %w", c.Direction, c.Instrument, c.BriefingID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert call %s %s for briefing %d: reading id: %w", c.Direction, c.Instrument, c.BriefingID, err)
	}
	c.ID = id
	return nil
}

// ReplaceCall rewrites an unscored call in place, keeping its id so the session
// still carries exactly one. A graded call is refused: the record of what was
// predicted cannot move after the session answered it.
func (s *Store) ReplaceCall(ctx context.Context, id int64, c *Call) error {
	if c == nil {
		return errors.New("journal: replace call: nil call")
	}
	if id <= 0 {
		return fmt.Errorf("journal: replace call: id must be positive, got %d", id)
	}
	if err := validateCall(c); err != nil {
		return err
	}

	const update = `UPDATE calls
		SET briefing_id = ?, instrument = ?, direction = ?, threshold_pct = ?, rationale = ?
		WHERE id = ? AND scored_at IS NULL`

	res, err := s.db.ExecContext(ctx, update,
		c.BriefingID, c.Instrument, c.Direction, c.ThresholdPct, c.Rationale, id)
	if err != nil {
		return fmt.Errorf("journal: replace call %d with %s %s: %w", id, c.Direction, c.Instrument, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: replace call %d: counting affected rows: %w", id, err)
	}
	if n == 0 {
		return s.unscoredRefusal(ctx, "replace", id)
	}
	c.ID = id
	return nil
}

func validateCall(c *Call) error {
	if c.BriefingID <= 0 {
		return errors.New("journal: insert call: briefing id is not set")
	}
	if c.Mode != ModePaper && c.Mode != ModeLive {
		return fmt.Errorf("journal: insert call for briefing %d: mode must be %q or %q, got %q", c.BriefingID, ModePaper, ModeLive, c.Mode)
	}
	if err := validateDay(c.Day); err != nil {
		return fmt.Errorf("journal: insert call for briefing %d: %w", c.BriefingID, err)
	}
	if c.Instrument == "" {
		return fmt.Errorf("journal: insert call for briefing %d: instrument is empty", c.BriefingID)
	}
	if c.Direction == "" {
		return fmt.Errorf("journal: insert call %s for briefing %d: direction is empty", c.Instrument, c.BriefingID)
	}
	if c.ThresholdPct < 0 {
		return fmt.Errorf("journal: insert call %s for briefing %d: threshold must not be negative, got %v", c.Instrument, c.BriefingID, c.ThresholdPct)
	}
	return nil
}

// CallByBriefing returns the call made in a briefing, or ErrNotFound.
func (s *Store) CallByBriefing(ctx context.Context, briefingID int64) (Call, error) {
	query := `SELECT ` + callColumns + ` FROM calls WHERE briefing_id = ?`
	c, err := scanCall(s.db.QueryRowContext(ctx, query, briefingID))
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, fmt.Errorf("journal: call for briefing %d: %w", briefingID, ErrNotFound)
	}
	if err != nil {
		return Call{}, fmt.Errorf("journal: call for briefing %d: %w", briefingID, err)
	}
	return c, nil
}

// CallByDay returns the call filed for a session, or ErrNotFound. A day carries
// one call: a re-run briefing does not get to replace what was predicted.
func (s *Store) CallByDay(ctx context.Context, mode, day string) (Call, error) {
	if err := validateDay(day); err != nil {
		return Call{}, fmt.Errorf("journal: call by day: %w", err)
	}
	query := `SELECT ` + callColumns + ` FROM calls WHERE mode = ? AND day = ? ORDER BY id LIMIT 1`
	c, err := scanCall(s.db.QueryRowContext(ctx, query, mode, day))
	if errors.Is(err, sql.ErrNoRows) {
		return Call{}, fmt.Errorf("journal: call for %s %s: %w", mode, day, ErrNotFound)
	}
	if err != nil {
		return Call{}, fmt.Errorf("journal: call for %s %s: %w", mode, day, err)
	}
	return c, nil
}

// UnscoredCalls returns calls made on or before throughDay that no session has
// graded yet, oldest first. An empty mode covers both paper and live.
func (s *Store) UnscoredCalls(ctx context.Context, mode, throughDay string) ([]Call, error) {
	if err := validateDay(throughDay); err != nil {
		return nil, fmt.Errorf("journal: unscored calls: %w", err)
	}
	query := `SELECT ` + callColumns + ` FROM calls WHERE scored_at IS NULL AND day <= ?`
	args := []any{throughDay}
	if mode != "" {
		query += ` AND mode = ?`
		args = append(args, mode)
	}
	query += ` ORDER BY day, id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: unscored calls for mode %q: %w", mode, err)
	}
	defer rows.Close()

	var out []Call
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: unscored calls for mode %q: %w", mode, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: unscored calls for mode %q: %w", mode, err)
	}
	return out, nil
}

func scanCall(sc scanner) (Call, error) {
	var (
		c         Call
		scoredAt  sql.NullString
		open      sql.NullFloat64
		close     sql.NullFloat64
		actualPct sql.NullFloat64
		correct   sql.NullInt64
	)
	err := sc.Scan(&c.ID, &c.BriefingID, &c.Mode, &c.Day, &c.Instrument, &c.Direction,
		&c.ThresholdPct, &c.Rationale, &scoredAt, &open, &close, &actualPct, &correct)
	if err != nil {
		return Call{}, err
	}
	c.Open = floatPtr(open)
	c.Close = floatPtr(close)
	c.ActualPct = floatPtr(actualPct)
	if scoredAt.Valid {
		t, err := parseTime(scoredAt.String)
		if err != nil {
			return Call{}, err
		}
		c.ScoredAt = &t
	}
	if correct.Valid {
		v := correct.Int64 == 1
		c.Correct = &v
	}
	return c, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
