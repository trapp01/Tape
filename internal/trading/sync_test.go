package trading

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/journal"
)

// F1: the venue's stop fired, the account went flat, and the journal never heard.
// GetOrder returned no legs, so tape showed the position open forever, the
// proceeds never reached cash, and the symbol could be sold again.
func TestBracketStopFillReachesTheJournal(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	stop, target := 95.0, 110.0
	res, err := eng.Submit(ctx, broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 10, StopLoss: &stop, TakeProfit: &target,
	}, journal.SourceHuman, "range break")
	if err != nil {
		t.Fatalf("bracket buy: %v", err)
	}
	if len(res.Fills) != 1 || res.Fills[0].Qty != 10 {
		t.Fatalf("entry fills = %+v, want one of 10", res.Fills)
	}

	// Both children are journalled at birth, so each is reconciled by its own id
	// after the parent goes terminal and drops out of the open set.
	stopLeg := legOfType(t, st, "stop")
	if stopLeg.Qty != 10 || stopLeg.Side != string(broker.Sell) {
		t.Fatalf("stop leg = %+v, want a sell of 10", stopLeg)
	}
	if err := fb.Fill(stopLeg.BrokerOrderID, 10, 95); err != nil {
		t.Fatalf("filling the stop: %v", err)
	}

	rep, err := eng.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(rep.Fills) != 1 || rep.Fills[0].Qty != 10 || rep.Fills[0].Side != string(broker.Sell) {
		t.Fatalf("sync fills = %+v, want one sell of 10", rep.Fills)
	}

	positions, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(positions) != 0 {
		t.Fatalf("journal still holds %+v after the stop filled", positions)
	}

	// Entry $100.05 out at $94.9525, less $1.00 in and $1.03 out.
	led, err := st.Ledger(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if !closeEnough(led.RealizedPL, -53.005) {
		t.Fatalf("realized = %v, want -53.005", led.RealizedPL)
	}
	if !closeEnough(led.Cash, 5000-53.005) {
		t.Fatalf("cash = %v, want the proceeds back at %v", led.Cash, 5000-53.005)
	}

	_, err = eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Sell, Qty: 1}, journal.SourceHuman, "")
	if err == nil || !strings.Contains(err.Error(), "rule: no shorting") {
		t.Fatalf("selling a closed position must be refused, got: %v", err)
	}
}

// F5: the venue reported filled quantity with no average price. The fill was
// dropped and the watermark advanced anyway, so it could never be recovered.
func TestFillWithNoAveragePriceIsRetriedNotDropped(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	limit := 99.0
	res, err := eng.Submit(ctx, broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 10, Type: broker.Limit, LimitPrice: &limit,
	}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	jo := res.Order

	silent := broker.Order{
		ID: jo.BrokerOrderID, Symbol: "AAPL", Side: broker.Buy, Qty: 10,
		Status: broker.StatusPartiallyFilled, FilledQty: 4,
	}
	fills, err := eng.applyBrokerOrder(ctx, &jo, silent)
	if err == nil {
		t.Fatal("a filled quantity with no price must be an error, not a silent drop")
	}
	if len(fills) != 0 {
		t.Fatalf("recorded %+v from a fill with no price", fills)
	}
	if jo.FilledQty != 0 {
		t.Fatalf("watermark advanced to %d with nothing journalled", jo.FilledQty)
	}
	stored, err := st.OrderByBrokerID(ctx, jo.BrokerOrderID)
	if err != nil {
		t.Fatalf("reading the order: %v", err)
	}
	if stored.FilledQty != 0 {
		t.Fatalf("stored filled qty = %d, want 0 until the venue reports a price", stored.FilledQty)
	}

	priced := silent
	avg := 99.0
	priced.FilledAvgPrice = &avg
	fills, err = eng.applyBrokerOrder(ctx, &jo, priced)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fills) != 1 || fills[0].Qty != 4 || !closeEnough(fills[0].RawPrice, 99) {
		t.Fatalf("second pass fills = %+v, want one of 4 at 99", fills)
	}
}

// F6: an inconsistent venue average backs out a negative price for the newly
// filled shares, which priced to a negative commission and paid the ledger.
func TestNonPositiveDerivedFillPriceIsRefused(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 200_000)
	ctx := context.Background()

	limit := 100.0
	res, err := eng.Submit(ctx, broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 1000, Type: broker.Limit, LimitPrice: &limit,
	}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := fb.Fill(res.Order.BrokerOrderID, 999, 100); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	if _, err := eng.Sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	before, err := st.Ledger(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	jo, err := st.OrderByBrokerID(ctx, res.Order.BrokerOrderID)
	if err != nil {
		t.Fatalf("reading the order: %v", err)
	}
	avg := 10.0
	bogus := broker.Order{
		ID: jo.BrokerOrderID, Symbol: "AAPL", Side: broker.Buy, Qty: 1000,
		Status: broker.StatusFilled, FilledQty: 1000, FilledAvgPrice: &avg,
	}
	fills, err := eng.applyBrokerOrder(ctx, &jo, bogus)
	if err == nil {
		t.Fatal("a derived price of -$89,900 was journalled as a fill")
	}
	if len(fills) != 0 {
		t.Fatalf("recorded %+v from an impossible price", fills)
	}

	after, err := st.Ledger(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("ledger after: %v", err)
	}
	if !closeEnough(after.Cash, before.Cash) {
		t.Fatalf("cash moved from %v to %v on a refused fill", before.Cash, after.Cash)
	}
	stored, err := st.OrderByBrokerID(ctx, jo.BrokerOrderID)
	if err != nil {
		t.Fatalf("re-reading the order: %v", err)
	}
	if stored.FilledQty != 999 {
		t.Fatalf("watermark advanced to %d on a refused fill", stored.FilledQty)
	}
}

func TestSyncPicksUpALaterFill(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	limit := 99.0
	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10, Type: broker.Limit, LimitPrice: &limit}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(res.Fills) != 0 || res.Order.Status != string(broker.StatusAccepted) {
		t.Fatalf("limit order should rest, got status %s with %d fills", res.Order.Status, len(res.Fills))
	}

	if err := fb.Fill(res.Order.BrokerOrderID, 10, 99); err != nil {
		t.Fatalf("filling at the venue: %v", err)
	}
	rep, err := eng.Sync(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if rep.Checked != 1 || len(rep.Fills) != 1 || rep.Fills[0].Qty != 10 {
		t.Fatalf("sync report = %+v, want one order checked and one fill of 10", rep)
	}

	// A second pass must not double-count the same execution.
	again, err := eng.Sync(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(again.Fills) != 0 {
		t.Fatalf("second sync recorded %d extra fills", len(again.Fills))
	}
	positions, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(positions) != 1 || positions[0].Qty != 10 {
		t.Fatalf("open positions = %+v, want 10 AAPL", positions)
	}
}

func TestSyncRecordsOneFillPerDelta(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, _ := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	limit := 101.0
	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10, Type: broker.Limit, LimitPrice: &limit}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	id := res.Order.BrokerOrderID

	if err := fb.Fill(id, 4, 100); err != nil {
		t.Fatalf("first partial: %v", err)
	}
	first, err := eng.Sync(ctx)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(first.Fills) != 1 || first.Fills[0].Qty != 4 || !closeEnough(first.Fills[0].RawPrice, 100) {
		t.Fatalf("first sync fills = %+v, want one fill of 4 at 100", first.Fills)
	}

	if err := fb.Fill(id, 6, 101); err != nil {
		t.Fatalf("second partial: %v", err)
	}
	second, err := eng.Sync(ctx)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(second.Fills) != 1 || second.Fills[0].Qty != 6 {
		t.Fatalf("second sync fills = %+v, want one fill of 6", second.Fills)
	}
	// The venue reports a running average; the delta must carry its own price.
	if !closeEnough(second.Fills[0].RawPrice, 101) {
		t.Fatalf("delta raw price = %v, want 101", second.Fills[0].RawPrice)
	}
}

func TestFlattenClosesAndJournalsAsEOD(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	limit := 1.0
	resting, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 1, Type: broker.Limit, LimitPrice: &limit}, journal.SourceHuman, "")
	if err != nil {
		t.Fatalf("resting order: %v", err)
	}

	fb.SetPrice("AAPL", 110)
	rep, err := eng.Flatten(ctx)
	if err != nil {
		t.Fatalf("flatten: %v", err)
	}
	if len(rep.Problems) != 0 {
		t.Fatalf("flatten problems: %v", rep.Problems)
	}
	if len(rep.StillOpen) != 0 {
		t.Fatalf("still open after flatten: %+v", rep.StillOpen)
	}
	if len(rep.Canceled) != 1 || rep.Canceled[0] != resting.Order.BrokerOrderID {
		t.Fatalf("canceled = %v, want the resting order %s", rep.Canceled, resting.Order.BrokerOrderID)
	}
	if len(rep.Orders) != 1 || rep.Orders[0].Source != journal.SourceEOD || rep.Orders[0].Side != string(broker.Sell) {
		t.Fatalf("flatten orders = %+v, want one eod sell", rep.Orders)
	}

	open, err := st.OpenPositions(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("open positions: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("tape still holds %+v after flatten", open)
	}
	stored, err := st.OrderByBrokerID(ctx, resting.Order.BrokerOrderID)
	if err != nil {
		t.Fatalf("reading cancelled order: %v", err)
	}
	if stored.Status != string(broker.StatusCanceled) {
		t.Fatalf("resting order status = %q, want canceled", stored.Status)
	}
}

func TestPositionsUnrealized(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, _ := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	fb.SetPrice("AAPL", 110)

	views, err := eng.Positions(ctx)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("want 1 position, got %d", len(views))
	}
	v := views[0]
	if !v.Priced || !closeEnough(v.CurrentPrice, 110) {
		t.Fatalf("current price = %v (priced %v), want 110", v.CurrentPrice, v.Priced)
	}
	if !closeEnough(v.AvgEntryPrice, 100.05) || !closeEnough(v.CostBasis, 1000.50) {
		t.Fatalf("entry/basis = %v/%v, want 100.05/1000.50", v.AvgEntryPrice, v.CostBasis)
	}
	if !closeEnough(v.UnrealizedPL, 99.50) {
		t.Fatalf("unrealized = %v, want 99.50", v.UnrealizedPL)
	}
	if want := 99.50 / 1000.50 * 100; !closeEnough(v.UnrealizedPct, want) {
		t.Fatalf("unrealized pct = %v, want %v", v.UnrealizedPct, want)
	}
}
