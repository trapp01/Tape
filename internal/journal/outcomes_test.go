package journal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

var outcomeTime = time.Date(2026, 8, 28, 21, 5, 0, 0, time.UTC)

// seedProposal files a briefing and one proposal on it, decided as status.
func seedProposal(t *testing.T, s *Store, mode, day string, index int, symbol, status string) Proposal {
	t.Helper()
	ctx := context.Background()
	briefingID := seedBriefing(t, s, mode, day)
	p := newProposal(briefingID, mode, day, index, symbol)
	if err := s.InsertProposals(ctx, []*Proposal{p}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	if status == "" || status == ProposalProposed {
		return *p
	}
	switch status {
	case ProposalTaken:
		orderID := seedProposalOrder(t, s, mode, symbol, string(broker.StatusFilled))
		if err := s.DecideTaken(ctx, p.ID, orderID, 16, 24, outcomeTime); err != nil {
			t.Fatalf("DecideTaken: %v", err)
		}
	case ProposalUnfilled:
		// An unfilled take needs a terminal order that traded nothing.
		orderID := seedProposalOrder(t, s, mode, symbol, string(broker.StatusCanceled))
		if err := s.DecideTaken(ctx, p.ID, orderID, 16, 24, outcomeTime); err != nil {
			t.Fatalf("DecideTaken: %v", err)
		}
		if err := s.MarkUnfilled(ctx, p.ID, outcomeTime); err != nil {
			t.Fatalf("MarkUnfilled: %v", err)
		}
	case ProposalSubmitting:
		if err := s.ClaimProposal(ctx, p.ID, outcomeTime); err != nil {
			t.Fatalf("ClaimProposal: %v", err)
		}
	default:
		if err := s.DecideProposal(ctx, p.ID, status, "the setup lost its level", nil, outcomeTime); err != nil {
			t.Fatalf("DecideProposal %s: %v", status, err)
		}
	}
	got, err := s.ProposalByDayIndex(ctx, mode, day, index)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	return got
}

func seedProposalOrder(t *testing.T, s *Store, mode, symbol, status string) int64 {
	t.Helper()
	o := &Order{
		Symbol: symbol, Side: "buy", Qty: 16, Type: "limit", Status: status,
		Source: SourceProposal, Mode: mode, SubmittedAt: outcomeTime,
	}
	if err := s.InsertOrder(context.Background(), o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	return o.ID
}

func newOutcome(proposalID int64, mode, day string) *ProposalOutcome {
	fillPrice, exitPrice := 128.40, 131.40
	filledAt := outcomeTime.Add(-4 * time.Hour)
	exitAt := outcomeTime.Add(-2 * time.Hour)
	return &ProposalOutcome{
		ProposalID: proposalID,
		Mode:       mode,
		Day:        day,
		Filled:     true,
		FillPrice:  &fillPrice,
		FilledAt:   &filledAt,
		ExitKind:   ExitTarget,
		ExitPrice:  &exitPrice,
		ExitAt:     &exitAt,
		Qty:        16,
		GrossPL:    48,
		Costs:      2.16,
		NetPL:      45.84,
		RMultiple:  1.91,
		Ambiguous:  true,
		ScoredAt:   outcomeTime,
	}
}

func TestInsertProposalOutcomeRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalPassed)

	o := newOutcome(p.ID, ModePaper, proposalDay)
	if err := s.InsertProposalOutcome(ctx, o); err != nil {
		t.Fatalf("InsertProposalOutcome: %v", err)
	}
	if o.ID == 0 {
		t.Fatal("outcome was not given an id")
	}

	got, err := s.OutcomeByProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("OutcomeByProposal: %v", err)
	}
	if !got.Filled || !got.Ambiguous {
		t.Errorf("Filled = %v, Ambiguous = %v, want both true", got.Filled, got.Ambiguous)
	}
	if got.ExitKind != ExitTarget || got.Qty != 16 {
		t.Errorf("exit did not survive: %+v", got)
	}
	if got.FillPrice == nil || *got.FillPrice != 128.40 {
		t.Errorf("FillPrice = %v, want 128.40", got.FillPrice)
	}
	if got.ExitPrice == nil || *got.ExitPrice != 131.40 {
		t.Errorf("ExitPrice = %v, want 131.40", got.ExitPrice)
	}
	if got.FilledAt == nil || !got.FilledAt.Equal(outcomeTime.Add(-4*time.Hour)) {
		t.Errorf("FilledAt = %v, want %v", got.FilledAt, outcomeTime.Add(-4*time.Hour))
	}
	if got.ExitAt == nil || !got.ExitAt.Equal(outcomeTime.Add(-2*time.Hour)) {
		t.Errorf("ExitAt = %v, want %v", got.ExitAt, outcomeTime.Add(-2*time.Hour))
	}
	closeTo(t, "GrossPL", got.GrossPL, 48)
	closeTo(t, "Costs", got.Costs, 2.16)
	closeTo(t, "NetPL", got.NetPL, 45.84)
	closeTo(t, "RMultiple", got.RMultiple, 1.91)
	if !got.ScoredAt.Equal(outcomeTime) {
		t.Errorf("ScoredAt = %v, want %v", got.ScoredAt, outcomeTime)
	}
}

func TestInsertProposalOutcomeUnfilledKeepsNilPrices(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalExpired)

	o := &ProposalOutcome{ProposalID: p.ID, Mode: ModePaper, Day: proposalDay, ExitKind: ExitNone}
	if err := s.InsertProposalOutcome(ctx, o); err != nil {
		t.Fatalf("InsertProposalOutcome: %v", err)
	}

	got, err := s.OutcomeByProposal(ctx, p.ID)
	if err != nil {
		t.Fatalf("OutcomeByProposal: %v", err)
	}
	if got.Filled || got.Ambiguous {
		t.Errorf("Filled = %v, Ambiguous = %v, want both false", got.Filled, got.Ambiguous)
	}
	if got.FillPrice != nil || got.FilledAt != nil || got.ExitPrice != nil || got.ExitAt != nil {
		t.Errorf("an entry that never filled carries prices: %+v", got)
	}
	if got.ScoredAt.IsZero() {
		t.Error("ScoredAt was not stamped")
	}
}

func TestInsertProposalOutcomeDefaultsExitKind(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalPassed)

	o := &ProposalOutcome{ProposalID: p.ID, Mode: ModePaper, Day: proposalDay}
	if err := s.InsertProposalOutcome(ctx, o); err != nil {
		t.Fatalf("InsertProposalOutcome: %v", err)
	}
	if o.ExitKind != ExitNone {
		t.Errorf("ExitKind = %q, want %q", o.ExitKind, ExitNone)
	}
}

func TestInsertProposalOutcomeRejectsUnknownExitKind(t *testing.T) {
	s := newStore(t)
	p := seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalPassed)

	o := &ProposalOutcome{ProposalID: p.ID, Mode: ModePaper, Day: proposalDay, ExitKind: "vibes"}
	if err := s.InsertProposalOutcome(context.Background(), o); err == nil {
		t.Fatal("an unknown exit kind was accepted, want an error")
	}
}

func TestInsertProposalOutcomeIsOncePerProposal(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalPassed)

	if err := s.InsertProposalOutcome(ctx, newOutcome(p.ID, ModePaper, proposalDay)); err != nil {
		t.Fatalf("first InsertProposalOutcome: %v", err)
	}
	err := s.InsertProposalOutcome(ctx, newOutcome(p.ID, ModePaper, proposalDay))
	if !errors.Is(err, ErrAlreadyScored) {
		t.Fatalf("second InsertProposalOutcome error = %v, want ErrAlreadyScored", err)
	}
}

func TestOutcomeByProposalNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.OutcomeByProposal(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OutcomeByProposal error = %v, want ErrNotFound", err)
	}
}

func TestOutcomesInRangeBoundsAreInclusive(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i, day := range []string{"2026-08-24", "2026-08-26", "2026-08-28"} {
		p := seedProposal(t, s, ModePaper, day, i+1, "NVDA", ProposalPassed)
		if err := s.InsertProposalOutcome(ctx, newOutcome(p.ID, ModePaper, day)); err != nil {
			t.Fatalf("InsertProposalOutcome %s: %v", day, err)
		}
	}

	got, err := s.OutcomesInRange(ctx, ModePaper, "2026-08-24", "2026-08-28")
	if err != nil {
		t.Fatalf("OutcomesInRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d outcomes over the full range, want 3", len(got))
	}
	if got[0].Day != "2026-08-24" || got[2].Day != "2026-08-28" {
		t.Errorf("outcomes came back out of order: %s then %s", got[0].Day, got[2].Day)
	}

	inner, err := s.OutcomesInRange(ctx, ModePaper, "2026-08-25", "2026-08-27")
	if err != nil {
		t.Fatalf("OutcomesInRange inner: %v", err)
	}
	if len(inner) != 1 || inner[0].Day != "2026-08-26" {
		t.Fatalf("inner range = %+v, want only 2026-08-26", inner)
	}
}

func TestOutcomesInRangeKeepsModesApart(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i, mode := range []string{ModePaper, ModeLive} {
		p := seedProposal(t, s, mode, proposalDay, i+1, "NVDA", ProposalPassed)
		if err := s.InsertProposalOutcome(ctx, newOutcome(p.ID, mode, proposalDay)); err != nil {
			t.Fatalf("InsertProposalOutcome %s: %v", mode, err)
		}
	}

	paper, err := s.OutcomesInRange(ctx, ModePaper, proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("OutcomesInRange paper: %v", err)
	}
	if len(paper) != 1 || paper[0].Mode != ModePaper {
		t.Fatalf("paper outcomes = %+v, want one paper row", paper)
	}
	both, err := s.OutcomesInRange(ctx, "", proposalDay, proposalDay)
	if err != nil {
		t.Fatalf("OutcomesInRange both: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("got %d outcomes for both modes, want 2", len(both))
	}
}

func TestUnscoredProposalsSkipsUndecidedAndScored(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	decided := map[string]int64{}
	for i, status := range []string{ProposalTaken, ProposalPassed, ProposalRejected, ProposalExpired, ProposalUnfilled} {
		p := seedProposal(t, s, ModePaper, proposalDay, i+1, "NVDA", status)
		decided[status] = p.ID
	}
	seedProposal(t, s, ModePaper, proposalDay, 6, "AAPL", ProposalProposed)
	seedProposal(t, s, ModePaper, proposalDay, 7, "MSFT", ProposalSubmitting)

	got, err := s.UnscoredProposals(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("UnscoredProposals: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d unscored proposals, want the 5 decided ones: %+v", len(got), got)
	}
	for _, p := range got {
		if p.Status == ProposalProposed || p.Status == ProposalSubmitting {
			t.Errorf("proposal %d is still %s and should not be scorable", p.ID, p.Status)
		}
	}

	// Scoring one takes it off the list.
	if err := s.InsertProposalOutcome(ctx, newOutcome(decided[ProposalPassed], ModePaper, proposalDay)); err != nil {
		t.Fatalf("InsertProposalOutcome: %v", err)
	}
	got, err = s.UnscoredProposals(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("UnscoredProposals after scoring: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d unscored proposals after scoring one, want 4", len(got))
	}
}

func TestUnscoredProposalsStopsAtThroughDay(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedProposal(t, s, ModePaper, "2026-08-27", 1, "NVDA", ProposalPassed)
	seedProposal(t, s, ModePaper, "2026-08-28", 1, "AAPL", ProposalPassed)

	got, err := s.UnscoredProposals(ctx, ModePaper, "2026-08-27")
	if err != nil {
		t.Fatalf("UnscoredProposals: %v", err)
	}
	if len(got) != 1 || got[0].Day != "2026-08-27" {
		t.Fatalf("got %+v, want only the 2026-08-27 proposal", got)
	}
}

func TestUnscoredProposalsKeepsModesApart(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedProposal(t, s, ModePaper, proposalDay, 1, "NVDA", ProposalPassed)
	seedProposal(t, s, ModeLive, proposalDay, 1, "AAPL", ProposalPassed)

	live, err := s.UnscoredProposals(ctx, ModeLive, proposalDay)
	if err != nil {
		t.Fatalf("UnscoredProposals live: %v", err)
	}
	if len(live) != 1 || live[0].Symbol != "AAPL" {
		t.Fatalf("live = %+v, want only the live proposal", live)
	}
	both, err := s.UnscoredProposals(ctx, "", proposalDay)
	if err != nil {
		t.Fatalf("UnscoredProposals both: %v", err)
	}
	if len(both) != 2 {
		t.Fatalf("got %d unscored proposals for both modes, want 2", len(both))
	}
}
