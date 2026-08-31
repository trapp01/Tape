package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned by every lookup that addresses a single record.
var ErrNotFound = errors.New("record not found")

// timeLayout is fixed-width and always UTC, so TEXT timestamps compare and sort
// chronologically inside SQLite.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

const metaStartingEquity = "starting_equity"

// Store is the journal database. Every method is safe to call concurrently;
// SQLite serialises the writes.
type Store struct {
	db             *sql.DB
	path           string
	startingEquity float64
}

// Open creates or opens the journal at path and migrates it to the current
// schema. startingEquity is recorded on first open and must match on every open
// after that, because all cash and return figures are measured against it.
func Open(path string, startingEquity float64) (*Store, error) {
	if path == "" {
		return nil, errors.New("journal: open: empty database path")
	}
	if startingEquity <= 0 {
		return nil, fmt.Errorf("journal: open %s: starting equity must be positive, got %v", path, startingEquity)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("journal: resolving database path %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("journal: creating database directory for %s: %w", abs, err)
	}

	db, err := sql.Open("sqlite", dsn(abs))
	if err != nil {
		return nil, fmt.Errorf("journal: opening %s: %w", abs, err)
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("journal: opening %s: %w", abs, err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db, path: abs, startingEquity: startingEquity}
	if err := s.reconcileStartingEquity(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// dsn builds the file URI. WAL keeps a reader from blocking the writer, foreign
// keys are off by default in SQLite, and the busy timeout absorbs a concurrent
// `tape` process instead of failing the command.
func dsn(abs string) string {
	u := url.URL{
		Scheme:   "file",
		Path:     abs,
		RawQuery: "_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000&_txlock=immediate",
	}
	return u.String()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("journal: closing %s: %w", s.path, err)
	}
	return nil
}

// Path is the absolute location of the database file.
func (s *Store) Path() string { return s.path }

// StartingEquity is the ledger size the journal was created with.
func (s *Store) StartingEquity() float64 { return s.startingEquity }

func (s *Store) reconcileStartingEquity(ctx context.Context) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaStartingEquity).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		const insert = `INSERT INTO meta (key, value) VALUES (?, ?)`
		value := strconv.FormatFloat(s.startingEquity, 'f', -1, 64)
		if _, err := s.db.ExecContext(ctx, insert, metaStartingEquity, value); err != nil {
			return fmt.Errorf("journal: recording starting equity in %s: %w", s.path, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("journal: reading starting equity from %s: %w", s.path, err)
	}

	stored, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("journal: starting equity %q in %s is not a number: %w", raw, s.path, err)
	}
	if math.Abs(stored-s.startingEquity) > 1e-9 {
		return fmt.Errorf("journal: %s was created with starting equity %g but the config says %g; "+
			"cash, returns and drawdown are all measured against the original figure, so mixing the two "+
			"would make every stat wrong. Set account.starting_equity back to %g, or delete %s to start a fresh journal",
			s.path, stored, s.startingEquity, stored, s.path)
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("journal: parsing timestamp %q: %w", s, err)
	}
	return t, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func nullFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func nullQty(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullTime(p *time.Time) any {
	if p == nil {
		return nil
	}
	return formatTime(*p)
}

func timePtr(n sql.NullString) (*time.Time, error) {
	if !n.Valid {
		return nil, nil
	}
	t, err := parseTime(n.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
