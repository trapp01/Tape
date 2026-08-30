package journal

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrations are applied in order; the slice index plus one is the version
// recorded in schema_migrations. Append only. Editing an applied entry leaves
// existing journals on a different schema than fresh ones.
var migrations = [][]string{
	schemaV1,
}

// schemaV1 is the order and fill record every stat is derived from.
var schemaV1 = []string{
	`CREATE TABLE meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`CREATE TABLE orders (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		broker_order_id  TEXT    NOT NULL DEFAULT '',
		client_order_id  TEXT    NOT NULL DEFAULT '',
		symbol           TEXT    NOT NULL,
		side             TEXT    NOT NULL,
		qty              INTEGER NOT NULL,
		type             TEXT    NOT NULL DEFAULT '',
		limit_price      REAL,
		stop_loss        REAL,
		take_profit      REAL,
		status           TEXT    NOT NULL DEFAULT '',
		filled_qty       INTEGER NOT NULL DEFAULT 0,
		filled_avg_price REAL,
		source           TEXT    NOT NULL DEFAULT '',
		mode             TEXT    NOT NULL,
		note             TEXT    NOT NULL DEFAULT '',
		submitted_at     TEXT    NOT NULL,
		updated_at       TEXT    NOT NULL
	)`,
	// Partial unique indexes: a locally created order has no venue id until the
	// broker accepts it, and every such row would collide on an empty string.
	`CREATE UNIQUE INDEX orders_broker_order_id ON orders(broker_order_id) WHERE broker_order_id <> ''`,
	`CREATE UNIQUE INDEX orders_client_order_id ON orders(client_order_id) WHERE client_order_id <> ''`,
	`CREATE INDEX orders_mode_submitted ON orders(mode, submitted_at)`,
	`CREATE TABLE fills (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id       INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
		broker_fill_id TEXT    NOT NULL DEFAULT '',
		symbol         TEXT    NOT NULL,
		side           TEXT    NOT NULL,
		qty            INTEGER NOT NULL,
		raw_price      REAL    NOT NULL,
		modeled_price  REAL    NOT NULL,
		commission     REAL    NOT NULL DEFAULT 0,
		fees           REAL    NOT NULL DEFAULT 0,
		filled_at      TEXT    NOT NULL
	)`,
	`CREATE UNIQUE INDEX fills_broker_fill_id ON fills(broker_fill_id) WHERE broker_fill_id <> ''`,
	`CREATE INDEX fills_order ON fills(order_id)`,
	`CREATE INDEX fills_symbol_filled_at ON fills(symbol, filled_at)`,
}

// migrate brings db up to the newest schema version, applying each pending
// migration in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	const createVersions = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(ctx, createVersions); err != nil {
		return fmt.Errorf("journal: creating schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("journal: reading schema version: %w", err)
	}
	if current > len(migrations) {
		return fmt.Errorf("journal: database is at schema version %d but this build only knows version %d; upgrade tape", current, len(migrations))
	}

	for i := current; i < len(migrations); i++ {
		if err := applyMigration(ctx, db, i+1, migrations[i]); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, stmts []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("journal: starting migration %d: %w", version, err)
	}
	defer tx.Rollback()

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("journal: applying migration %d: %w", version, err)
		}
	}
	const record = `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`
	if _, err := tx.ExecContext(ctx, record, version, formatTime(time.Now())); err != nil {
		return fmt.Errorf("journal: recording migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal: committing migration %d: %w", version, err)
	}
	return nil
}
