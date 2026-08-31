package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const playbookVersionColumns = `id, created_at, sha256, path, retro_id, note, config_hash`

const insertPlaybookVersion = `INSERT INTO playbook_versions (created_at, sha256, path, retro_id, note, config_hash)
	VALUES (?, ?, ?, ?, ?, ?)`

// execer is whatever can run one statement, so a snapshot files the same way
// inside a transaction as outside one.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// InsertPlaybookVersion snapshots the rules in force and fills in the ID.
// CreatedAt defaults to now when zero.
func (s *Store) InsertPlaybookVersion(ctx context.Context, v *PlaybookVersion) error {
	if v == nil {
		return errors.New("journal: insert playbook version: nil version")
	}
	if err := validatePlaybookVersion(v); err != nil {
		return err
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	id, err := insertPlaybookVersionTx(ctx, s.db, v)
	if err != nil {
		return err
	}
	v.ID = id
	return nil
}

func validatePlaybookVersion(v *PlaybookVersion) error {
	if strings.TrimSpace(v.SHA256) == "" {
		return errors.New("journal: insert playbook version: sha256 is empty")
	}
	if v.RetroID != nil && *v.RetroID <= 0 {
		return fmt.Errorf("journal: insert playbook version %s: retro id must be positive, got %d", v.SHA256, *v.RetroID)
	}
	return nil
}

// insertPlaybookVersionTx writes the row and returns its id without touching v,
// so a caller inside a transaction only claims the id once it commits.
func insertPlaybookVersionTx(ctx context.Context, q execer, v *PlaybookVersion) (int64, error) {
	res, err := q.ExecContext(ctx, insertPlaybookVersion,
		formatTime(v.CreatedAt), v.SHA256, v.Path, nullInt(v.RetroID), v.Note, v.ConfigHash)
	if err != nil {
		return 0, fmt.Errorf("journal: insert playbook version %s: %w", v.SHA256, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("journal: insert playbook version %s: reading id: %w", v.SHA256, err)
	}
	return id, nil
}

// LatestPlaybookVersion returns the newest snapshot, or ErrNotFound when the
// playbook has never been versioned.
func (s *Store) LatestPlaybookVersion(ctx context.Context) (PlaybookVersion, error) {
	query := `SELECT ` + playbookVersionColumns + ` FROM playbook_versions ORDER BY created_at DESC, id DESC LIMIT 1`
	v, err := scanPlaybookVersion(s.db.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return PlaybookVersion{}, fmt.Errorf("journal: latest playbook version: %w", ErrNotFound)
	}
	if err != nil {
		return PlaybookVersion{}, fmt.Errorf("journal: latest playbook version: %w", err)
	}
	return v, nil
}

// ListPlaybookVersions returns snapshots newest first. A limit of zero or less
// means no cap.
func (s *Store) ListPlaybookVersions(ctx context.Context, limit int) ([]PlaybookVersion, error) {
	query := `SELECT ` + playbookVersionColumns + ` FROM playbook_versions ORDER BY created_at DESC, id DESC`
	var args []any
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	return queryList(ctx, s, "list playbook versions", query, args, scanPlaybookVersion)
}

func scanPlaybookVersion(sc scanner) (PlaybookVersion, error) {
	var (
		v         PlaybookVersion
		retroID   sql.NullInt64
		createdAt string
	)
	err := sc.Scan(&v.ID, &createdAt, &v.SHA256, &v.Path, &retroID, &v.Note, &v.ConfigHash)
	if err != nil {
		return PlaybookVersion{}, err
	}
	v.RetroID = intPtr(retroID)
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return PlaybookVersion{}, err
	}
	return v, nil
}
