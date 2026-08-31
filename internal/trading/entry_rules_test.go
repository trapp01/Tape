package trading

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/risk"
)

// recordingSink stands in for the journal's refusal table.
type recordingSink struct {
	mu  sync.Mutex
	got []journal.Refusal
	err error
}

func (s *recordingSink) Record(_ context.Context, r journal.Refusal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, r)
	return s.err
}

func (s *recordingSink) records() []journal.Refusal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]journal.Refusal(nil), s.got...)
}

func newGuardedEngine(t *testing.T, fb *fake.Broker, equity float64, limits risk.Limits) (*Engine, *journal.Store, *recordingSink) {
	t.Helper()
	st, err := journal.Open(filepath.Join(t.TempDir(), "tape.db"), equity)
	if err != nil {
		t.Fatalf("opening journal: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sink := &recordingSink{}
	eng, err := New(Deps{
		Broker:       fb,
		Data:         fb,
		Journal:      st,
		Costs:        costs.Default(),
		Limits:       limits,
		Refusals:     sink,
		Mode:         journal.ModePaper,
		Loc:          time.UTC,
		PollWindow:   40 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("building engine: %v", err)
	}
	return eng, st, sink
}

// wantRefusal asserts the error names its rule and its numbers, and that the same
// refusal reached the record.
func wantRefusal(t *testing.T, err error, sink *recordingSink, rule, symbol string, numbers ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a %s refusal, got nil", rule)
	}
	if !strings.Contains(err.Error(), "(rule: "+rule+")") {
		t.Fatalf("refusal must name the rule %q, got: %v", rule, err)
	}
	for _, want := range numbers {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal missing %q, got: %v", want, err)
		}
	}

	got := sink.records()
	if len(got) != 1 {
		t.Fatalf("sink holds %d refusals, want 1: %+v", len(got), got)
	}
	r := got[0]
	if r.Rule != rule || r.Symbol != symbol || r.Detail != err.Error() {
		t.Fatalf("recorded %q/%q/%q, want rule %q, symbol %q, and the full message", r.Rule, r.Symbol, r.Detail, rule, symbol)
	}
	if r.Mode != journal.ModePaper || r.Source != journal.SourceHuman || r.Day != market.SessionDate(time.Now()) || r.At.IsZero() {
		t.Fatalf("recorded refusal = %+v, want it stamped paper/human/session", r)
	}
}

func buyAt(symbol string, qty int, limit float64) broker.OrderRequest {
	price := limit
	return broker.OrderRequest{Symbol: symbol, Side: broker.Buy, Qty: qty, Type: broker.Limit, LimitPrice: &price}
}

func TestEntryWithoutAStopIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true})

	_, err := eng.Submit(context.Background(), broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleNoStop, "AAPL", "10 AAPL", "$100.00")
}

func TestStopAboveTheEntryIsRefused(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true})

	req := buyAt("AAPL", 10, 100)
	stop := 105.0
	req.StopLoss = &stop

	_, err := eng.Submit(context.Background(), req, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleNoStop, "AAPL", "$105.00", "$100.00", "not below")
}

// 20 at $100.00 stopping at $98.00 risks $40.00; half a percent of a $5,000
// ledger is $25.00.
func TestEntryOverTheRiskCapIsRefused(t *testing.T) {
	fb := fake.New()
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true, PerTradePct: 0.5})

	req := buyAt("AAPL", 20, 100)
	stop := 98.0
	req.StopLoss = &stop

	_, err := eng.Submit(context.Background(), req, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleRiskCap, "AAPL", "$40.00", "$25.00", "0.5%", "$5,000.00")
}

// Two filled positions plus one resting buy fill a limit of three, and the fourth
// name is refused. Adding to a symbol already in that set takes no new slot.
func TestMaxPositionsCountsHeldAndPending(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	for _, symbol := range []string{"AAPL", "MSFT", "NVDA", "TSLA"} {
		fb.SetPrice(symbol, 10)
	}
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{MaxPositions: 3})
	ctx := context.Background()

	for _, symbol := range []string{"AAPL", "MSFT"} {
		if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: symbol, Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
			t.Fatalf("buying %s: %v", symbol, err)
		}
	}
	if _, err := eng.Submit(ctx, buyAt("NVDA", 10, 5), journal.SourceHuman, ""); err != nil {
		t.Fatalf("resting NVDA buy: %v", err)
	}

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "TSLA", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleMaxPositions, "TSLA", "position 4", "limit of 3", "AAPL, MSFT, NVDA")

	if _, err := eng.Submit(ctx, buyAt("AAPL", 10, 11), journal.SourceHuman, ""); err != nil {
		t.Fatalf("adding to the AAPL position took a new slot: %v", err)
	}
}

func TestAveragingDownIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{})
	ctx := context.Background()

	// The fill is modeled at $100.05, five basis points against the buyer.
	if _, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, ""); err != nil {
		t.Fatalf("opening buy: %v", err)
	}

	_, err := eng.Submit(ctx, buyAt("AAPL", 5, 95), journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleNoAveragingDown, "AAPL", "10 AAPL", "$100.05", "$95.00")

	if _, err := eng.Submit(ctx, buyAt("AAPL", 5, 105), journal.SourceHuman, ""); err != nil {
		t.Fatalf("adding above the average is not averaging down: %v", err)
	}
}

func TestEntryInsideTheCloseWindowIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	fb.MarketOpen = true
	fb.NextClose = time.Now().UTC().Add(20 * time.Minute)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{NoEntriesBeforeCloseMinutes: 30})
	ctx := context.Background()

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleFlatByClose, "AAPL", "20 minutes", "30 minutes")

	// A closed market is not inside the window; the order queues for the open.
	fb.MarketOpen = false
	if _, err := eng.Submit(ctx, buyAt("AAPL", 10, 100), journal.SourceHuman, ""); err != nil {
		t.Fatalf("buy against a closed market: %v", err)
	}
}

// A target below the entry is a losing trade dressed as a bracket, and the
// bracket would rest a sell under the price the moment it is accepted.
func TestTargetBelowTheEntryIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("AAPL", 10, 100)
	stop, target := 98.0, 97.0
	req.StopLoss, req.TakeProfit = &stop, &target

	_, err := eng.Submit(ctx, req, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleTargetSide, "AAPL", "$97.00", "$100.00")
}

// A proposal written at last night's prices is a level, not a plan. An entry the
// tape has left behind is refused before it can rest at the venue.
func TestStaleEntryIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 111.00)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true, PerTradePct: 0.5, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("NVDA", 16, 120.60)
	stop := 119.10
	req.StopLoss = &stop

	// A last of 111.00 is already under the 119.10 stop, so the trade the
	// proposal describes was over before the order could rest.
	_, err := eng.Submit(ctx, req, journal.SourceHuman, "")
	wantRefusal(t, err, sink, ruleStaleEntry, "NVDA", "$111.00", "$119.10", "already through the stop")

	fb.SetPrice("NVDA", 121.00)
	if _, err := eng.Submit(ctx, req, journal.SourceHuman, ""); err != nil {
		t.Fatalf("an entry the tape is still near must be allowed: %v", err)
	}
}

// An entry the tape has left behind, with the stop still intact, is refused on
// the distance alone.
func TestEntryTooFarFromTheLastIsRefused(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("NVDA", 127.50)
	eng, _, sink := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true, MaxEntryDeviationPct: 5})
	ctx := context.Background()

	req := buyAt("NVDA", 16, 120.60)
	stop := 119.10
	req.StopLoss = &stop

	_, err := eng.Submit(ctx, req, journal.SourceHuman, "")
	// 120.60 sits 5.41% below a last of 127.50, over the 5% limit.
	wantRefusal(t, err, sink, ruleStaleEntry, "NVDA", "$120.60", "$127.50", "5.41%")
}

// Sizing rounds against a cap computed from live equity, so the two disagree in
// the last cent. A refusal that prints "$25.00 is over the $25.00 cap" reads as
// a bug and teaches the trader to work around the rule.
func TestRiskCapAllowsTheRoundingCent(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 4999, risk.Limits{RequireStop: true, PerTradePct: 0.5})
	ctx := context.Background()

	// $25.00 risked against a $24.995 cap: half a cent of drift, not a breach.
	req := buyAt("AAPL", 10, 100)
	stop := 97.50
	req.StopLoss = &stop

	if _, err := eng.Submit(ctx, req, journal.SourceHuman, ""); err != nil {
		t.Fatalf("half a cent of equity drift refused the trade: %v", err)
	}
}

// A rule that refuses the idea itself decides the proposal; one that refuses
// today's circumstances does not. take reads this to know which.
func TestRefusalsCarryTheirKind(t *testing.T) {
	intrinsic := []string{ruleValidOrder, ruleNoStop, ruleTargetSide, ruleNoShorting}
	situational := []string{ruleRiskCap, ruleMaxPositions, ruleNoAveragingDown, ruleFlatByClose, ruleDailyHalt, ruleNoOverspend, ruleStaleEntry}

	for _, rule := range intrinsic {
		if situationalRule(rule) {
			t.Errorf("%q is a property of the idea, not of today", rule)
		}
	}
	for _, rule := range situational {
		if !situationalRule(rule) {
			t.Errorf("%q is about today and must not reject the idea forever", rule)
		}
	}
}

func TestRefusalErrorCarriesTheKind(t *testing.T) {
	fb := fake.New()
	fb.Spread = 0
	fb.SetPrice("AAPL", 100)
	eng, _, _ := newGuardedEngine(t, fb, 5000, risk.Limits{RequireStop: true, MaxPositions: 0})
	ctx := context.Background()

	_, err := eng.Submit(ctx, broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 10}, journal.SourceHuman, "")
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("want a RefusalError, got %v", err)
	}
	if refusal.Situational {
		t.Fatalf("%q is intrinsic to the idea, got Situational", refusal.Rule)
	}
}
