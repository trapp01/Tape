package journal

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newBriefing(mode, day string, at time.Time) *Briefing {
	cost := 0.0123
	return &Briefing{
		Mode:         mode,
		GeneratedAt:  at,
		Day:          day,
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-5",
		InputJSON:    []byte(`{"watchlist":["SPY"]}`),
		OutputJSON:   []byte(`{"market_read":"quiet"}`),
		InputTokens:  1200,
		OutputTokens: 340,
		CostUSD:      &cost,
		LatencyMs:    4210,
	}
}

// insertBriefingWithCall files a briefing and its call of the day.
func insertBriefingWithCall(t *testing.T, s *Store, mode, day, instrument, direction string, at time.Time) (Briefing, Call) {
	t.Helper()
	ctx := context.Background()
	b := newBriefing(mode, day, at)
	if err := s.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("InsertBriefing %s %s: %v", mode, day, err)
	}
	c := &Call{
		BriefingID:   b.ID,
		Mode:         mode,
		Day:          day,
		Instrument:   instrument,
		Direction:    direction,
		ThresholdPct: 0.3,
		Rationale:    "M2 continuation above yesterday's high",
	}
	if err := s.InsertCall(ctx, c); err != nil {
		t.Fatalf("InsertCall %s %s: %v", mode, day, err)
	}
	return *b, *c
}

func TestInsertAndFetchBriefing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b := newBriefing(ModePaper, "2026-08-24", at)
	if err := s.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("InsertBriefing left ID at zero")
	}

	got, err := s.BriefingByID(ctx, b.ID)
	if err != nil {
		t.Fatalf("BriefingByID: %v", err)
	}
	if !got.GeneratedAt.Equal(at) {
		t.Errorf("GeneratedAt = %v, want %v", got.GeneratedAt, at)
	}
	if string(got.InputJSON) != string(b.InputJSON) || string(got.OutputJSON) != string(b.OutputJSON) {
		t.Errorf("payloads round-tripped as %q / %q", got.InputJSON, got.OutputJSON)
	}
	if got.Provider != "anthropic" || got.Model != "claude-sonnet-4-5" {
		t.Errorf("provider/model = %q/%q", got.Provider, got.Model)
	}
	if got.InputTokens != 1200 || got.OutputTokens != 340 || got.LatencyMs != 4210 {
		t.Errorf("tokens/latency = %d/%d/%d", got.InputTokens, got.OutputTokens, got.LatencyMs)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", got.CostUSD)
	}

	if _, err := s.BriefingByID(ctx, b.ID+99); !errors.Is(err, ErrNotFound) {
		t.Errorf("BriefingByID missing: %v, want ErrNotFound", err)
	}
}

func TestInsertBriefingDefaultsAndRejects(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	b := newBriefing(ModePaper, "2026-08-24", time.Time{})
	if err := s.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	if b.GeneratedAt.IsZero() {
		t.Error("InsertBriefing left GeneratedAt at zero")
	}

	tests := []struct {
		name  string
		mutit func(*Briefing)
	}{
		{"bad mode", func(b *Briefing) { b.Mode = "sim" }},
		{"no mode", func(b *Briefing) { b.Mode = "" }},
		{"no day", func(b *Briefing) { b.Day = "" }},
		{"malformed day", func(b *Briefing) { b.Day = "24/08/2026" }},
		{"no input", func(b *Briefing) { b.InputJSON = nil }},
		{"no output", func(b *Briefing) { b.OutputJSON = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad := newBriefing(ModePaper, "2026-08-24", time.Time{})
			tc.mutit(bad)
			if err := s.InsertBriefing(ctx, bad); err == nil {
				t.Fatal("InsertBriefing succeeded, want an error")
			}
		})
	}
	if err := s.InsertBriefing(ctx, nil); err == nil {
		t.Error("InsertBriefing(nil) succeeded, want an error")
	}
}

func TestLatestBriefingTakesTheRerun(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := "2026-08-24"
	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	first := newBriefing(ModePaper, day, at)
	if err := s.InsertBriefing(ctx, first); err != nil {
		t.Fatalf("InsertBriefing first: %v", err)
	}
	second := newBriefing(ModePaper, day, at.Add(20*time.Minute))
	second.Model = "rerun"
	if err := s.InsertBriefing(ctx, second); err != nil {
		t.Fatalf("InsertBriefing second: %v", err)
	}

	got, err := s.LatestBriefing(ctx, ModePaper, day)
	if err != nil {
		t.Fatalf("LatestBriefing: %v", err)
	}
	if got.ID != second.ID || got.Model != "rerun" {
		t.Errorf("LatestBriefing returned id %d model %q, want %d rerun", got.ID, got.Model, second.ID)
	}

	if _, err := s.LatestBriefing(ctx, ModePaper, "2026-08-25"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestBriefing for an empty day: %v, want ErrNotFound", err)
	}
	if _, err := s.LatestBriefing(ctx, ModeLive, day); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestBriefing live: %v, want ErrNotFound", err)
	}
}

func TestListBriefingsNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 20, 12, 52, 0, 0, time.UTC)
	days := []string{"2026-08-20", "2026-08-21", "2026-08-24"}
	for i, day := range days {
		if err := s.InsertBriefing(ctx, newBriefing(ModePaper, day, at.AddDate(0, 0, i))); err != nil {
			t.Fatalf("InsertBriefing %s: %v", day, err)
		}
	}
	if err := s.InsertBriefing(ctx, newBriefing(ModeLive, "2026-08-25", at.AddDate(0, 0, 5))); err != nil {
		t.Fatalf("InsertBriefing live: %v", err)
	}

	paper, err := s.ListBriefings(ctx, ModePaper, 0)
	if err != nil {
		t.Fatalf("ListBriefings: %v", err)
	}
	if len(paper) != 3 {
		t.Fatalf("got %d paper briefings, want 3", len(paper))
	}
	if paper[0].Day != "2026-08-24" || paper[2].Day != "2026-08-20" {
		t.Errorf("order = %s..%s, want newest first", paper[0].Day, paper[2].Day)
	}

	both, err := s.ListBriefings(ctx, "", 0)
	if err != nil {
		t.Fatalf("ListBriefings both: %v", err)
	}
	if len(both) != 4 {
		t.Errorf("got %d briefings across modes, want 4", len(both))
	}

	limited, err := s.ListBriefings(ctx, ModePaper, 2)
	if err != nil {
		t.Fatalf("ListBriefings limit: %v", err)
	}
	if len(limited) != 2 || limited[0].Day != "2026-08-24" {
		t.Errorf("limit 2 returned %d rows starting at %v", len(limited), limited)
	}
}

func TestDeletingABriefingTakesItsCall(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	at := time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC)
	b, _ := insertBriefingWithCall(t, s, ModePaper, "2026-08-24", "SPY", "up", at)

	if _, err := s.db.ExecContext(ctx, `DELETE FROM briefings WHERE id = ?`, b.ID); err != nil {
		t.Fatalf("deleting briefing: %v", err)
	}
	if _, err := s.CallByBriefing(ctx, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("CallByBriefing after cascade: %v, want ErrNotFound", err)
	}
}

// TestBriefingTablesArriveByMigration opens a journal that stopped at the order
// and fill schema, the way an existing install does, and writes a briefing to it.
func TestBriefingTablesArriveByMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tape.db")

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("opening the raw database: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("creating schema_migrations: %v", err)
	}
	if err := applyMigration(ctx, db, 1, schemaV1); err != nil {
		t.Fatalf("applying migration 1: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the raw database: %v", err)
	}

	s, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("Open on a v1 journal: %v", err)
	}
	defer s.Close()

	b := newBriefing(ModePaper, "2026-08-24", time.Date(2026, 8, 24, 12, 52, 0, 0, time.UTC))
	if err := s.InsertBriefing(ctx, b); err != nil {
		t.Fatalf("InsertBriefing after the upgrade: %v", err)
	}
}

func TestBriefingDayFormatIsFixedWidth(t *testing.T) {
	if err := validateDay("2026-08-24"); err != nil {
		t.Errorf("validateDay: %v", err)
	}
	err := validateDay("2026-8-24")
	if err == nil {
		t.Fatal("validateDay accepted a variable-width day")
	}
	if !strings.Contains(err.Error(), dayLayout) {
		t.Errorf("error %q does not name the layout", err)
	}
}
