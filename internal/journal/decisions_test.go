package journal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

// fileProposal writes one idea and returns it, ready to be decided.
func fileProposal(t *testing.T, s *Store, mode, day string, index int, symbol string) *Proposal {
	t.Helper()
	id := seedBriefing(t, s, mode, day)
	p := newProposal(id, mode, day, index, symbol)
	if err := s.InsertProposals(context.Background(), []*Proposal{p}); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	return p
}

// fileOrder writes the order a taken proposal points at. The link is a foreign
// key, so the order has to exist.
func fileOrder(t *testing.T, s *Store, mode, symbol string, proposalID *int64) int64 {
	t.Helper()
	o := &Order{
		Symbol:      symbol,
		Side:        string(broker.Buy),
		Qty:         16,
		Type:        "limit",
		Status:      "new",
		Source:      SourceProposal,
		ProposalID:  proposalID,
		Mode:        mode,
		SubmittedAt: proposalTime,
	}
	if err := s.InsertOrder(context.Background(), o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	return o.ID
}

func TestDecideProposal(t *testing.T) {
	ctx := context.Background()
	decidedAt := proposalTime.Add(20 * time.Minute)

	tests := []struct {
		name   string
		status string
		reason string
		// takes asks for a real order to link the decision to.
		takes bool
	}{
		{"taken", ProposalTaken, "", true},
		{"passed", ProposalPassed, "Spread too wide at the open.", false},
		{"rejected", ProposalRejected, "risk: entry is 8.10% from the last price (rule: entry near last)", false},
		{"expired", ProposalExpired, "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")

			var orderID *int64
			if tc.takes {
				id := fileOrder(t, s, ModePaper, "NVDA", &p.ID)
				orderID = &id
			}
			if err := s.DecideProposal(ctx, p.ID, tc.status, tc.reason, orderID, decidedAt); err != nil {
				t.Fatalf("DecideProposal: %v", err)
			}
			got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
			if err != nil {
				t.Fatalf("ProposalByDayIndex: %v", err)
			}
			if got.Status != tc.status {
				t.Errorf("status = %q, want %q", got.Status, tc.status)
			}
			if got.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.reason)
			}
			if got.DecidedAt == nil || !got.DecidedAt.Equal(decidedAt) {
				t.Errorf("decided at = %v, want %v", got.DecidedAt, decidedAt)
			}
			if orderID == nil {
				if got.OrderID != nil {
					t.Errorf("order id = %v, want none", *got.OrderID)
				}
			} else if got.OrderID == nil || *got.OrderID != *orderID {
				t.Errorf("order id = %v, want %d", got.OrderID, *orderID)
			}
		})
	}
}

func TestDecideProposalRefusals(t *testing.T) {
	ctx := context.Background()
	orderID := int64(77)

	tests := []struct {
		name    string
		status  string
		reason  string
		orderID *int64
		wantErr string
	}{
		{"taken without an order", ProposalTaken, "looks good", nil, "needs the order"},
		{"passed without a reason", ProposalPassed, "", nil, "needs a reason"},
		{"passed with a blank reason", ProposalPassed, "   ", nil, "needs a reason"},
		{"rejected without a reason", ProposalRejected, "", nil, "needs a reason"},
		{"an unknown status", "maybe", "later", nil, "not one of"},
		{"back to proposed", ProposalProposed, "", nil, "not one of"},
		{"no status", "", "", &orderID, "not one of"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")

			err := s.DecideProposal(ctx, p.ID, tc.status, tc.reason, tc.orderID, proposalTime)
			if err == nil {
				t.Fatal("DecideProposal accepted the decision, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
			got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
			if err != nil {
				t.Fatalf("ProposalByDayIndex: %v", err)
			}
			if got.Status != ProposalProposed {
				t.Errorf("a refused decision moved the row to %q", got.Status)
			}
		})
	}
}

// A decision is made once. The second attempt names what is already on the row,
// so a pass cannot become a take after the session answered it.
func TestDecideProposalOnlyOnce(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")

	if err := s.DecideProposal(ctx, p.ID, ProposalPassed, "No edge at that entry.", nil, proposalTime); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}

	orderID := fileOrder(t, s, ModePaper, "NVDA", nil)
	err := s.DecideProposal(ctx, p.ID, ProposalTaken, "", &orderID, proposalTime)
	if err == nil {
		t.Fatal("a passed proposal was taken afterwards")
	}
	if !strings.Contains(err.Error(), ProposalPassed) {
		t.Errorf("error %q does not name the decision on the row", err)
	}

	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalPassed || got.Reason != "No edge at that entry." {
		t.Errorf("the second decision changed the row: %+v", got)
	}
}

func TestDecideProposalNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	if err := s.DecideProposal(ctx, 9999, ProposalExpired, "", nil, proposalTime); !errors.Is(err, ErrNotFound) {
		t.Errorf("deciding a proposal that does not exist: %v, want ErrNotFound", err)
	}
	if err := s.DecideProposal(ctx, 0, ProposalExpired, "", nil, proposalTime); err == nil {
		t.Error("DecideProposal accepted id 0")
	}
}

// Nothing is left open at the end of a session: an idea nobody acted on is
// expired, which is a fact about the day, not a gap in it.
func TestExpireOpenProposals(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	at := proposalTime.Add(8 * time.Hour)

	paper := seedBriefing(t, s, ModePaper, proposalDay)
	live := seedBriefing(t, s, ModeLive, proposalDay)
	slate := []*Proposal{
		newProposal(paper, ModePaper, proposalDay, 1, "NVDA"),
		newProposal(paper, ModePaper, proposalDay, 2, "AAPL"),
		newProposal(paper, ModePaper, proposalDay, 3, "SPY"),
		newProposal(live, ModeLive, proposalDay, 1, "TSLA"),
	}
	if err := s.InsertProposals(ctx, slate); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}
	taken := fileOrder(t, s, ModePaper, "NVDA", &slate[0].ID)
	if err := s.DecideProposal(ctx, slate[0].ID, ProposalTaken, "", &taken, proposalTime); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}

	n, err := s.ExpireOpenProposals(ctx, ModePaper, proposalDay, at)
	if err != nil {
		t.Fatalf("ExpireOpenProposals: %v", err)
	}
	if n != 2 {
		t.Errorf("expired %d proposals, want the 2 nobody decided", n)
	}

	got, err := s.ProposalsForDay(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if got[0].Status != ProposalTaken {
		t.Errorf("the taken proposal became %q", got[0].Status)
	}
	for _, p := range got[1:] {
		if p.Status != ProposalExpired {
			t.Errorf("proposal %d status = %q, want %q", p.Index, p.Status, ProposalExpired)
		}
		if p.DecidedAt == nil || !p.DecidedAt.Equal(at) {
			t.Errorf("proposal %d decided at %v, want %v", p.Index, p.DecidedAt, at)
		}
		if p.Reason == "" {
			t.Errorf("proposal %d expired without saying why", p.Index)
		}
	}

	// Live is a separate record and a paper close does not touch it.
	liveSlate, err := s.ProposalsForDay(ctx, ModeLive, proposalDay)
	if err != nil {
		t.Fatalf("ProposalsForDay live: %v", err)
	}
	if len(liveSlate) != 1 || liveSlate[0].Status != ProposalProposed {
		t.Errorf("the paper close touched live: %+v", liveSlate)
	}

	again, err := s.ExpireOpenProposals(ctx, ModePaper, proposalDay, at)
	if err != nil {
		t.Fatalf("ExpireOpenProposals twice: %v", err)
	}
	if again != 0 {
		t.Errorf("a second close expired %d more proposals", again)
	}
}

func TestExpireOpenProposalsChecksTheDay(t *testing.T) {
	s := newStore(t)
	if _, err := s.ExpireOpenProposals(context.Background(), ModePaper, "2026-8-28", proposalTime); err == nil {
		t.Error("ExpireOpenProposals accepted a variable-width day")
	}
}
