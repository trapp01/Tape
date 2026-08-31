package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
)

// crashedTake reproduces the window the review found: the venue took the order
// and the journal recorded it, and the process died before the decision landed.
func crashedTake(t *testing.T, fb *fake.Broker) (journal.Proposal, string) {
	t.Helper()
	ctx := context.Background()
	st, cfg := openTestJournal(t)
	p := proposalRow(t, 1)

	entry := p.Entry
	bo, err := fb.SubmitOrder(ctx, broker.OrderRequest{
		Symbol: p.Symbol, Side: broker.Buy, Qty: p.Qty, Type: broker.Limit, LimitPrice: &entry,
	})
	if err != nil {
		t.Fatalf("submitting the crashed run's order: %v", err)
	}
	o := &journal.Order{
		BrokerOrderID: bo.ID,
		ClientOrderID: "tape-p1-crash",
		Symbol:        p.Symbol,
		Side:          string(broker.Buy),
		Qty:           p.Qty,
		Type:          string(broker.Limit),
		LimitPrice:    &entry,
		Status:        string(bo.Status),
		Source:        journal.SourceProposal,
		ProposalID:    &p.ID,
		Mode:          cfg.Mode,
		SubmittedAt:   timeNow().UTC(),
	}
	if err := st.InsertOrder(ctx, o); err != nil {
		t.Fatalf("journalling the crashed run's order: %v", err)
	}
	if err := st.ClaimProposal(ctx, p.ID, timeNow().UTC()); err != nil {
		t.Fatalf("claiming the proposal: %v", err)
	}
	return p, bo.ID
}

// A claimed proposal is one an order may already exist for. Taking it again is
// how the same idea reaches the venue twice.
func TestTakeRefusesAClaimedProposal(t *testing.T) {
	fb := newSlateHome(t)
	crashedTake(t, fb)

	out, err := run(t, "take", "1")
	if err == nil || !strings.Contains(err.Error(), "may already be live") {
		t.Fatalf("taking a claimed proposal must be refused, got %v:\n%s", err, out)
	}
	if n := venueOrderCount(t, fb, "NVDA", broker.Buy); n != 1 {
		t.Fatalf("the venue holds %d NVDA entries, want the 1 the crashed run sent", n)
	}
}

// Reconciliation closes the crash window from the record, without sending
// anything: the order is already the trade.
func TestProposalsReconcileClosesACrashedTake(t *testing.T) {
	fb := newSlateHome(t)
	_, brokerID := crashedTake(t, fb)

	out, err := run(t, "proposals", "--reconcile")
	if err != nil {
		t.Fatalf("proposals --reconcile: %v", err)
	}
	if !strings.Contains(out, "1 proposal(s) closed out") {
		t.Fatalf("reconcile must say what it closed:\n%s", out)
	}

	p := proposalRow(t, 1)
	if p.Status != journal.ProposalTaken || p.OrderID == nil {
		t.Fatalf("proposal 1 = %+v, want taken linked to the order that reached the venue", p)
	}
	if n := venueOrderCount(t, fb, "NVDA", broker.Buy); n != 1 {
		t.Fatalf("reconciling sent a second order; the venue holds %d", n)
	}
	if _, err := run(t, "take", "1"); err == nil || !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("a reconciled proposal is decided, got: %v", err)
	}
	_ = brokerID
}

// A venue that will not answer decides nothing, and the order may still be
// live. The claim stays, and the command says what to run.
func TestTakeLeavesTheClaimWhenTheVenueFails(t *testing.T) {
	fb := newSlateHome(t)
	fb.SubmitErr = errors.New("gateway timeout")

	out, err := run(t, "take", "1")
	if err == nil {
		t.Fatal("a venue failure must fail the command")
	}
	if !strings.Contains(out, "order may be live for proposal #1") || !strings.Contains(out, "--reconcile") {
		t.Fatalf("take must say the order may be live:\n%s", out)
	}
	if p := proposalRow(t, 1); p.Status != journal.ProposalSubmitting {
		t.Fatalf("proposal 1 = %s, want the claim standing", p.Status)
	}
	if _, err := run(t, "take", "1"); err == nil || !strings.Contains(err.Error(), "may already be live") {
		t.Fatalf("a standing claim must refuse a second take, got: %v", err)
	}
}

// --qty lowers what is traded, and the row kept the sized quantity, so the
// record read as a 16-share trade the account never made.
func TestTakeRecordsTheQuantityActuallyTraded(t *testing.T) {
	newSlateHome(t)

	if _, err := run(t, "take", "1", "--qty", "4"); err != nil {
		t.Fatalf("take 1 --qty 4: %v", err)
	}

	p := proposalRow(t, 1)
	if p.Qty != 16 {
		t.Fatalf("the sized quantity changed to %d; the slate said 16", p.Qty)
	}
	if p.TakenQty == nil || *p.TakenQty != 4 {
		t.Fatalf("traded qty = %v, want 4", p.TakenQty)
	}
	if p.TakenRiskUSD == nil || *p.TakenRiskUSD != 6 {
		t.Fatalf("traded risk = %v, want the $6.00 four shares risk", p.TakenRiskUSD)
	}

	list, err := run(t, "proposals")
	if err != nil {
		t.Fatalf("proposals: %v", err)
	}
	if !strings.Contains(list, "16→4") || !strings.Contains(list, "$24.00→$6.00") {
		t.Fatalf("the table must show what was traded next to what was sized:\n%s", list)
	}

	why, err := run(t, "why", "1")
	if err != nil {
		t.Fatalf("why 1: %v", err)
	}
	if !strings.Contains(why, "traded") || !strings.Contains(why, "4 sh of the 16 sized") {
		t.Fatalf("why must show the traded size:\n%s", why)
	}
}

// venueOrderCount counts the venue's orders for a symbol on one side.
func venueOrderCount(t *testing.T, fb *fake.Broker, symbol string, side broker.Side) int {
	t.Helper()
	orders, err := fb.ListOrders(context.Background(), broker.ListOrdersFilter{})
	if err != nil {
		t.Fatalf("listing venue orders: %v", err)
	}
	n := 0
	for _, o := range orders {
		if o.Symbol == symbol && o.Side == side {
			n++
		}
	}
	return n
}
