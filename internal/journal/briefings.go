package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// dayLayout is the local calendar day a briefing and its call are filed under.
// Fixed width, so day ranges compare as strings inside SQLite.
const dayLayout = "2006-01-02"

const briefingColumns = `id, mode, generated_at, day, provider, model,
	input_json, output_json, input_tokens, output_tokens, cost_usd, latency_ms`

// InsertBriefing writes b and fills in its ID. GeneratedAt defaults to now when zero.
func (s *Store) InsertBriefing(ctx context.Context, b *Briefing) error {
	if b == nil {
		return errors.New("journal: insert briefing: nil briefing")
	}
	if err := validateBriefing(b); err != nil {
		return err
	}
	if b.GeneratedAt.IsZero() {
		b.GeneratedAt = time.Now().UTC()
	}

	const insert = `INSERT INTO briefings (
		mode, generated_at, day, provider, model,
		input_json, output_json, input_tokens, output_tokens, cost_usd, latency_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, insert,
		b.Mode, formatTime(b.GeneratedAt), b.Day, b.Provider, b.Model,
		b.InputJSON, b.OutputJSON, b.InputTokens, b.OutputTokens, nullFloat(b.CostUSD), b.LatencyMs)
	if err != nil {
		return fmt.Errorf("journal: insert briefing for %s %s: %w", b.Mode, b.Day, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert briefing for %s %s: reading id: %w", b.Mode, b.Day, err)
	}
	b.ID = id
	return nil
}

func validateBriefing(b *Briefing) error {
	if b.Mode != ModePaper && b.Mode != ModeLive {
		return fmt.Errorf("journal: insert briefing: mode must be %q or %q, got %q", ModePaper, ModeLive, b.Mode)
	}
	if err := validateDay(b.Day); err != nil {
		return fmt.Errorf("journal: insert briefing for %s: %w", b.Mode, err)
	}
	if len(b.InputJSON) == 0 {
		return fmt.Errorf("journal: insert briefing for %s %s: input json is empty", b.Mode, b.Day)
	}
	if len(b.OutputJSON) == 0 {
		return fmt.Errorf("journal: insert briefing for %s %s: output json is empty", b.Mode, b.Day)
	}
	return nil
}

// validateDay keeps day columns in one fixed-width layout; range queries compare
// them as strings.
func validateDay(day string) error {
	if day == "" {
		return errors.New("day is empty")
	}
	if _, err := time.Parse(dayLayout, day); err != nil {
		return fmt.Errorf("day %q is not %s", day, dayLayout)
	}
	return nil
}

// BriefingByID returns one briefing, or ErrNotFound.
func (s *Store) BriefingByID(ctx context.Context, id int64) (Briefing, error) {
	query := `SELECT ` + briefingColumns + ` FROM briefings WHERE id = ?`
	b, err := scanBriefing(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Briefing{}, fmt.Errorf("journal: briefing %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Briefing{}, fmt.Errorf("journal: briefing %d: %w", id, err)
	}
	return b, nil
}

// LatestBriefing returns the most recent briefing written for day in mode, or
// ErrNotFound. A rerun is a new row, so this is the one the reader means.
func (s *Store) LatestBriefing(ctx context.Context, mode, day string) (Briefing, error) {
	query := `SELECT ` + briefingColumns + ` FROM briefings
		WHERE mode = ? AND day = ? ORDER BY generated_at DESC, id DESC LIMIT 1`
	b, err := scanBriefing(s.db.QueryRowContext(ctx, query, mode, day))
	if errors.Is(err, sql.ErrNoRows) {
		return Briefing{}, fmt.Errorf("journal: latest briefing for %s %s: %w", mode, day, ErrNotFound)
	}
	if err != nil {
		return Briefing{}, fmt.Errorf("journal: latest briefing for %s %s: %w", mode, day, err)
	}
	return b, nil
}

// ListBriefings returns briefings newest first. An empty mode covers both paper
// and live; a limit of zero or less means no cap.
func (s *Store) ListBriefings(ctx context.Context, mode string, limit int) ([]Briefing, error) {
	query := `SELECT ` + briefingColumns + ` FROM briefings`
	var args []any
	if mode != "" {
		query += ` WHERE mode = ?`
		args = append(args, mode)
	}
	query += ` ORDER BY generated_at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("journal: list briefings for mode %q: %w", mode, err)
	}
	defer rows.Close()

	var out []Briefing
	for rows.Next() {
		b, err := scanBriefing(rows)
		if err != nil {
			return nil, fmt.Errorf("journal: list briefings for mode %q: %w", mode, err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("journal: list briefings for mode %q: %w", mode, err)
	}
	return out, nil
}

func scanBriefing(sc scanner) (Briefing, error) {
	var (
		b           Briefing
		costUSD     sql.NullFloat64
		generatedAt string
	)
	err := sc.Scan(&b.ID, &b.Mode, &generatedAt, &b.Day, &b.Provider, &b.Model,
		&b.InputJSON, &b.OutputJSON, &b.InputTokens, &b.OutputTokens, &costUSD, &b.LatencyMs)
	if err != nil {
		return Briefing{}, err
	}
	b.CostUSD = floatPtr(costUSD)
	if b.GeneratedAt, err = parseTime(generatedAt); err != nil {
		return Briefing{}, err
	}
	return b, nil
}
