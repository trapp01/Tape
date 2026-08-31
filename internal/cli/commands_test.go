package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
)

// F1 through the commands: `tape buy --stop --target`, the stop fires at the
// venue, and tape has to see it. Before the fix pos showed the position forever,
// the proceeds never reached cash, and sell would have shorted the symbol.
func TestBracketStopFillFlattensTheJournal(t *testing.T) {
	// The $5 stop on ten shares risks $50, so the ledger has to be big enough for
	// the 0.5% per-trade cap to allow it.
	newHome(t, "11000")
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	useFake(t, fb)

	if _, err := run(t, "buy", "AAPL", "10", "--stop", "95", "--target", "110"); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}

	stopID := restingStopID(t, fb)
	if err := fb.Fill(stopID, 10, 95); err != nil {
		t.Fatalf("filling the stop at the venue: %v", err)
	}

	out, err := run(t, "pos")
	if err != nil {
		t.Fatalf("pos: %v", err)
	}
	if !strings.Contains(out, "flat: the ledger holds nothing.") {
		t.Fatalf("pos must be flat once the stop filled:\n%s", out)
	}

	out, err = run(t, "sell", "AAPL", "1")
	if err == nil || !strings.Contains(err.Error(), "rule: no shorting") {
		t.Fatalf("sell after the stop must be refused, got %v:\n%s", err, out)
	}

	out, err = run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "-$53.0") {
		t.Fatalf("status must carry the realized loss:\n%s", out)
	}
}

// restingStopID finds the bracket's stop leg on the venue's books.
func restingStopID(t *testing.T, fb *fake.Broker) string {
	t.Helper()
	orders, err := fb.ListOrders(context.Background(), broker.ListOrdersFilter{OpenOnly: true})
	if err != nil {
		t.Fatalf("listing venue orders: %v", err)
	}
	for _, o := range orders {
		if o.Side == broker.Sell && o.Type == broker.OrderType("stop") {
			return o.ID
		}
	}
	t.Fatalf("no resting stop leg at the venue: %+v", orders)
	return ""
}

// cancelRestingStop frees the shares a bracket has committed, which is what a
// trader does before closing the position by hand.
func cancelRestingStop(t *testing.T, fb *fake.Broker) {
	t.Helper()
	if err := fb.CancelOrder(context.Background(), restingStopID(t, fb)); err != nil {
		t.Fatalf("cancelling the stop leg: %v", err)
	}
}

// A bracket rests a stop and a target over the whole position, and counting
// both left every share committed. `tape sell` then refused the exit for a
// position tape itself had opened, with no way out but eod.
func TestSellClearsItsOwnBracketLegs(t *testing.T) {
	newHome(t, "11000")
	fb := fake.New()
	fb.SetPrice("NVDA", 100)
	useFake(t, fb)

	if _, err := run(t, "buy", "NVDA", "10", "--stop", "95", "--target", "110"); err != nil {
		t.Fatalf("bracket buy: %v", err)
	}

	out, err := run(t, "sell", "NVDA", "10")
	if err != nil {
		t.Fatalf("the exit must not be trapped by its own bracket: %v", err)
	}
	if !strings.Contains(out, "cancelled 2 resting bracket legs for NVDA") {
		t.Fatalf("sell must say what it took off the books:\n%s", out)
	}

	pos, err := run(t, "pos")
	if err != nil {
		t.Fatalf("pos: %v", err)
	}
	if !strings.Contains(pos, "flat: the ledger holds nothing.") {
		t.Fatalf("the exit did not close the position:\n%s", pos)
	}
}

// The gap the review found twice: an order rests and nothing but eod takes it
// off the books.
func TestCancelByIDAndAll(t *testing.T) {
	newHome(t, "11000")
	fb := fake.New()
	fb.SetPrice("NVDA", 100)
	fb.SetPrice("AAPL", 50)
	useFake(t, fb)
	shortPoll(t)

	if _, err := run(t, "cancel"); err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("cancelling nothing in particular must be refused, got: %v", err)
	}
	if _, err := run(t, "buy", "NVDA", "10", "--limit", "98", "--stop", "96"); err != nil {
		t.Fatalf("resting NVDA buy: %v", err)
	}
	if _, err := run(t, "buy", "AAPL", "10", "--limit", "49", "--stop", "47.50"); err != nil {
		t.Fatalf("resting AAPL buy: %v", err)
	}

	out, err := run(t, "cancel", "1")
	if err != nil {
		t.Fatalf("cancel 1: %v", err)
	}
	if !strings.Contains(out, "cancelled 1 order(s)") || !strings.Contains(out, "NVDA") {
		t.Fatalf("cancel output:\n%s", out)
	}

	out, err = run(t, "cancel", "--all")
	if err != nil {
		t.Fatalf("cancel --all: %v", err)
	}
	if !strings.Contains(out, "AAPL") {
		t.Fatalf("cancel --all must reach the order still working:\n%s", out)
	}

	open, err := run(t, "orders", "--open")
	if err != nil {
		t.Fatalf("orders --open: %v", err)
	}
	if strings.Contains(open, "NVDA") || strings.Contains(open, "AAPL") {
		t.Fatalf("orders are still open after cancelling everything:\n%s", open)
	}
	if _, err := run(t, "cancel", "1"); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("cancelling a terminal order must say so, got: %v", err)
	}
}

func TestBuySellPosAndEOD(t *testing.T) {
	newHome(t, "5000")
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	useFake(t, fb)

	out, err := run(t, "buy", "aapl", "10", "--stop", "98", "--note", "range break")
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	if !strings.HasPrefix(out, "[paper] buy 10 AAPL\n") {
		t.Fatalf("buy banner:\n%s", out)
	}
	for _, want := range []string{"$100.00", "$100.05", "$1.00", "journal id"} {
		if !strings.Contains(out, want) {
			t.Fatalf("buy output missing %q:\n%s", want, out)
		}
	}

	fb.SetPrice("AAPL", 110)
	out, err = run(t, "pos")
	if err != nil {
		t.Fatalf("pos: %v", err)
	}
	for _, want := range []string{"AAPL", "$1,000.50", "+$99.50", "+9.95%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pos output missing %q:\n%s", want, out)
		}
	}

	// The bracket's stop leg has all ten shares spoken for, so it comes off the
	// books before any of them can be sold by hand.
	cancelRestingStop(t, fb)
	if _, err := run(t, "sell", "AAPL", "4"); err != nil {
		t.Fatalf("sell: %v", err)
	}
	out, err = run(t, "sell", "AAPL", "99")
	if err == nil || !strings.Contains(err.Error(), "rule: no shorting") {
		t.Fatalf("oversell must be refused by name, got %v:\n%s", err, out)
	}

	out, err = run(t, "orders")
	if err != nil {
		t.Fatalf("orders: %v", err)
	}
	if !strings.Contains(out, "SYMBOL") || !strings.Contains(out, "human") {
		t.Fatalf("orders output:\n%s", out)
	}

	out, err = run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	for _, want := range []string{"[paper] end of day", "Flatten", "Recap", "flat."} {
		if !strings.Contains(out, want) {
			t.Fatalf("eod output missing %q:\n%s", want, out)
		}
	}
}

// F7: the fill row added a sell's costs to its proceeds and headed the column
// COST BASIS, so an exit read $2.06 richer than the ledger booked.
func TestSellFillRowShowsNetProceeds(t *testing.T) {
	newHome(t, "5000")
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	useFake(t, fb)

	out, err := run(t, "buy", "AAPL", "10", "--stop", "98")
	if err != nil {
		t.Fatalf("buy: %v", err)
	}
	// 10 at $100.05 modeled is $1,000.50, and the buyer pays the $1.00 commission.
	if !strings.Contains(out, "COST") || !strings.Contains(out, "$1,001.50") {
		t.Fatalf("buy fill row must total the cost:\n%s", out)
	}

	cancelRestingStop(t, fb)
	out, err = run(t, "sell", "AAPL", "10")
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	if strings.Contains(out, "COST BASIS") {
		t.Fatalf("a sell has no cost basis:\n%s", out)
	}
	// 10 at $99.95 modeled is $999.50, less $1.00 commission and $0.03 of fees.
	if !strings.Contains(out, "NET") || !strings.Contains(out, "$998.47") {
		t.Fatalf("sell fill row must net the costs out of the proceeds:\n%s", out)
	}
}

func TestStatusShowsLedgerAndLabelsBrokerBalance(t *testing.T) {
	newHome(t, "5000")
	fb := fake.New()
	useFake(t, fb)

	out, err := run(t, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"[paper] status", "starting equity", "$5,000.00", "ignored by stats", "Market"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestBuyBeyondCashIsRefusedWithNumbers(t *testing.T) {
	newHome(t, "500")
	fb := fake.New()
	fb.SetPrice("NVDA", 128.40)
	useFake(t, fb)

	// A stop this tight keeps the entry inside the risk cap, so the refusal that
	// fires is the one about cash.
	_, err := run(t, "buy", "NVDA", "16", "--stop", "128.30")
	if err == nil {
		t.Fatal("want a refusal")
	}
	for _, want := range []string{"buy 16 NVDA", "rule: no overspend", "$500.00", "$2,056.59"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal missing %q, got: %v", want, err)
		}
	}
}

func TestStatusWithoutKeysExplainsHowToGetThem(t *testing.T) {
	newHome(t, "5000")
	t.Setenv("ALPACA_API_KEY", "")
	t.Setenv("ALPACA_API_SECRET", "")

	_, err := run(t, "status")
	if err == nil {
		t.Fatal("status without keys must fail")
	}
	if !strings.Contains(err.Error(), "ALPACA_API_KEY") || !strings.Contains(err.Error(), "app.alpaca.markets") {
		t.Fatalf("missing-keys error must say what to do, got: %v", err)
	}
}
