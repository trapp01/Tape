package trading

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
)

// F2: three back-to-back limit buys of 45 at $100 all fit a $5,000 ledger while
// pending orders reserve nothing. Cash ended at -$8,509.75.
func TestOpenBuyOrdersReserveCash(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	buy45 := func() broker.OrderRequest {
		limit := 100.0
		return broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 45, Type: broker.Limit, LimitPrice: &limit}
	}
	if _, err := eng.Submit(ctx, buy45(), journal.SourceHuman, ""); err != nil {
		t.Fatalf("first buy: %v", err)
	}

	// 45 at the $100.00 limit models to $4,502.25 plus the $1.00 commission floor,
	// which leaves $496.75 of the ledger free.
	for _, attempt := range []string{"second", "third"} {
		_, err := eng.Submit(ctx, buy45(), journal.SourceHuman, "")
		if err == nil {
			t.Fatalf("%s buy was accepted; the resting order already claims the cash", attempt)
		}
		for _, want := range []string{"rule: no overspend", "committed to open orders", "$4,503.25", "$496.75"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s refusal missing %q, got: %v", attempt, want, err)
			}
		}
	}

	led, err := st.Ledger(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if !closeEnough(led.Cash, 5000) {
		t.Fatalf("ledger cash = %v, want the untouched 5000", led.Cash)
	}
}

// F2: a resting sell already promises its shares. Selling the same ten at market
// filled both and took the position to -10.
func TestOpenSellOrdersReserveShares(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("buy: %v", err)
	}
	limit := 110.0
	if _, err := eng.Submit(ctx, broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Sell, Qty: 10, Type: broker.Limit, LimitPrice: &limit,
	}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("resting sell: %v", err)
	}

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 10}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("the market sell was accepted; the resting sell already promises those ten shares")
	}
	for _, want := range []string{"rule: no shorting", "holds 10", "10 already committed", "leaving 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal missing %q, got: %v", want, err)
		}
	}

	positions, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(positions) != 1 || positions[0].Qty != 10 {
		t.Fatalf("positions = %+v, want 10 AAPL still held", positions)
	}
}

// F4: $5,000 cash against a $100.00 ask, buy 50. The raw estimate is exactly
// $5,000 and the fill cost $5,003.50, taking cash to -$3.50.
func TestOverspendEstimateIncludesSlippageAndCommission(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _ := newTestEngine(t, fb, 5000)

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 50}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("50 at $100.00 was accepted, but the modeled fill costs more than the ledger holds")
	}
	for _, want := range []string{"rule: no overspend", "$5,000.00", "$5,003.50"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal missing %q, got: %v", want, err)
		}
	}
}

// F8: the positive-price check only ran for buys, so a sell with a zero limit
// reached the venue.
func TestSellRefusesNonPositiveLimit(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	fb.SubmitErr = errors.New("no order should reach the venue")
	eng, _ := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	for _, limit := range []float64{0, -1} {
		price := limit
		_, err := eng.Submit(ctx, broker.OrderRequest{
			Symbol: "AAPL", Side: broker.Sell, Qty: 1, Type: broker.Limit, LimitPrice: &price,
		}, journal.SourceHuman, "")
		if err == nil || !strings.Contains(err.Error(), "limit price must be positive") {
			t.Fatalf("sell --limit %v: %v", limit, err)
		}
	}
}

// F10: the guardrails read the journal, so Submit reconciles it first. A stop that
// fired since the last command turns the next sell into a short.
func TestSubmitSyncsBeforeTheGuardrails(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	stop, target := 95.0, 110.0
	if _, err := eng.Submit(ctx, broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 10, StopLoss: &stop, TakeProfit: &target,
	}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}

	// The stop fires at the venue and tape has not looked since.
	leg := legOfType(t, st, "stop")
	if err := fb.Fill(leg.BrokerOrderID, 10, 95); err != nil {
		t.Fatalf("filling the stop: %v", err)
	}

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 10}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("the sell was accepted against a position the stop had already closed")
	}
	if !strings.Contains(err.Error(), "rule: no shorting") || !strings.Contains(err.Error(), "holds 0") {
		t.Fatalf("refusal = %v, want no shorting against a flat ledger", err)
	}
}

// legOfType finds the journalled bracket child of the given order type.
func legOfType(t *testing.T, st *journal.Store, kind string) journal.Order {
	t.Helper()
	orders, err := st.ListOrders(context.Background(), journal.ListFilter{Mode: journal.ModePaper})
	if err != nil {
		t.Fatalf("listing orders: %v", err)
	}
	for _, o := range orders {
		if o.Type == kind && strings.HasPrefix(o.Note, "bracket leg of ") {
			return o
		}
	}
	t.Fatalf("no journalled %q bracket leg among %+v", kind, orders)
	return journal.Order{}
}
