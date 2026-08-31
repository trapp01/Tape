package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const refusalColumns = `id, mode, day, at, rule, symbol, detail, source`

// InsertRefusal records a guardrail saying no and fills in the ID. The count of
// these over a week is a fact about the strategy, not noise: a rule that never
// fires is not protecting anything, and one that fires daily is the plan.
func (s *Store) InsertRefusal(ctx context.Context, r *Refusal) error {
	if r == nil {
		return errors.New("journal: insert refusal: nil refusal")
	}
	if r.Mode != ModePaper && r.Mode != ModeLive {
		return fmt.Errorf("journal: insert refusal: mode must be %q or %q, got %q", ModePaper, ModeLive, r.Mode)
	}
	if err := validateDay(r.Day); err != nil {
		return fmt.Errorf("journal: insert refusal for %s: %w", r.Mode, err)
	}
	if strings.TrimSpace(r.Rule) == "" {
		return fmt.Errorf("journal: insert refusal for %s %s: rule is empty", r.Mode, r.Day)
	}
	if r.At.IsZero() {
		r.At = time.Now().UTC()
	}

	const insert = `INSERT INTO refusals (mode, day, at, rule, symbol, detail, source)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, insert,
		r.Mode, r.Day, formatTime(r.At), r.Rule, r.Symbol, r.Detail, r.Source)
	if err != nil {
		return fmt.Errorf("journal: insert refusal %s for %s %s: %w", r.Rule, r.Mode, r.Day, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert refusal %s for %s %s: reading id: %w", r.Rule, r.Mode, r.Day, err)
	}
	r.ID = id
	return nil
}

// RefusalsForDay returns a session's refusals, oldest first.
func (s *Store) RefusalsForDay(ctx context.Context, mode, day string) ([]Refusal, error) {
	if err := validateDay(day); err != nil {
		return nil, fmt.Errorf("journal: refusals for day: %w", err)
	}
	query := `SELECT ` + refusalColumns + ` FROM refusals WHERE mode = ? AND day = ? ORDER BY at, id`

	rows, err := s.db.QueryContext(ctx, query, mode, day)
	if err != nil {
		return nil, fmt.Errorf("journal: refusals for %s %s: %w", mode, day, err)
	}
	defer rows.Close()

	var out []Refusal
	for rows.Next() {
		r, err := scanRefusal(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: refusals for %s %s: %w", mode, day, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: refusals for %s %s: %w", mode, day, err)
	}
	return out, nil
}

// RefusalCount counts refusals in [fromDay, toDay]. An empty mode covers both;
// an empty bound drops that end of the range.
func (s *Store) RefusalCount(ctx context.Context, mode, fromDay, toDay string) (int, error) {
	query := `SELECT COUNT(*) FROM refusals`
	var where []string
	var args []any

	if mode != "" {
		where = append(where, "mode = ?")
		args = append(args, mode)
	}
	if fromDay != "" {
		if err := validateDay(fromDay); err != nil {
			return 0, fmt.Errorf("journal: refusal count: from %w", err)
		}
		where = append(where, "day >= ?")
		args = append(args, fromDay)
	}
	if toDay != "" {
		if err := validateDay(toDay); err != nil {
			return 0, fmt.Errorf("journal: refusal count: to %w", err)
		}
		where = append(where, "day <= ?")
		args = append(args, toDay)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var n int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("journal: refusal count for mode %q: %w", mode, err)
	}
	return n, nil
}

func scanRefusal(sc scanner) (Refusal, error) {
	var (
		r  Refusal
		at string
	)
	if err := sc.Scan(&r.ID, &r.Mode, &r.Day, &at, &r.Rule, &r.Symbol, &r.Detail, &r.Source); err != nil {
		return Refusal{}, err
	}
	var err error
	if r.At, err = parseTime(at); err != nil {
		return Refusal{}, err
	}
	return r, nil
}
