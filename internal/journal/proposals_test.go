package journal

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

const proposalDay = "2026-08-28"

var proposalTime = time.Date(2026, 8, 28, 12, 52, 0, 0, time.UTC)

// seedBriefing files a briefing to hang proposals on; a proposal without one has
// nothing to be a proposal from.
func seedBriefing(t *testing.T, s *Store, mode, day string) int64 {
	t.Helper()
	b := newBriefing(mode, day, proposalTime)
	if err := s.InsertBriefing(context.Background(), b); err != nil {
		t.Fatalf("InsertBriefing: %v", err)
	}
	return b.ID
}

func newProposal(briefingID int64, mode, day string, index int, symbol string) *Proposal {
	return &Proposal{
		BriefingID:   briefingID,
		Mode:         mode,
		Day:          day,
		Index:        index,
		Symbol:       symbol,
		Side:         "long",
		SetupID:      "M2",
		Entry:        128.40,
		Stop:         126.90,
		Target:       131.40,
		Qty:          16,
		RiskUSD:      24,
		Thesis:       "Yesterday's high held on the retest.",
		Invalidation: "A five-minute close back under 127.80.",
		Confidence:   "medium",
		CreatedAt:    proposalTime,
	}
}

func TestInsertProposalsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := seedBriefing(t, s, ModePaper, proposalDay)

	slate := []*Proposal{
		newProposal(id, ModePaper, proposalDay, 1, "NVDA"),
		newProposal(id, ModePaper, proposalDay, 2, "AAPL"),
	}
	if err := s.InsertProposals(ctx, slate); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	for i, p := range slate {
		if p.ID == 0 {
			t.Fatalf("proposal %d was not given an id", i)
		}
		if p.Status != ProposalProposed {
			t.Errorf("proposal %d status = %q, want %q", i, p.Status, ProposalProposed)
		}
	}

	got, err := s.ProposalsByBriefing(ctx, id)
	if err != nil {
		t.Fatalf("ProposalsByBriefing: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d proposals, want 2", len(got))
	}
	if got[0].Index != 1 || got[1].Index != 2 {
		t.Errorf("proposals came back out of order: %d then %d", got[0].Index, got[1].Index)
	}

	first := got[0]
	want := slate[0]
	if first.Symbol != "NVDA" || first.Side != "long" || first.SetupID != "M2" {
		t.Errorf("identity did not survive: %+v", first)
	}
	if first.Entry != want.Entry || first.Stop != want.Stop || first.Target != want.Target {
		t.Errorf("prices did not survive: %+v", first)
	}
	if first.Qty != 16 || first.RiskUSD != 24 {
		t.Errorf("the plan did not survive: qty %d risk %v", first.Qty, first.RiskUSD)
	}
	if first.Thesis != want.Thesis || first.Invalidation != want.Invalidation || first.Confidence != "medium" {
		t.Errorf("the words did not survive: %+v", first)
	}
	if !first.CreatedAt.Equal(proposalTime) {
		t.Errorf("created at = %v, want %v", first.CreatedAt, proposalTime)
	}
	if first.DecidedAt != nil || first.OrderID != nil || first.Reason != "" {
		t.Errorf("a new proposal carries a decision: %+v", first)
	}
}

func TestInsertProposalsOfAnEmptySlate(t *testing.T) {
	s := newStore(t)
	if err := s.InsertProposals(context.Background(), nil); err != nil {
		t.Errorf("a morning with no ideas is not an error: %v", err)
	}
}

// The slate is one transaction: a briefing's ideas are all recorded or none are,
// so a half-written morning can never be read back as the whole one.
func TestInsertProposalsIsAllOrNothing(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := seedBriefing(t, s, ModePaper, proposalDay)

	slate := []*Proposal{
		newProposal(id, ModePaper, proposalDay, 1, "NVDA"),
		newProposal(id, ModePaper, proposalDay, 1, "AAPL"),
	}
	if err := s.InsertProposals(ctx, slate); err == nil {
		t.Fatal("InsertProposals accepted two proposals at the same index")
	}
	for i, p := range slate {
		if p.ID != 0 {
			t.Errorf("proposal %d kept id %d from a rolled-back write", i, p.ID)
		}
	}
	got, err := s.ProposalsByBriefing(ctx, id)
	if err != nil {
		t.Fatalf("ProposalsByBriefing: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the rolled-back slate left %d rows", len(got))
	}
}

func TestInsertProposalsRefusals(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := seedBriefing(t, s, ModePaper, proposalDay)

	tests := []struct {
		name    string
		mutate  func(*Proposal)
		wantErr string
	}{
		{"no briefing", func(p *Proposal) { p.BriefingID = 0 }, "briefing id"},
		{"an unknown mode", func(p *Proposal) { p.Mode = "shadow" }, "mode"},
		{"no day", func(p *Proposal) { p.Day = "" }, "day"},
		{"a variable-width day", func(p *Proposal) { p.Day = "2026-8-28" }, dayLayout},
		{"a zero index", func(p *Proposal) { p.Index = 0 }, "1-based"},
		{"a negative index", func(p *Proposal) { p.Index = -1 }, "1-based"},
		{"no symbol", func(p *Proposal) { p.Symbol = "" }, "symbol"},
		{"a decision at insert", func(p *Proposal) { p.Status = ProposalTaken }, ProposalProposed},
		{"a briefing that does not exist", func(p *Proposal) { p.BriefingID = 9999 }, "insert proposal"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProposal(id, ModePaper, proposalDay, 1, "NVDA")
			tc.mutate(p)
			err := s.InsertProposals(ctx, []*Proposal{p})
			if err == nil {
				t.Fatal("InsertProposals accepted the proposal, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}

	if err := s.InsertProposals(ctx, []*Proposal{nil}); err == nil {
		t.Error("InsertProposals accepted a nil proposal")
	}
}

// The day is what `tape take N` addresses, and paper and live never share a slate.
func TestProposalsForDayAndIndex(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	paper := seedBriefing(t, s, ModePaper, proposalDay)
	live := seedBriefing(t, s, ModeLive, proposalDay)

	if err := s.InsertProposals(ctx, []*Proposal{
		newProposal(paper, ModePaper, proposalDay, 1, "NVDA"),
		newProposal(paper, ModePaper, proposalDay, 2, "AAPL"),
		newProposal(live, ModeLive, proposalDay, 1, "TSLA"),
	}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}

	got, err := s.ProposalsForDay(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if len(got) != 2 || got[0].Symbol != "NVDA" || got[1].Symbol != "AAPL" {
		t.Fatalf("paper day = %+v", got)
	}

	one, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if one.Symbol != "NVDA" {
		t.Errorf("proposal 1 = %s, want NVDA", one.Symbol)
	}
	if _, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("proposal 3: %v, want ErrNotFound", err)
	}
	// The live slate is a different morning's record and must not leak into paper.
	if _, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1); err != nil || one.Symbol == "TSLA" {
		t.Errorf("live proposals leaked into paper: %+v", one)
	}
	if empty, err := s.ProposalsForDay(ctx, ModePaper, "2026-08-27"); err != nil || len(empty) != 0 {
		t.Errorf("another day = %+v (%v)", empty, err)
	}
}

// A forced re-run files a second slate. `tape take 1` means the one the trader
// was just shown, which is the newer row.
func TestProposalByDayIndexPrefersTheLatestSlate(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first := seedBriefing(t, s, ModePaper, proposalDay)
	second := seedBriefing(t, s, ModePaper, proposalDay)

	if err := s.InsertProposals(ctx, []*Proposal{newProposal(first, ModePaper, proposalDay, 1, "NVDA")}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	if err := s.InsertProposals(ctx, []*Proposal{newProposal(second, ModePaper, proposalDay, 1, "AAPL")}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}

	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Symbol != "AAPL" {
		t.Errorf("proposal 1 = %s, want the re-run's AAPL", got.Symbol)
	}
	all, err := s.ProposalsForDay(ctx, ModePaper, proposalDay)
	if err != nil || len(all) != 2 {
		t.Errorf("both slates stay on the record: %+v (%v)", all, err)
	}
}

// An order carries the proposal it executes, so a taken idea can be followed all
// the way to its fills.
func TestOrderCarriesItsProposal(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := seedBriefing(t, s, ModePaper, proposalDay)

	p := newProposal(id, ModePaper, proposalDay, 1, "NVDA")
	if err := s.InsertProposals(ctx, []*Proposal{p}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}

	o := &Order{
		BrokerOrderID: "venue-1",
		Symbol:        "NVDA",
		Side:          string(broker.Buy),
		Qty:           16,
		Type:          "limit",
		Status:        "new",
		Source:        SourceProposal,
		ProposalID:    &p.ID,
		Mode:          ModePaper,
		SubmittedAt:   proposalTime,
	}
	if err := s.InsertOrder(ctx, o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}

	got, err := s.OrderByBrokerID(ctx, "venue-1")
	if err != nil {
		t.Fatalf("OrderByBrokerID: %v", err)
	}
	if got.ProposalID == nil || *got.ProposalID != p.ID {
		t.Fatalf("order proposal id = %v, want %d", got.ProposalID, p.ID)
	}

	listed, err := s.ListOrders(ctx, ListFilter{Mode: ModePaper})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(listed) != 1 || listed[0].ProposalID == nil || *listed[0].ProposalID != p.ID {
		t.Errorf("listed orders lost the link: %+v", listed)
	}

	// A human order has no proposal behind it, and the column says so.
	human := &Order{
		BrokerOrderID: "venue-2",
		Symbol:        "SPY",
		Side:          string(broker.Buy),
		Qty:           1,
		Status:        "new",
		Source:        SourceHuman,
		Mode:          ModePaper,
		SubmittedAt:   proposalTime,
	}
	if err := s.InsertOrder(ctx, human); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	back, err := s.OrderByBrokerID(ctx, "venue-2")
	if err != nil {
		t.Fatalf("OrderByBrokerID: %v", err)
	}
	if back.ProposalID != nil {
		t.Errorf("a human order carries proposal %v", *back.ProposalID)
	}
}

// TestProposalTablesArriveByMigration opens a journal that stopped at the
// briefing schema, the way an existing install does, and files a proposal on it.
func TestProposalTablesArriveByMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tape.db")

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("opening the raw database: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("creating schema_migrations: %v", err)
	}
	for i, stmts := range [][]string{schemaV1, schemaV2} {
		if err := applyMigration(ctx, db, i+1, stmts); err != nil {
			t.Fatalf("applying migration %d: %v", i+1, err)
		}
	}
	// A v2 order predates the proposal link, so the upgrade has to backfill NULL.
	const insert = `INSERT INTO orders (symbol, side, qty, status, mode, submitted_at, updated_at)
		VALUES ('SPY', 'buy', 1, 'filled', 'paper', ?, ?)`
	stamp := formatTime(proposalTime)
	if _, err := db.ExecContext(ctx, insert, stamp, stamp); err != nil {
		t.Fatalf("seeding a v2 order: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing the raw database: %v", err)
	}

	s, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("Open on a v2 journal: %v", err)
	}
	defer s.Close()

	id := seedBriefing(t, s, ModePaper, proposalDay)
	if err := s.InsertProposals(ctx, []*Proposal{newProposal(id, ModePaper, proposalDay, 1, "NVDA")}); err != nil {
		t.Fatalf("InsertProposals after the upgrade: %v", err)
	}
	if err := s.InsertRefusal(ctx, &Refusal{Mode: ModePaper, Day: proposalDay, Rule: "no overspend"}); err != nil {
		t.Fatalf("InsertRefusal after the upgrade: %v", err)
	}
	orders, err := s.ListOrders(ctx, ListFilter{Mode: ModePaper})
	if err != nil {
		t.Fatalf("ListOrders after the upgrade: %v", err)
	}
	if len(orders) != 1 || orders[0].ProposalID != nil {
		t.Errorf("the v2 order did not survive the upgrade: %+v", orders)
	}
}
