package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ScoreCall grades a call against the session it was made for. A call is graded
// once: re-scoring is refused so the record cannot be rewritten after the fact.
func (s *Store) ScoreCall(ctx context.Context, id int64, open, close, actualPct float64, correct bool, at time.Time) error {
	if id <= 0 {
		return fmt.Errorf("journal: score call: id must be positive, got %d", id)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	const update = `UPDATE calls
		SET scored_at = ?, open = ?, close = ?, actual_pct = ?, correct = ?
		WHERE id = ? AND scored_at IS NULL`

	res, err := s.db.ExecContext(ctx, update, formatTime(at), open, close, actualPct, boolToInt(correct), id)
	if err != nil {
		return fmt.Errorf("journal: score call %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: score call %d: counting affected rows: %w", id, err)
	}
	if n == 0 {
		return s.unscoredRefusal(ctx, "score", id)
	}
	return nil
}

// unscoredRefusal names why an update guarded on scored_at matched nothing: no
// such call, or one that already carries a score.
func (s *Store) unscoredRefusal(ctx context.Context, verb string, id int64) error {
	var scoredAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT scored_at FROM calls WHERE id = ?`, id).Scan(&scoredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("journal: %s call %d: %w", verb, id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("journal: %s call %d: %w", verb, id, err)
	}
	return fmt.Errorf("journal: %s call %d: already scored at %s; a call is graded once", verb, id, scoredAt.String)
}

// CallAccuracy counts scored calls in [fromDay, toDay] and how many were right.
// An empty mode covers both; an empty fromDay or toDay drops that bound.
func (s *Store) CallAccuracy(ctx context.Context, mode, fromDay, toDay string) (correct, total int, err error) {
	query := `SELECT COUNT(*), COALESCE(SUM(CASE WHEN correct = 1 THEN 1 ELSE 0 END), 0)
		FROM calls WHERE scored_at IS NOT NULL`
	var where []string
	var args []any

	if mode != "" {
		where = append(where, "mode = ?")
		args = append(args, mode)
	}
	if fromDay != "" {
		if err := validateDay(fromDay); err != nil {
			return 0, 0, fmt.Errorf("journal: call accuracy: from %w", err)
		}
		where = append(where, "day >= ?")
		args = append(args, fromDay)
	}
	if toDay != "" {
		if err := validateDay(toDay); err != nil {
			return 0, 0, fmt.Errorf("journal: call accuracy: to %w", err)
		}
		where = append(where, "day <= ?")
		args = append(args, toDay)
	}
	if len(where) > 0 {
		query += " AND " + strings.Join(where, " AND ")
	}

	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total, &correct); err != nil {
		return 0, 0, fmt.Errorf("journal: call accuracy for mode %q: %w", mode, err)
	}
	return correct, total, nil
}
