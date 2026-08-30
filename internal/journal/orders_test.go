package journal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInsertUpdateFetchOrder(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	limit := 101.25
	o := &Order{
		BrokerOrderID: "brk-1",
		ClientOrderID: "cli-1",
		Symbol:        "AAPL",
		Side:          "buy",
		Qty:           10,
		Type:          "limit",
		LimitPrice:    &limit,
		Status:        "new",
		Source:        SourceProposal,
		Mode:          ModePaper,
		Note:          "breakout retest",
	}
	if err := s.InsertOrder(ctx, o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	if o.ID == 0 {
		t.Fatal("InsertOrder left ID at zero")
	}
	if o.SubmittedAt.IsZero() || o.UpdatedAt.IsZero() {
		t.Fatal("InsertOrder left the timestamps at zero")
	}

	avg := 101.10
	if err := s.UpdateOrder(ctx, "brk-1", "filled", 10, &avg); err != nil {
		t.Fatalf("UpdateOrder: %v", err)
	}

	got, err := s.OrderByBrokerID(ctx, "brk-1")
	if err != nil {
		t.Fatalf("OrderByBrokerID: %v", err)
	}
	if got.Status != "filled" || got.FilledQty != 10 {
		t.Errorf("status/filled = %q/%d, want filled/10", got.Status, got.FilledQty)
	}
	if got.FilledAvgPrice == nil || *got.FilledAvgPrice != avg {
		t.Errorf("FilledAvgPrice = %v, want %v", got.FilledAvgPrice, avg)
	}
	if got.LimitPrice == nil || *got.LimitPrice != limit {
		t.Errorf("LimitPrice = %v, want %v", got.LimitPrice, limit)
	}
	if got.Note != "breakout retest" || got.Source != SourceProposal {
		t.Errorf("note/source = %q/%q", got.Note, got.Source)
	}
	if !got.SubmittedAt.Equal(o.SubmittedAt.UTC()) {
		t.Errorf("SubmittedAt = %v, want %v", got.SubmittedAt, o.SubmittedAt.UTC())
	}
	if !got.UpdatedAt.After(got.SubmittedAt) && !got.UpdatedAt.Equal(got.SubmittedAt) {
		t.Errorf("UpdatedAt %v predates SubmittedAt %v", got.UpdatedAt, got.SubmittedAt)
	}

	byClient, err := s.OrderByClientID(ctx, "cli-1")
	if err != nil {
		t.Fatalf("OrderByClientID: %v", err)
	}
	if byClient.ID != o.ID {
		t.Errorf("OrderByClientID returned id %d, want %d", byClient.ID, o.ID)
	}
}

func TestOrderLookupNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	if _, err := s.OrderByBrokerID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("OrderByBrokerID error = %v, want ErrNotFound", err)
	}
	if _, err := s.OrderByClientID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("OrderByClientID error = %v, want ErrNotFound", err)
	}
	if err := s.UpdateOrder(ctx, "missing", "filled", 1, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateOrder error = %v, want ErrNotFound", err)
	}
}

func TestInsertOrderRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	tests := []struct {
		name  string
		order Order
	}{
		{"no symbol", Order{Side: "buy", Qty: 1, Mode: ModePaper}},
		{"zero qty", Order{Symbol: "AAPL", Side: "buy", Mode: ModePaper}},
		{"bad side", Order{Symbol: "AAPL", Side: "long", Qty: 1, Mode: ModePaper}},
		{"no mode", Order{Symbol: "AAPL", Side: "buy", Qty: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.order
			if err := s.InsertOrder(ctx, &o); err == nil {
				t.Fatal("InsertOrder succeeded, want an error")
			}
		})
	}
}

func TestListOrders(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	mk := func(symbol, status, mode string, at time.Time) {
		o := &Order{Symbol: symbol, Side: "buy", Qty: 1, Status: status, Mode: mode, SubmittedAt: at}
		if err := s.InsertOrder(ctx, o); err != nil {
			t.Fatalf("InsertOrder %s: %v", symbol, err)
		}
	}
	mk("AAPL", "filled", ModePaper, day)
	mk("MSFT", "new", ModePaper, day.Add(time.Hour))
	mk("NVDA", "accepted", ModeLive, day.Add(2*time.Hour))
	mk("TSLA", "canceled", ModePaper, day.Add(-48*time.Hour))

	all, err := s.ListOrders(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("got %d orders, want 4", len(all))
	}
	if all[0].Symbol != "NVDA" {
		t.Errorf("first order = %s, want NVDA (newest first)", all[0].Symbol)
	}

	paper, err := s.ListOrders(ctx, ListFilter{Mode: ModePaper})
	if err != nil {
		t.Fatalf("ListOrders paper: %v", err)
	}
	if len(paper) != 3 {
		t.Errorf("got %d paper orders, want 3", len(paper))
	}

	open, err := s.ListOrders(ctx, ListFilter{OpenOnly: true})
	if err != nil {
		t.Fatalf("ListOrders open: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("got %d open orders, want 2 (new, accepted)", len(open))
	}

	since, err := s.ListOrders(ctx, ListFilter{Since: day})
	if err != nil {
		t.Fatalf("ListOrders since: %v", err)
	}
	if len(since) != 3 {
		t.Errorf("got %d orders since %v, want 3", len(since), day)
	}

	limited, err := s.ListOrders(ctx, ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListOrders limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("got %d orders, want 2", len(limited))
	}
}
