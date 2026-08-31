package trading

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
)

func fullLimits() risk.Limits {
	return risk.Limits{
		RequireStop:                 true,
		PerTradePct:                 0.5,
		MaxPositions:                3,
		MaxDailyLosses:              2,
		NoEntriesBeforeCloseMinutes: 30,
	}
}

func TestEntryClearingEveryRuleIsSubmitted(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, st, sink := newGuardedEngine(t, fb, 5000, fullLimits())
	ctx := context.Background()

	stop := 98.0
	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10, StopLoss: &stop}, journal.SourceHuman, "opening range")
	if err != nil {
		t.Fatalf("an entry inside every limit was refused: %v", err)
	}
	if len(res.Fills) != 1 || res.Fills[0].Qty != 10 {
		t.Fatalf("fills = %+v, want one fill of 10", res.Fills)
	}
	if got := sink.records(); len(got) != 0 {
		t.Fatalf("sink holds %+v, want nothing recorded for an accepted order", got)
	}

	stored, err := st.OrderByClientID(ctx, res.Order.ClientOrderID)
	if err != nil {
		t.Fatalf("reading journal order: %v", err)
	}
	if stored.StopLoss == nil || !closeEnough(*stored.StopLoss, 98) {
		t.Fatalf("journal order stop = %v, want 98", stored.StopLoss)
	}
}

// Exits run none of the entry rules: a rule that blocks a sell traps the trader
// in the position it was meant to protect.
func TestExitsIgnoreTheEntryRules(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	limits := fullLimits()
	limits.RequireStop = false
	limits.MaxPositions = 1
	limits.MaxDailyLosses = 1
	eng, _, sink := newGuardedEngine(t, fb, 5000, limits)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("opening buy: %v", err)
	}

	fb.NextClose = time.Now().UTC().Add(10 * time.Minute)
	if _, err := eng.Submit(ctx, buyAt("AAPL", 1, 110), journal.SourceHuman, ""); err == nil {
		t.Fatal("an entry ten minutes before the close was accepted")
	}

	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 10}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("selling ten minutes before the close: %v", err)
	}
	if len(res.Fills) != 1 || res.Fills[0].Qty != 10 {
		t.Fatalf("sell fills = %+v, want one fill of 10", res.Fills)
	}
	if got := sink.records(); len(got) != 1 {
		t.Fatalf("sink holds %d refusals, want only the buy's: %+v", len(got), got)
	}
}

func TestRefusalSinkFailureJoinsTheRefusal(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true})
	sink.err = errors.New("journal is locked")

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("want a refusal, got nil")
	}
	for _, want := range []string{"rule: no entry without a stop", "recording the no entry without a stop refusal", "journal is locked"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q, got: %v", want, err)
		}
	}
}

// Equity is the ledger's cash plus what it holds at tape's own basis. The $1.00
// commission is the whole difference from the $5,000 it started with.
func TestEquityCountsCashAndCostBasis(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 5000, risk.Limits{})
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("buy: %v", err)
	}

	equity, err := eng.Equity(ctx)
	if err != nil {
		t.Fatalf("equity: %v", err)
	}
	if !closeEnough(equity, 4999) {
		t.Fatalf("equity = %v, want 4999 (cash 3998.50 plus a 1000.50 basis)", equity)
	}
}

func TestSubmitForLinksTheProposal(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 5000, risk.Limits{})

	id := int64(42)
	res, err := eng.SubmitFor(context.Background(), broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceProposal, "", &id)
	if err != nil {
		t.Fatalf("submit for proposal 42: %v", err)
	}
	if res.Order.ProposalID == nil || *res.Order.ProposalID != 42 {
		t.Fatalf("journalled order proposal = %v, want 42", res.Order.ProposalID)
	}
}
