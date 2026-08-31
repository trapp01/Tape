package trading

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
)

// A bracket rests a stop and a target for the whole position, but only one of
// them can ever trade. Counting both left the shares doubly committed, so the
// trader could not sell a position tape itself had opened.
func TestSellClosesAPositionTheBracketWasHolding(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 100)
	eng, st, _ := newGuardedEngine(t, fb, 20000, risk.Limits{RequireStop: true, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("NVDA", 10, 100)
	stop, target := 98.0, 104.0
	req.StopLoss, req.TakeProfit = &stop, &target
	if _, err := eng.Submit(ctx, req, journal.SourceHuman, ""); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}
	if err := fb.Fill(entryOrderID(t, fb, "NVDA"), 10, 100); err != nil {
		t.Fatalf("filling the entry: %v", err)
	}

	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "NVDA", Side: broker.Sell, Qty: 10}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("a manual exit must never be trapped by its own bracket: %v", err)
	}
	if len(res.Cancelled) != 2 {
		t.Fatalf("cancelled %d resting legs, want the stop and the target", len(res.Cancelled))
	}
	for _, o := range res.Cancelled {
		stored, err := st.OrderByBrokerID(ctx, o.BrokerOrderID)
		if err != nil {
			t.Fatalf("reading cancelled leg %s: %v", o.BrokerOrderID, err)
		}
		if stored.Status != string(broker.StatusCanceled) {
			t.Fatalf("leg %s is %q in the journal, want the cancellation recorded", o.BrokerOrderID, stored.Status)
		}
	}

	held, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(held) != 0 {
		t.Fatalf("the ledger still holds %+v after the exit", held)
	}
}

// Selling more than the ledger holds is still shorting, bracket or no bracket.
func TestSellBeyondHeldIsStillRefusedWithABracket(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 100)
	eng, _, _ := newGuardedEngine(t, fb, 20000, risk.Limits{RequireStop: true, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("NVDA", 10, 100)
	stop := 98.0
	req.StopLoss = &stop
	if _, err := eng.Submit(ctx, req, journal.SourceHuman, ""); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}
	if err := fb.Fill(entryOrderID(t, fb, "NVDA"), 10, 100); err != nil {
		t.Fatalf("filling the entry: %v", err)
	}

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "NVDA", Side: broker.Sell, Qty: 25}, journal.SourceHuman, "")
	if err == nil || !strings.Contains(err.Error(), "rule: no shorting") {
		t.Fatalf("overselling must still be refused, got: %v", err)
	}
}

// The two legs of one bracket are one claim on the shares, not two.
func TestCommittedSellQtyCountsOneLegPerBracket(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 100)
	eng, _, _ := newGuardedEngine(t, fb, 20000, risk.Limits{RequireStop: true, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("NVDA", 10, 100)
	stop, target := 98.0, 104.0
	req.StopLoss, req.TakeProfit = &stop, &target
	if _, err := eng.Submit(ctx, req, journal.SourceHuman, ""); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}

	committed, err := eng.committedSellQty(ctx, "NVDA")
	if err != nil {
		t.Fatalf("committedSellQty: %v", err)
	}
	if committed != 10 {
		t.Fatalf("committed = %d, want the 10 shares one leg can deliver", committed)
	}
}

// The gap the review found twice: an order rests and nothing but eod takes it
// off the books.
func TestCancelOpenByIDAndAll(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 100)
	fb.SetPrice("AAPL", 50)
	eng, st, _ := newGuardedEngine(t, fb, 20000, risk.Limits{})
	ctx := context.Background()

	first, err := eng.Submit(ctx, buyAt("NVDA", 5, 90), journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("resting NVDA buy: %v", err)
	}
	if _, err := eng.Submit(ctx, buyAt("AAPL", 5, 40), journal.SourceHuman, ""); err != nil {
		t.Fatalf("resting AAPL buy: %v", err)
	}

	one, err := eng.CancelOpen(ctx, []int64{first.Order.ID})
	if err != nil {
		t.Fatalf("CancelOpen: %v", err)
	}
	if len(one) != 1 || one[0].Status != string(broker.StatusCanceled) {
		t.Fatalf("cancelled %+v, want the one order recorded as cancelled", one)
	}

	rest, err := eng.CancelOpen(ctx, nil)
	if err != nil {
		t.Fatalf("CancelOpen all: %v", err)
	}
	if len(rest) != 1 {
		t.Fatalf("cancelling everything open touched %d orders, want the 1 left", len(rest))
	}
	open, err := st.ListOrders(ctx, journal.ListFilter{Mode: journal.ModePaper, OpenOnly: true})
	if err != nil {
		t.Fatalf("listing open orders: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("%d orders are still open in the journal: %+v", len(open), open)
	}

	if _, err := eng.CancelOpen(ctx, []int64{first.Order.ID}); err == nil {
		t.Fatal("cancelling an order that is already terminal must say so")
	}
}

// entryOrderID finds the resting entry the venue is holding for a symbol.
func entryOrderID(t *testing.T, fb *fake.Broker, symbol string) string {
	t.Helper()
	orders, err := fb.ListOrders(context.Background(), broker.ListOrdersFilter{OpenOnly: true})
	if err != nil {
		t.Fatalf("listing venue orders: %v", err)
	}
	for _, o := range orders {
		if o.Symbol == symbol && o.Side == broker.Buy {
			return o.ID
		}
	}
	t.Fatalf("no resting entry for %s: %+v", symbol, orders)
	return ""
}
