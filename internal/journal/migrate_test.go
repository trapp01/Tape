package journal

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// openAtVersion builds a journal carrying only the first n migrations, so the
// next Open has real work to do.
func openAtVersion(t *testing.T, path string, n int) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer db.Close()

	ctx := context.Background()
	const createVersions = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(ctx, createVersions); err != nil {
		t.Fatalf("creating schema_migrations: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := applyMigration(ctx, db, i+1, migrations[i]); err != nil {
			t.Fatalf("applying migration %d: %v", i+1, err)
		}
	}
}

func TestUpgradeFromV4AddsTheMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")
	openAtVersion(t, path, 4)

	s, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("Open on a v4 journal: %v", err)
	}
	defer s.Close()

	var version int
	if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("reading schema version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("schema version = %d, want %d", version, len(migrations))
	}

	for _, table := range []string{"proposal_outcomes", "retros", "retro_diffs", "playbook_versions", "note_scores"} {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s is missing after the upgrade: %v", table, err)
		}
	}
	for _, index := range []string{
		"proposal_outcomes_proposal", "proposal_outcomes_mode_day",
		"retros_mode_generated", "retro_diffs_retro_idx",
		"playbook_versions_sha256", "note_scores_briefing_symbol", "note_scores_mode_day",
		"note_scores_mode_day_symbol",
	} {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
		if err != nil {
			t.Errorf("index %s is missing after the upgrade: %v", index, err)
		}
	}
}

func TestUpgradeFromV4KeepsExistingRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tape.db")
	openAtVersion(t, path, 4)

	before, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("Open at v4: %v", err)
	}
	briefingID := seedBriefing(t, before, ModePaper, proposalDay)
	p := newProposal(briefingID, ModePaper, proposalDay, 1, "NVDA")
	if err := before.InsertProposals(ctx, []*Proposal{p}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	if err := before.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	after, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer after.Close()

	got, err := after.ProposalsByBriefing(ctx, briefingID)
	if err != nil {
		t.Fatalf("ProposalsByBriefing: %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "NVDA" {
		t.Fatalf("proposals = %+v, want the NVDA row the v4 journal held", got)
	}

	// The new tables are usable against the records that were already there.
	if err := after.InsertProposalOutcome(ctx, newOutcome(p.ID, ModePaper, proposalDay)); err != nil {
		t.Fatalf("InsertProposalOutcome after the upgrade: %v", err)
	}
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")
	openAtVersion(t, path, len(migrations))

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	const ahead = `INSERT INTO schema_migrations (version, applied_at) VALUES (?, '2026-08-31T00:00:00.000000000Z')`
	if _, err := db.Exec(ahead, len(migrations)+1); err != nil {
		t.Fatalf("recording a future migration: %v", err)
	}
	db.Close()

	if _, err := Open(path, startingEquity); err == nil {
		t.Fatal("Open accepted a journal from a newer build, want an error")
	}
}
