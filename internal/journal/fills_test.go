package journal

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInsertFillIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	o := &Order{Symbol: "AAPL", Side: "buy", Qty: 10, Status: "filled", Mode: ModePaper}
	if err := s.InsertOrder(ctx, o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	at := time.Date(2026, 8, 24, 15, 30, 0, 0, time.UTC)

	withID := func() *Fill {
		return &Fill{OrderID: o.ID, BrokerFillID: "exec-1", Symbol: "AAPL", Side: "buy",
			Qty: 10, RawPrice: 100, ModeledPrice: 100.05, Commission: 1, FilledAt: at}
	}
	first := withID()
	if err := s.InsertFill(ctx, first); err != nil {
		t.Fatalf("InsertFill: %v", err)
	}
	replay := withID()
	if err := s.InsertFill(ctx, replay); err != nil {
		t.Fatalf("replay InsertFill: %v", err)
	}
	if replay.ID != first.ID {
		t.Errorf("replay ID = %d, want the stored %d", replay.ID, first.ID)
	}

	// Without a venue fill id the execution's own shape has to carry the identity.
	noID := func() *Fill {
		return &Fill{OrderID: o.ID, Symbol: "AAPL", Side: "buy",
			Qty: 10, RawPrice: 100, ModeledPrice: 100.05, Commission: 1, FilledAt: at}
	}
	shaped := noID()
	if err := s.InsertFill(ctx, shaped); err != nil {
		t.Fatalf("InsertFill without broker id: %v", err)
	}
	shapedReplay := noID()
	if err := s.InsertFill(ctx, shapedReplay); err != nil {
		t.Fatalf("replay InsertFill without broker id: %v", err)
	}
	if shapedReplay.ID != shaped.ID {
		t.Errorf("replay ID = %d, want the stored %d", shapedReplay.ID, shaped.ID)
	}

	fills, err := s.Fills(ctx, time.Time{}, time.Time{}, ModePaper)
	if err != nil {
		t.Fatalf("Fills: %v", err)
	}
	if len(fills) != 2 {
		t.Fatalf("got %d fills, want 2 (one per identity)", len(fills))
	}
}

// The same venue fill id under a different order is a collision, not a replay.
// Answering nil there would drop a real execution on the floor.
func TestInsertFillRejectsAReusedBrokerFillID(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	entry := &Order{Symbol: "AAPL", Side: "buy", Qty: 10, Status: "filled", Mode: ModePaper}
	exit := &Order{Symbol: "AAPL", Side: "sell", Qty: 10, Status: "filled", Mode: ModePaper}
	for _, o := range []*Order{entry, exit} {
		if err := s.InsertOrder(ctx, o); err != nil {
			t.Fatalf("InsertOrder: %v", err)
		}
	}
	at := time.Date(2026, 8, 24, 15, 30, 0, 0, time.UTC)

	first := &Fill{OrderID: entry.ID, BrokerFillID: "exec-1", Symbol: "AAPL", Side: "buy",
		Qty: 10, RawPrice: 100, ModeledPrice: 100.05, Commission: 1, FilledAt: at}
	if err := s.InsertFill(ctx, first); err != nil {
		t.Fatalf("InsertFill: %v", err)
	}

	clash := &Fill{OrderID: exit.ID, BrokerFillID: "exec-1", Symbol: "AAPL", Side: "sell",
		Qty: 10, RawPrice: 110, ModeledPrice: 109.95, Commission: 1, FilledAt: at.Add(time.Hour)}
	err := s.InsertFill(ctx, clash)
	if err == nil {
		t.Fatal("a fill id already used by another order was accepted as a replay")
	}
	for _, want := range []string{"exec-1", "already recorded against order"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestInsertFillRequiresAnOrder(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	f := &Fill{OrderID: 999, Symbol: "AAPL", Side: "buy", Qty: 1, RawPrice: 10, ModeledPrice: 10}
	if err := s.InsertFill(ctx, f); err == nil {
		t.Fatal("InsertFill against a missing order succeeded, want a foreign key error")
	}
}

func TestFillsTimeWindow(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 1, 100, day, 1, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 1, 101, day.Add(2*time.Hour), 1, 0.02)

	got, err := s.Fills(ctx, day, day.Add(time.Hour), ModePaper)
	if err != nil {
		t.Fatalf("Fills: %v", err)
	}
	if len(got) != 1 || got[0].Side != "buy" {
		t.Fatalf("window [day, day+1h) returned %+v, want just the buy", got)
	}
	if !got[0].FilledAt.Equal(day) {
		t.Errorf("FilledAt = %v, want %v", got[0].FilledAt, day)
	}
	if got[0].ModeledPrice != 100 || got[0].Commission != 1 {
		t.Errorf("fill = %+v, want modeled 100 commission 1", got[0])
	}
}
