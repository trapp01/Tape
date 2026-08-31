package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// rowQuerier is whatever can read one row, so a refusal can be explained from
// inside the transaction that hit it.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const markDiff = `UPDATE retro_diffs SET applied_at = ?, version_id = ?
	WHERE id = ? AND applied_at IS NULL`

// ApplyRetroDiffs files the playbook snapshot an apply produced and marks every
// diff that produced it, in one transaction. A diff the playbook already carries
// fails the whole apply, so one edit can never land twice or claim two versions.
func (s *Store) ApplyRetroDiffs(ctx context.Context, v *PlaybookVersion, diffIDs []int64, at time.Time) error {
	if v == nil {
		return errors.New("journal: apply retro diffs: nil version")
	}
	if len(diffIDs) == 0 {
		return errors.New("journal: apply retro diffs: no diffs chosen")
	}
	if err := validatePlaybookVersion(v); err != nil {
		return err
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: apply retro diffs: %w", err)
	}
	defer tx.Rollback()

	id, err := insertPlaybookVersionTx(ctx, tx, v)
	if err != nil {
		return err
	}
	for _, diffID := range diffIDs {
		if diffID <= 0 {
			return fmt.Errorf("journal: apply retro diff: id must be positive, got %d", diffID)
		}
		res, err := tx.ExecContext(ctx, markDiff, formatTime(at), id, diffID)
		if err != nil {
			return fmt.Errorf("journal: apply retro diff %d: %w", diffID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("journal: apply retro diff %d: counting affected rows: %w", diffID, err)
		}
		if n == 0 {
			return appliedRefusal(ctx, tx, diffID)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: apply retro diffs: %w", err)
	}
	v.ID = id
	return nil
}

// MarkDiffApplied records that the trader accepted one edit and names the
// playbook snapshot it produced. A diff is applied once; the second attempt is
// refused so one edit cannot claim two versions.
func (s *Store) MarkDiffApplied(ctx context.Context, diffID, versionID int64, at time.Time) error {
	if diffID <= 0 {
		return fmt.Errorf("journal: apply retro diff: id must be positive, got %d", diffID)
	}
	if versionID <= 0 {
		return fmt.Errorf("journal: apply retro diff %d: playbook version id must be positive, got %d", diffID, versionID)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	res, err := s.db.ExecContext(ctx, markDiff, formatTime(at), versionID, diffID)
	if err != nil {
		return fmt.Errorf("journal: apply retro diff %d: %w", diffID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("journal: apply retro diff %d: counting affected rows: %w", diffID, err)
	}
	if n == 0 {
		return appliedRefusal(ctx, s.db, diffID)
	}
	return nil
}

// appliedRefusal names why an update guarded on applied_at matched nothing: no
// such diff, or one the playbook already carries.
func appliedRefusal(ctx context.Context, q rowQuerier, diffID int64) error {
	var appliedAt sql.NullString
	err := q.QueryRowContext(ctx, `SELECT applied_at FROM retro_diffs WHERE id = ?`, diffID).Scan(&appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("journal: apply retro diff %d: %w", diffID, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("journal: apply retro diff %d: %w", diffID, err)
	}
	return fmt.Errorf("journal: apply retro diff %d: already applied at %s; an edit lands once", diffID, appliedAt.String)
}
