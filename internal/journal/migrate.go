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
	schemaV2,
	schemaV3,
	schemaV4,
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

// schemaV2 archives the morning briefing and its one falsifiable call. input_json
// and output_json are the verbatim payloads, so a briefing can be re-read next to
// what actually happened.
var schemaV2 = []string{
	`CREATE TABLE briefings (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		mode          TEXT    NOT NULL,
		generated_at  TEXT    NOT NULL,
		day           TEXT    NOT NULL,
		provider      TEXT    NOT NULL DEFAULT '',
		model         TEXT    NOT NULL DEFAULT '',
		input_json    BLOB    NOT NULL,
		output_json   BLOB    NOT NULL,
		input_tokens  INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		cost_usd      REAL,
		latency_ms    INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE INDEX briefings_mode_day ON briefings(mode, day)`,
	`CREATE TABLE calls (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		briefing_id   INTEGER NOT NULL REFERENCES briefings(id) ON DELETE CASCADE,
		mode          TEXT    NOT NULL,
		day           TEXT    NOT NULL,
		instrument    TEXT    NOT NULL,
		direction     TEXT    NOT NULL,
		threshold_pct REAL    NOT NULL DEFAULT 0,
		rationale     TEXT    NOT NULL DEFAULT '',
		scored_at     TEXT,
		open          REAL,
		close         REAL,
		actual_pct    REAL,
		correct       INTEGER
	)`,
	`CREATE INDEX calls_mode_day ON calls(mode, day)`,
	// One call per briefing: the call of the day is the briefing's single graded claim.
	`CREATE UNIQUE INDEX calls_briefing ON calls(briefing_id)`,
}

// schemaV3 records the co-pilot: every trade idea the model produced, what the
// trader decided about it, and every time a guardrail said no. A proposal row is
// written whether or not it is taken, which is what makes the pass side scorable.
var schemaV3 = []string{
	`CREATE TABLE proposals (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		briefing_id  INTEGER NOT NULL REFERENCES briefings(id) ON DELETE CASCADE,
		mode         TEXT    NOT NULL,
		day          TEXT    NOT NULL,
		idx          INTEGER NOT NULL,
		symbol       TEXT    NOT NULL,
		side         TEXT    NOT NULL,
		setup_id     TEXT    NOT NULL DEFAULT '',
		entry        REAL    NOT NULL DEFAULT 0,
		stop         REAL    NOT NULL DEFAULT 0,
		target       REAL    NOT NULL DEFAULT 0,
		qty          INTEGER NOT NULL DEFAULT 0,
		risk_usd     REAL    NOT NULL DEFAULT 0,
		thesis       TEXT    NOT NULL DEFAULT '',
		invalidation TEXT    NOT NULL DEFAULT '',
		confidence   TEXT    NOT NULL DEFAULT '',
		status       TEXT    NOT NULL,
		reason       TEXT    NOT NULL DEFAULT '',
		decided_at   TEXT,
		order_id     INTEGER REFERENCES orders(id),
		created_at   TEXT    NOT NULL
	)`,
	// The index is the number the trader types in `tape take N`, so it addresses
	// exactly one idea within its briefing.
	`CREATE UNIQUE INDEX proposals_briefing_idx ON proposals(briefing_id, idx)`,
	`CREATE INDEX proposals_mode_day ON proposals(mode, day)`,
	`CREATE INDEX proposals_status ON proposals(status)`,
	`CREATE TABLE refusals (
		id     INTEGER PRIMARY KEY AUTOINCREMENT,
		mode   TEXT NOT NULL,
		day    TEXT NOT NULL,
		at     TEXT NOT NULL,
		rule   TEXT NOT NULL,
		symbol TEXT NOT NULL DEFAULT '',
		detail TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX refusals_mode_day ON refusals(mode, day)`,
	`ALTER TABLE orders ADD COLUMN proposal_id INTEGER`,
	`CREATE INDEX orders_proposal ON orders(proposal_id)`,
}

// schemaV4 records what a taken proposal actually traded, which `take --qty`
// can lower below the sized quantity, and links a bracket leg to the order it
// protects so a one-cancels-other pair is not counted as two claims.
var schemaV4 = []string{
	`ALTER TABLE proposals ADD COLUMN taken_qty INTEGER`,
	`ALTER TABLE proposals ADD COLUMN taken_risk_usd REAL`,
	`ALTER TABLE orders ADD COLUMN parent_order_id TEXT NOT NULL DEFAULT ''`,
	`CREATE INDEX orders_parent ON orders(parent_order_id) WHERE parent_order_id <> ''`,
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
