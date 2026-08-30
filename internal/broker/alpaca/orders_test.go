package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

func TestSubmitMarketOrder(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/orders" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		writeJSON(t, w, http.StatusOK, `{
			"id": "ord-1",
			"client_order_id": "tape-42",
			"symbol": "AAPL",
			"side": "buy",
			"type": "market",
			"status": "accepted",
			"qty": "10",
			"filled_qty": "0",
			"submitted_at": "2026-08-30T14:30:00Z"
		}`)
	})

	order, err := c.SubmitOrder(context.Background(), broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 10, Type: broker.Market, ClientOrderID: "tape-42",
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	if body["symbol"] != "AAPL" || body["side"] != "buy" || body["type"] != "market" {
		t.Fatalf("body = %+v", body)
	}
	if body["qty"] != "10" {
		t.Fatalf("qty = %v, want \"10\"", body["qty"])
	}
	if body["time_in_force"] != "day" {
		t.Fatalf("time_in_force = %v, want day", body["time_in_force"])
	}
	if body["client_order_id"] != "tape-42" {
		t.Fatalf("client_order_id = %v", body["client_order_id"])
	}
	if body["order_class"] != "" {
		t.Fatalf("order_class = %v, want empty for a plain market order", body["order_class"])
	}
	if body["take_profit"] != nil || body["stop_loss"] != nil {
		t.Fatalf("unexpected bracket legs in body: %+v", body)
	}

	if order.ID != "ord-1" || order.ClientOrderID != "tape-42" || order.Qty != 10 {
		t.Fatalf("order = %+v", order)
	}
	if order.Status != broker.StatusAccepted || order.RawStatus != "accepted" {
		t.Fatalf("status = %q / raw %q, want accepted / accepted", order.Status, order.RawStatus)
	}
	if !order.SubmittedAt.Equal(time.Date(2026, 8, 30, 14, 30, 0, 0, time.UTC)) {
		t.Fatalf("submitted_at = %v", order.SubmittedAt)
	}
}

func TestSubmitBracketOrder(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		writeJSON(t, w, http.StatusOK, `{
			"id": "ord-2",
			"client_order_id": "tape-43",
			"symbol": "AAPL",
			"side": "buy",
			"type": "limit",
			"status": "new",
			"qty": "5",
			"filled_qty": "0",
			"limit_price": "190.00",
			"submitted_at": "2026-08-30T14:30:00Z",
			"legs": [
				{"id": "leg-tp", "symbol": "AAPL", "side": "sell", "type": "limit",
				 "status": "held", "qty": "5", "filled_qty": "0", "limit_price": "200.00",
				 "submitted_at": "2026-08-30T14:30:00Z"},
				{"id": "leg-sl", "symbol": "AAPL", "side": "sell", "type": "stop",
				 "status": "held", "qty": "5", "filled_qty": "0",
				 "submitted_at": "2026-08-30T14:30:00Z"}
			]
		}`)
	})

	limit, tp, sl := 190.0, 200.0, 185.0
	order, err := c.SubmitOrder(context.Background(), broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 5, Type: broker.Limit,
		LimitPrice: &limit, TakeProfit: &tp, StopLoss: &sl, ClientOrderID: "tape-43",
	})
	if err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}

	if body["order_class"] != "bracket" {
		t.Fatalf("order_class = %v, want bracket", body["order_class"])
	}
	if body["limit_price"] != "190" {
		t.Fatalf("limit_price = %v", body["limit_price"])
	}
	if body["client_order_id"] != "tape-43" {
		t.Fatalf("client_order_id = %v", body["client_order_id"])
	}
	takeProfit, ok := body["take_profit"].(map[string]any)
	if !ok || takeProfit["limit_price"] != "200" {
		t.Fatalf("take_profit = %+v, want limit_price 200", body["take_profit"])
	}
	stopLoss, ok := body["stop_loss"].(map[string]any)
	if !ok || stopLoss["stop_price"] != "185" {
		t.Fatalf("stop_loss = %+v, want stop_price 185", body["stop_loss"])
	}

	if order.Status != broker.StatusNew {
		t.Fatalf("status = %q, want new", order.Status)
	}
	if len(order.Legs) != 2 {
		t.Fatalf("legs = %+v, want 2", order.Legs)
	}
	if order.Legs[0].ID != "leg-tp" || order.Legs[1].ID != "leg-sl" {
		t.Fatalf("legs = %+v", order.Legs)
	}
	if order.Legs[1].Type != broker.OrderType("stop") {
		t.Fatalf("stop leg type = %q", order.Legs[1].Type)
	}
	if order.Legs[0].Status != broker.StatusAccepted || order.Legs[0].RawStatus != "held" {
		t.Fatalf("held leg = %q / raw %q, want accepted / held", order.Legs[0].Status, order.Legs[0].RawStatus)
	}
}

// A single protective price is an OTO; Alpaca rejects a bracket missing a leg.
func TestSubmitSingleProtectiveLegUsesOTO(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		writeJSON(t, w, http.StatusOK, `{"id":"ord-3","symbol":"AAPL","side":"buy",
			"type":"market","status":"new","qty":"1","filled_qty":"0",
			"submitted_at":"2026-08-30T14:30:00Z"}`)
	})

	sl := 185.0
	if _, err := c.SubmitOrder(context.Background(), broker.OrderRequest{
		Symbol: "AAPL", Side: broker.Buy, Qty: 1, Type: broker.Market, StopLoss: &sl,
	}); err != nil {
		t.Fatalf("SubmitOrder: %v", err)
	}
	if body["order_class"] != "oto" {
		t.Fatalf("order_class = %v, want oto", body["order_class"])
	}
	if body["take_profit"] != nil {
		t.Fatalf("take_profit = %v, want null", body["take_profit"])
	}
}

func TestSubmitOrderRejectsBadRequests(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the venue")
	})
	price := 10.0

	for _, tc := range []struct {
		name string
		req  broker.OrderRequest
		want string
	}{
		{"no symbol", broker.OrderRequest{Side: broker.Buy, Qty: 1, Type: broker.Market}, "symbol is empty"},
		{"zero qty", broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Type: broker.Market}, "qty must be positive"},
		{"bad side", broker.OrderRequest{Symbol: "AAPL", Side: "hold", Qty: 1, Type: broker.Market}, "side must be"},
		{"limit without price", broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 1, Type: broker.Limit}, "limit order needs a limit price"},
		{"market with price", broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 1, Type: broker.Market, LimitPrice: &price}, "market order cannot carry a limit price"},
		{"bad type", broker.OrderRequest{Symbol: "AAPL", Side: broker.Buy, Qty: 1, Type: "stop"}, "type must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.SubmitOrder(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), "alpaca: submit order ") {
				t.Fatalf("error = %v, want the alpaca prefix with context", err)
			}
		})
	}
}

func TestGetOrder(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/orders/ord-1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, `{
			"id": "ord-1", "symbol": "AAPL", "side": "sell", "type": "limit",
			"status": "partially_filled", "qty": "10", "filled_qty": "4",
			"filled_avg_price": "191.25", "limit_price": "191.00",
			"submitted_at": "2026-08-30T14:30:00Z",
			"filled_at": "2026-08-30T14:31:00Z"
		}`)
	})

	order, err := c.GetOrder(context.Background(), "ord-1")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if order.Status != broker.StatusPartiallyFilled || order.FilledQty != 4 {
		t.Fatalf("order = %+v", order)
	}
	if order.FilledAvgPrice == nil || *order.FilledAvgPrice != 191.25 {
		t.Fatalf("filled_avg_price = %v", order.FilledAvgPrice)
	}
	if order.LimitPrice == nil || *order.LimitPrice != 191.00 {
		t.Fatalf("limit_price = %v", order.LimitPrice)
	}
	if order.FilledAt == nil || !order.FilledAt.Equal(time.Date(2026, 8, 30, 14, 31, 0, 0, time.UTC)) {
		t.Fatalf("filled_at = %v", order.FilledAt)
	}
}

// F1: without nested=true the venue omits Legs, so a stop that fired never
// reaches the journal and the position looks open forever.
func TestGetOrderRequestsNestedLegs(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/orders/ord-1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, `{
			"id": "ord-1", "symbol": "AAPL", "side": "buy", "type": "market",
			"status": "filled", "qty": "5", "filled_qty": "5",
			"filled_avg_price": "190.00", "submitted_at": "2026-08-30T14:30:00Z",
			"legs": [
				{"id": "leg-sl", "symbol": "AAPL", "side": "sell", "type": "stop",
				 "status": "filled", "qty": "5", "filled_qty": "5",
				 "filled_avg_price": "185.00", "submitted_at": "2026-08-30T14:30:00Z"}
			]
		}`)
	})

	order, err := c.GetOrder(context.Background(), "ord-1")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got := query.Get("nested"); got != "true" {
		t.Fatalf("nested = %q, want true", got)
	}
	if len(order.Legs) != 1 || order.Legs[0].ID != "leg-sl" {
		t.Fatalf("legs = %+v, want the stop leg", order.Legs)
	}
	leg := order.Legs[0]
	if leg.Status != broker.StatusFilled || leg.FilledQty != 5 {
		t.Fatalf("leg = %q %d filled, want filled 5", leg.Status, leg.FilledQty)
	}
	if leg.FilledAvgPrice == nil || *leg.FilledAvgPrice != 185 {
		t.Fatalf("leg filled_avg_price = %v, want 185", leg.FilledAvgPrice)
	}
}

func TestGetOrderNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, `{"code":40410000,"message":"order not found"}`)
	})

	_, err := c.GetOrder(context.Background(), "missing")
	if !errors.Is(err, broker.ErrOrderNotFound) {
		t.Fatalf("error = %v, want ErrOrderNotFound", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want the order id in the message", err)
	}
}

func TestListOrdersQueryParams(t *testing.T) {
	after := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name   string
		filter broker.ListOrdersFilter
		want   map[string]string
		absent []string
	}{
		{
			name:   "defaults to all",
			filter: broker.ListOrdersFilter{},
			want:   map[string]string{"status": "all", "nested": "true"},
			absent: []string{"after", "limit"},
		},
		{
			name:   "open only with bounds",
			filter: broker.ListOrdersFilter{After: after, OpenOnly: true, Limit: 25},
			want: map[string]string{
				"status": "open",
				"nested": "true",
				"after":  after.Format(time.RFC3339),
				"limit":  "25",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query url.Values
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v2/orders" {
					t.Errorf("path = %q", r.URL.Path)
				}
				query = r.URL.Query()
				writeJSON(t, w, http.StatusOK, `[{"id":"ord-1","symbol":"AAPL","side":"buy",
					"type":"market","status":"filled","qty":"3","filled_qty":"3",
					"submitted_at":"2026-08-30T14:30:00Z"}]`)
			})

			orders, err := c.ListOrders(context.Background(), tc.filter)
			if err != nil {
				t.Fatalf("ListOrders: %v", err)
			}
			if len(orders) != 1 || orders[0].Status != broker.StatusFilled {
				t.Fatalf("orders = %+v", orders)
			}
			for k, v := range tc.want {
				if got := query.Get(k); got != v {
					t.Errorf("query %s = %q, want %q", k, got, v)
				}
			}
			for _, k := range tc.absent {
				if _, ok := query[k]; ok {
					t.Errorf("query %s should be absent, got %q", k, query.Get(k))
				}
			}
		})
	}
}

func TestCancelOrder(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v2/orders/ord-1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.CancelOrder(context.Background(), "ord-1"); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
}

func TestCancelOrderNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, `{"code":40410000,"message":"order not found"}`)
	})

	err := c.CancelOrder(context.Background(), "missing")
	if !errors.Is(err, broker.ErrOrderNotFound) {
		t.Fatalf("error = %v, want ErrOrderNotFound", err)
	}
}
