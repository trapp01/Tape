package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const retroColumns = `id, mode, generated_at, from_day, to_day, provider, model,
	input_json, output_json, input_tokens, output_tokens, cost_usd, latency_ms`

const retroDiffColumns = `id, retro_id, idx, section, change, rationale,
	before_text, after_text, applied_at, version_id`

// InsertRetro writes a weekly review and its proposed playbook edits in one
// transaction, numbering the diffs 1..n and filling in every ID.
func (s *Store) InsertRetro(ctx context.Context, r *Retro, diffs []*RetroDiff) error {
	if r == nil {
		return errors.New("journal: insert retro: nil retro")
	}
	if err := validateRetro(r, diffs); err != nil {
		return err
	}
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: insert retro for %s %s..%s: %w", r.Mode, r.FromDay, r.ToDay, err)
	}
	defer tx.Rollback()

	const insertRetro = `INSERT INTO retros (
		mode, generated_at, from_day, to_day, provider, model,
		input_json, output_json, input_tokens, output_tokens, cost_usd, latency_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := tx.ExecContext(ctx, insertRetro,
		r.Mode, formatTime(r.GeneratedAt), r.FromDay, r.ToDay, r.Provider, r.Model,
		r.InputJSON, r.OutputJSON, r.InputTokens, r.OutputTokens, nullFloat(r.CostUSD), r.LatencyMs)
	if err != nil {
		return fmt.Errorf("journal: insert retro for %s %s..%s: %w", r.Mode, r.FromDay, r.ToDay, err)
	}
	retroID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("journal: insert retro for %s %s..%s: reading id: %w", r.Mode, r.FromDay, r.ToDay, err)
	}

	const insertDiff = `INSERT INTO retro_diffs (
		retro_id, idx, section, change, rationale, before_text, after_text
	) VALUES (?, ?, ?, ?, ?, ?, ?)`

	ids := make([]int64, len(diffs))
	for i, d := range diffs {
		res, err := tx.ExecContext(ctx, insertDiff,
			retroID, i+1, d.Section, d.Change, d.Rationale, d.Before, d.After)
		if err != nil {
			return fmt.Errorf("journal: insert retro diff %d (%s): %w", i+1, d.Section, err)
		}
		if ids[i], err = res.LastInsertId(); err != nil {
			return fmt.Errorf("journal: insert retro diff %d (%s): reading id: %w", i+1, d.Section, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: insert retro for %s %s..%s: %w", r.Mode, r.FromDay, r.ToDay, err)
	}

	r.ID = retroID
	for i, d := range diffs {
		d.ID = ids[i]
		d.RetroID = retroID
		d.Index = i + 1
	}
	return nil
}

func validateRetro(r *Retro, diffs []*RetroDiff) error {
	if r.Mode != ModePaper && r.Mode != ModeLive {
		return fmt.Errorf("journal: insert retro: mode must be %q or %q, got %q", ModePaper, ModeLive, r.Mode)
	}
	if err := validateDayRange(fmt.Sprintf("insert retro for %s", r.Mode), r.FromDay, r.ToDay); err != nil {
		return err
	}
	if len(r.InputJSON) == 0 {
		return fmt.Errorf("journal: insert retro for %s %s..%s: input json is empty", r.Mode, r.FromDay, r.ToDay)
	}
	if len(r.OutputJSON) == 0 {
		return fmt.Errorf("journal: insert retro for %s %s..%s: output json is empty", r.Mode, r.FromDay, r.ToDay)
	}
	for i, d := range diffs {
		if d == nil {
			return fmt.Errorf("journal: insert retro for %s %s..%s: diff %d is nil", r.Mode, r.FromDay, r.ToDay, i+1)
		}
	}
	return nil
}

// RetroByID returns one review, or ErrNotFound.
func (s *Store) RetroByID(ctx context.Context, id int64) (Retro, error) {
	query := `SELECT ` + retroColumns + ` FROM retros WHERE id = ?`
	r, err := scanRetro(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Retro{}, fmt.Errorf("journal: retro %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Retro{}, fmt.Errorf("journal: retro %d: %w", id, err)
	}
	return r, nil
}

// ListRetros returns reviews newest first. An empty mode covers both paper and
// live; a limit of zero or less means no cap.
func (s *Store) ListRetros(ctx context.Context, mode string, limit int) ([]Retro, error) {
	query := `SELECT ` + retroColumns + ` FROM retros`
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
	return queryList(ctx, s, fmt.Sprintf("list retros for mode %q", mode), query, args, scanRetro)
}

// DiffsByRetro returns a review's proposed edits in the order it made them.
func (s *Store) DiffsByRetro(ctx context.Context, retroID int64) ([]RetroDiff, error) {
	query := `SELECT ` + retroDiffColumns + ` FROM retro_diffs WHERE retro_id = ? ORDER BY idx`
	return queryList(ctx, s, fmt.Sprintf("diffs for retro %d", retroID), query, []any{retroID}, scanRetroDiff)
}

func scanRetro(sc scanner) (Retro, error) {
	var (
		r           Retro
		costUSD     sql.NullFloat64
		generatedAt string
	)
	err := sc.Scan(&r.ID, &r.Mode, &generatedAt, &r.FromDay, &r.ToDay, &r.Provider, &r.Model,
		&r.InputJSON, &r.OutputJSON, &r.InputTokens, &r.OutputTokens, &costUSD, &r.LatencyMs)
	if err != nil {
		return Retro{}, err
	}
	r.CostUSD = floatPtr(costUSD)
	if r.GeneratedAt, err = parseTime(generatedAt); err != nil {
		return Retro{}, err
	}
	return r, nil
}

func scanRetroDiff(sc scanner) (RetroDiff, error) {
	var (
		d         RetroDiff
		appliedAt sql.NullString
		versionID sql.NullInt64
	)
	err := sc.Scan(&d.ID, &d.RetroID, &d.Index, &d.Section, &d.Change, &d.Rationale,
		&d.Before, &d.After, &appliedAt, &versionID)
	if err != nil {
		return RetroDiff{}, err
	}
	d.VersionID = intPtr(versionID)
	if d.AppliedAt, err = timePtr(appliedAt); err != nil {
		return RetroDiff{}, err
	}
	return d, nil
}
