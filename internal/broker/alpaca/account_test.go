package alpaca

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

func TestAccount(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/account" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("APCA-API-KEY-ID"); got != "key" {
			t.Errorf("key header = %q", got)
		}
		writeJSON(t, w, http.StatusOK, `{
			"id": "acct-1",
			"equity": "10500.25",
			"cash": "2500.50",
			"buying_power": "21000.50"
		}`)
	})

	acct, err := c.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	want := broker.Account{ID: "acct-1", Equity: 10500.25, Cash: 2500.50, BuyingPower: 21000.50, Paper: true}
	if acct != want {
		t.Fatalf("Account() = %+v, want %+v", acct, want)
	}
}

func TestPositions(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/positions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `[{
			"symbol": "AAPL",
			"qty": "10",
			"avg_entry_price": "190.10",
			"current_price": "195.50",
			"market_value": "1955.00",
			"unrealized_pl": "54.00"
		}]`)
	})

	got, err := c.Positions(context.Background())
	if err != nil {
		t.Fatalf("Positions: %v", err)
	}
	want := []broker.Position{{
		Symbol: "AAPL", Qty: 10, AvgEntryPrice: 190.10,
		CurrentPrice: 195.50, MarketValue: 1955.00, UnrealizedPL: 54.00,
	}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("Positions() = %+v, want %+v", got, want)
	}
}

func TestClock(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/clock" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `{
			"timestamp": "2026-08-30T14:30:00Z",
			"is_open": true,
			"next_open": "2026-08-31T13:30:00Z",
			"next_close": "2026-08-30T20:00:00Z"
		}`)
	})

	clk, err := c.Clock(context.Background())
	if err != nil {
		t.Fatalf("Clock: %v", err)
	}
	if !clk.IsOpen {
		t.Fatal("IsOpen = false, want true")
	}
	if !clk.Now.Equal(time.Date(2026, 8, 30, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("Now = %v", clk.Now)
	}
	if !clk.NextClose.Equal(time.Date(2026, 8, 30, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextClose = %v", clk.NextClose)
	}
}

func TestClosePosition(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/positions/AAPL" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `{"id":"ord-9","symbol":"AAPL","side":"sell",
			"type":"market","status":"new","qty":"10","filled_qty":"0",
			"submitted_at":"2026-08-30T14:30:00Z"}`)
	})

	order, err := c.ClosePosition(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	if order.ID != "ord-9" || order.Side != broker.Sell || order.Qty != 10 {
		t.Fatalf("order = %+v", order)
	}
}

func TestCloseAllPositionsCancelsOrders(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/positions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `[{"symbol":"AAPL","status":200,"body":{
			"id":"ord-10","symbol":"AAPL","side":"sell","type":"market","status":"new",
			"qty":"10","filled_qty":"0","submitted_at":"2026-08-30T14:30:00Z"}}]`)
	})

	orders, err := c.CloseAllPositions(context.Background())
	if err != nil {
		t.Fatalf("CloseAllPositions: %v", err)
	}
	if query.Get("cancel_orders") != "true" {
		t.Fatalf("cancel_orders = %q, want true", query.Get("cancel_orders"))
	}
	if len(orders) != 1 || orders[0].ID != "ord-10" {
		t.Fatalf("orders = %+v", orders)
	}
}
