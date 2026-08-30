package trading

import (
	"context"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
)

func newTestEngine(t *testing.T, fb *fake.Broker, equity float64) (*Engine, *journal.Store) {
	t.Helper()
	st, err := journal.Open(filepath.Join(t.TempDir(), "tape.db"), equity)
	if err != nil {
		t.Fatalf("opening journal: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	eng, err := New(Deps{
		Broker:       fb,
		Data:         fb,
		Journal:      st,
		Costs:        costs.Default(),
		Mode:         journal.ModePaper,
		Loc:          time.UTC,
		PollWindow:   40 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("building engine: %v", err)
	}
	return eng, st
}

func closeEnough(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestSubmitMarketBuyIsJournaledWithCosts(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, st := newTestEngine(t, fb, 5000)
	ctx := context.Background()

	res, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "aapl", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "opening range")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Order.Symbol != "AAPL" {
		t.Fatalf("symbol not uppercased: %q", res.Order.Symbol)
	}
	if want := regexp.MustCompile(`^tape-\d+-[0-9a-f]{4}$`); !want.MatchString(res.Order.ClientOrderID) {
		t.Fatalf("client order id %q does not match tape-<unixnano>-<4 hex>", res.Order.ClientOrderID)
	}
	if len(res.Fills) != 1 {
		t.Fatalf("want 1 fill, got %d", len(res.Fills))
	}

	f := res.Fills[0]
	if f.Qty != 10 || !closeEnough(f.RawPrice, 100) {
		t.Fatalf("fill qty/raw = %d/%v, want 10/100", f.Qty, f.RawPrice)
	}
	if !closeEnough(f.ModeledPrice, 100.05) {
		t.Fatalf("modeled price = %v, want 100.05 (5 bps against the buyer)", f.ModeledPrice)
	}
	if !closeEnough(f.Commission, 1.00) || !closeEnough(f.Fees, 0) {
		t.Fatalf("commission/fees = %v/%v, want 1/0", f.Commission, f.Fees)
	}

	stored, err := st.OrderByClientID(ctx, res.Order.ClientOrderID)
	if err != nil {
		t.Fatalf("reading journal order: %v", err)
	}
	if stored.Status != string(broker.StatusFilled) || stored.FilledQty != 10 {
		t.Fatalf("journal order = %s %d filled, want filled 10", stored.Status, stored.FilledQty)
	}
	if stored.Source != journal.SourceHuman || stored.Note != "opening range" {
		t.Fatalf("journal order source/note = %q/%q", stored.Source, stored.Note)
	}

	led, err := st.Ledger(ctx, journal.ModePaper)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if want := 5000 - 10*100.05 - 1.00; !closeEnough(led.Cash, want) {
		t.Fatalf("ledger cash = %v, want %v", led.Cash, want)
	}
}

func TestSubmitRefusesSellBeyondHeld(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("MSFT", 400)
	eng, _ := newTestEngine(t, fb, 5000)

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "MSFT", Side: broker.Sell, Qty: 5}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("want a refusal, got nil")
	}
	if !strings.Contains(err.Error(), "rule: no shorting") || !strings.Contains(err.Error(), "holds 0") {
		t.Fatalf("refusal must name the rule and the numbers, got: %v", err)
	}
}

func TestSubmitRefusesBuyBeyondCash(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("NVDA", 128.40)
	eng, _ := newTestEngine(t, fb, 1204.10)

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "NVDA", Side: broker.Buy, Qty: 16}, journal.SourceHuman, "")
	if err == nil {
		t.Fatal("want a refusal, got nil")
	}
	// 16 at the $128.41 ask is $2,054.56 raw; the estimate charges 5 bps of
	// slippage and the $1.00 commission floor on top.
	for _, want := range []string{"rule: no overspend", "$1,204.10", "$2,056.59"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal missing %q, got: %v", want, err)
		}
	}
}

func TestSubmitRefusesBadQuantity(t *testing.T) {
	fb := fake.New()
	eng, _ := newTestEngine(t, fb, 5000)

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 0}, journal.SourceHuman, "")
	if err == nil || !strings.Contains(err.Error(), "positive whole number") {
		t.Fatalf("want a positive-quantity refusal, got: %v", err)
	}
}
