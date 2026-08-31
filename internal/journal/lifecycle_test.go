package journal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

// settleOrder puts an order into a terminal state with a fill count, which is
// what MarkUnfilled reads to tell a dead order from a traded one.
func settleOrder(t *testing.T, s *Store, orderID int64, status string, filledQty int) {
	t.Helper()
	const update = `UPDATE orders SET status = ?, filled_qty = ? WHERE id = ?`
	if _, err := s.db.ExecContext(context.Background(), update, status, filledQty, orderID); err != nil {
		t.Fatalf("settling order %d: %v", orderID, err)
	}
}

// A claim is exclusive: the window between the venue accepting an order and the
// journal recording the take is a state, not a gap a second take can walk into.
func TestClaimProposalIsExclusive(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")

	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalSubmitting {
		t.Fatalf("status = %q, want %q", got.Status, ProposalSubmitting)
	}
	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err == nil {
		t.Fatal("a second claim was accepted; an order may already be live")
	}
}

// A situational refusal happens after the claim and decides nothing, so the
// claim comes off and the idea is takeable again.
func TestReleaseProposalReopensAClaim(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")

	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}
	if err := s.ReleaseProposal(ctx, p.ID); err != nil {
		t.Fatalf("ReleaseProposal: %v", err)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalProposed || got.DecidedAt != nil {
		t.Fatalf("released proposal = %+v, want a clean %q row", got, ProposalProposed)
	}
	if err := s.ReleaseProposal(ctx, p.ID); err == nil {
		t.Fatal("releasing a proposal that holds no claim was accepted")
	}
}

// A take may be decided from a claim: the order reached the venue, the decision
// is being written now.
func TestDecideProposalFromASubmittingClaim(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
	orderID := fileOrder(t, s, ModePaper, "NVDA", &p.ID)

	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}
	if err := s.DecideProposal(ctx, p.ID, ProposalTaken, "", &orderID, proposalTime); err != nil {
		t.Fatalf("DecideProposal from a claim: %v", err)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalTaken || got.OrderID == nil || *got.OrderID != orderID {
		t.Fatalf("proposal = %+v, want taken linked to order %d", got, orderID)
	}
}

// A crash between the venue accepting the order and the decision landing leaves
// a claim with an order pointing at it. Reconciliation closes that, once.
func TestReconcileSubmittedProposals(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
	orderID := fileOrder(t, s, ModePaper, "NVDA", &p.ID)
	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}

	ids, err := s.ReconcileSubmittedProposals(ctx, ModePaper, proposalTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileSubmittedProposals: %v", err)
	}
	if len(ids) != 1 || ids[0] != p.ID {
		t.Fatalf("reconciled %v, want [%d]", ids, p.ID)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalTaken || got.OrderID == nil || *got.OrderID != orderID {
		t.Fatalf("proposal = %+v, want taken linked to order %d", got, orderID)
	}
	// The order that reached the venue is what was traded, whatever was sized.
	if got.TakenQty == nil || *got.TakenQty != 16 {
		t.Fatalf("traded qty = %v, want the order's 16", got.TakenQty)
	}

	again, err := s.ReconcileSubmittedProposals(ctx, ModePaper, proposalTime)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("a second pass reconciled %v", again)
	}
}

// A claim with nothing at the venue is left alone: reconciliation closes the
// crash window, it does not invent a trade.
func TestReconcileLeavesAnUnorderedClaim(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
	if err := s.ClaimProposal(ctx, p.ID, proposalTime); err != nil {
		t.Fatalf("ClaimProposal: %v", err)
	}

	ids, err := s.ReconcileSubmittedProposals(ctx, ModePaper, proposalTime)
	if err != nil {
		t.Fatalf("ReconcileSubmittedProposals: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("reconciled %v with no order at the venue", ids)
	}
}

// A taken proposal whose order died unfilled is not a trade. The record has to
// say so or the pass side is scored against a position nobody ever held.
func TestMarkUnfilled(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
	orderID := fileOrder(t, s, ModePaper, "NVDA", &p.ID)
	if err := s.DecideProposal(ctx, p.ID, ProposalTaken, "", &orderID, proposalTime); err != nil {
		t.Fatalf("DecideProposal: %v", err)
	}
	settleOrder(t, s, orderID, string(broker.StatusCanceled), 0)

	at := proposalTime.Add(6 * time.Hour)
	if err := s.MarkUnfilled(ctx, p.ID, at); err != nil {
		t.Fatalf("MarkUnfilled: %v", err)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalUnfilled || got.Reason == "" {
		t.Fatalf("proposal = %+v, want %q with a reason", got, ProposalUnfilled)
	}
	if got.OrderID == nil || *got.OrderID != orderID {
		t.Fatalf("an unfilled proposal keeps the order it was submitted as, got %+v", got.OrderID)
	}
}

func TestMarkUnfilledRefusals(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		status    string
		filledQty int
		decide    bool
		wantErr   string
	}{
		{"a partly filled order", string(broker.StatusCanceled), 4, true, "filled"},
		{"an order still working", string(broker.StatusNew), 0, true, "terminal"},
		{"a proposal nobody took", string(broker.StatusCanceled), 0, false, ProposalProposed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
			orderID := fileOrder(t, s, ModePaper, "NVDA", &p.ID)
			if tc.decide {
				if err := s.DecideProposal(ctx, p.ID, ProposalTaken, "", &orderID, proposalTime); err != nil {
					t.Fatalf("DecideProposal: %v", err)
				}
			}
			settleOrder(t, s, orderID, tc.status, tc.filledQty)

			err := s.MarkUnfilled(ctx, p.ID, proposalTime)
			if err == nil {
				t.Fatal("MarkUnfilled accepted a proposal that was traded or never taken")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// UnfilledForDay is what eod runs: it finds the day's dead takes without the
// caller having to know which order belongs to which idea.
func TestUnfilledForDay(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	briefingID := seedBriefing(t, s, ModePaper, proposalDay)
	slate := []*Proposal{
		newProposal(briefingID, ModePaper, proposalDay, 1, "NVDA"),
		newProposal(briefingID, ModePaper, proposalDay, 2, "AAPL"),
	}
	if err := s.InsertProposals(ctx, slate); err != nil {
		t.Fatalf("InsertProposals: %v", err)
	}

	dead := fileOrder(t, s, ModePaper, "NVDA", &slate[0].ID)
	traded := fileOrder(t, s, ModePaper, "AAPL", &slate[1].ID)
	for i, orderID := range []int64{dead, traded} {
		if err := s.DecideProposal(ctx, slate[i].ID, ProposalTaken, "", &orderID, proposalTime); err != nil {
			t.Fatalf("DecideProposal %d: %v", i, err)
		}
	}
	settleOrder(t, s, dead, string(broker.StatusExpired), 0)
	settleOrder(t, s, traded, string(broker.StatusFilled), 16)

	ids, err := s.UnfilledForDay(ctx, ModePaper, proposalDay, proposalTime)
	if err != nil {
		t.Fatalf("UnfilledForDay: %v", err)
	}
	if len(ids) != 1 || ids[0] != slate[0].ID {
		t.Fatalf("unfilled = %v, want [%d]", ids, slate[0].ID)
	}
	got, err := s.ProposalsForDay(ctx, ModePaper, proposalDay)
	if err != nil {
		t.Fatalf("ProposalsForDay: %v", err)
	}
	if got[0].Status != ProposalUnfilled || got[1].Status != ProposalTaken {
		t.Fatalf("statuses = %q, %q", got[0].Status, got[1].Status)
	}
}

// --qty lowers the size, so the row has to carry what was traded next to what
// was proposed; otherwise the record reads as a trade that never happened.
func TestDecideTakenRecordsWhatWasTraded(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := fileProposal(t, s, ModePaper, proposalDay, 1, "NVDA")
	orderID := fileOrder(t, s, ModePaper, "NVDA", &p.ID)

	if err := s.DecideTaken(ctx, p.ID, orderID, 4, 6, proposalTime); err != nil {
		t.Fatalf("DecideTaken: %v", err)
	}
	got, err := s.ProposalByDayIndex(ctx, ModePaper, proposalDay, 1)
	if err != nil {
		t.Fatalf("ProposalByDayIndex: %v", err)
	}
	if got.Status != ProposalTaken || got.Qty != 16 {
		t.Fatalf("proposal = %+v, want the proposed size untouched", got)
	}
	if got.TakenQty == nil || *got.TakenQty != 4 {
		t.Fatalf("taken qty = %v, want 4", got.TakenQty)
	}
	if got.TakenRiskUSD == nil || *got.TakenRiskUSD != 6 {
		t.Fatalf("taken risk = %v, want 6", got.TakenRiskUSD)
	}
}
