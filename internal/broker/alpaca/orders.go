package alpaca

import (
	"context"
	"fmt"

	sdk "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/shopspring/decimal"

	"github.com/trapp01/tape/internal/broker"
)

// SubmitOrder places a day order. StopLoss and TakeProfit together make it a bracket;
// one alone makes it an OTO, because Alpaca rejects a bracket with a missing leg.
func (c *Client) SubmitOrder(ctx context.Context, req broker.OrderRequest) (broker.Order, error) {
	what := fmt.Sprintf("submit order %s %s %d", req.Symbol, req.Side, req.Qty)

	placeReq, err := placeOrderRequest(req)
	if err != nil {
		return broker.Order{}, fmt.Errorf("alpaca: %s: %w", what, err)
	}
	order, err := call(ctx, func() (*sdk.Order, error) {
		return c.trading.PlaceOrder(placeReq)
	})
	if err != nil {
		return broker.Order{}, fmt.Errorf("alpaca: %s: %w", what, err)
	}
	return toOrder(*order), nil
}

// GetOrder asks for nested results so a bracket's stop and target arrive with
// their parent, the way ListOrders does.
func (c *Client) GetOrder(ctx context.Context, id string) (broker.Order, error) {
	order, err := c.getOrderNested(ctx, id)
	if err != nil {
		return broker.Order{}, fmt.Errorf("alpaca: get order %s: %w", id, err)
	}
	return toOrder(order), nil
}

// ListOrders asks for nested results so bracket children arrive with their parent.
func (c *Client) ListOrders(ctx context.Context, f broker.ListOrdersFilter) ([]broker.Order, error) {
	listReq := sdk.GetOrdersRequest{Status: "all", Nested: true}
	if f.OpenOnly {
		listReq.Status = "open"
	}
	if !f.After.IsZero() {
		listReq.After = f.After
	}
	if f.Limit > 0 {
		listReq.Limit = f.Limit
	}

	raw, err := call(ctx, func() ([]sdk.Order, error) {
		return c.trading.GetOrders(listReq)
	})
	if err != nil {
		return nil, fmt.Errorf("alpaca: list orders: %w", err)
	}
	out := make([]broker.Order, 0, len(raw))
	for _, o := range raw {
		out = append(out, toOrder(o))
	}
	return out, nil
}

func (c *Client) CancelOrder(ctx context.Context, id string) error {
	_, err := call(ctx, func() (struct{}, error) {
		return struct{}{}, c.trading.CancelOrder(id)
	})
	if err != nil {
		if notFound(err) {
			return fmt.Errorf("alpaca: cancel order %s: %w", id, broker.ErrOrderNotFound)
		}
		return fmt.Errorf("alpaca: cancel order %s: %w", id, err)
	}
	return nil
}

func placeOrderRequest(req broker.OrderRequest) (sdk.PlaceOrderRequest, error) {
	if req.Symbol == "" {
		return sdk.PlaceOrderRequest{}, fmt.Errorf("symbol is empty")
	}
	if req.Qty <= 0 {
		return sdk.PlaceOrderRequest{}, fmt.Errorf("qty must be positive, got %d", req.Qty)
	}
	switch req.Side {
	case broker.Buy, broker.Sell:
	default:
		return sdk.PlaceOrderRequest{}, fmt.Errorf("side must be %q or %q, got %q", broker.Buy, broker.Sell, req.Side)
	}
	switch req.Type {
	case broker.Market:
		if req.LimitPrice != nil {
			return sdk.PlaceOrderRequest{}, fmt.Errorf("market order cannot carry a limit price")
		}
	case broker.Limit:
		if req.LimitPrice == nil {
			return sdk.PlaceOrderRequest{}, fmt.Errorf("limit order needs a limit price")
		}
	default:
		return sdk.PlaceOrderRequest{}, fmt.Errorf("type must be %q or %q, got %q", broker.Market, broker.Limit, req.Type)
	}

	qty := decimal.NewFromInt(int64(req.Qty))
	out := sdk.PlaceOrderRequest{
		Symbol:        req.Symbol,
		Qty:           &qty,
		Side:          sdk.Side(req.Side),
		Type:          sdk.OrderType(req.Type),
		TimeInForce:   sdk.Day,
		LimitPrice:    priceDecimal(req.LimitPrice),
		ClientOrderID: req.ClientOrderID,
	}

	switch {
	case req.TakeProfit != nil && req.StopLoss != nil:
		out.OrderClass = sdk.Bracket
	case req.TakeProfit != nil || req.StopLoss != nil:
		out.OrderClass = sdk.OTO
	}
	if req.TakeProfit != nil {
		out.TakeProfit = &sdk.TakeProfit{LimitPrice: priceDecimal(req.TakeProfit)}
	}
	if req.StopLoss != nil {
		out.StopLoss = &sdk.StopLoss{StopPrice: priceDecimal(req.StopLoss)}
	}
	return out, nil
}

// mapStatus normalises Alpaca's order vocabulary. Anything unrecognised counts as
// accepted so a live order is never mistaken for a terminal one; Order.RawStatus
// keeps the original string.
func mapStatus(raw string) broker.OrderStatus {
	switch raw {
	case "new", "pending_new", "accepted_for_bidding":
		return broker.StatusNew
	case "accepted":
		return broker.StatusAccepted
	case "partially_filled":
		return broker.StatusPartiallyFilled
	case "filled":
		return broker.StatusFilled
	case "canceled", "replaced":
		// A replacement carries a new venue id tape never learns about, so the row
		// it replaces takes no further fills. RawStatus keeps "replaced".
		return broker.StatusCanceled
	case "rejected":
		return broker.StatusRejected
	case "expired", "done_for_day":
		// tape submits day orders only, so done_for_day can take no further fills.
		return broker.StatusExpired
	default:
		return broker.StatusAccepted
	}
}

// toOrder converts one order and its bracket children. Leg types such as "stop" have
// no broker constant and pass through as their raw string.
func toOrder(o sdk.Order) broker.Order {
	out := broker.Order{
		ID:             o.ID,
		ClientOrderID:  o.ClientOrderID,
		Symbol:         o.Symbol,
		Side:           broker.Side(o.Side),
		Type:           broker.OrderType(o.Type),
		LimitPrice:     decimalPtr(o.LimitPrice),
		Status:         mapStatus(o.Status),
		RawStatus:      o.Status,
		FilledQty:      int(o.FilledQty.IntPart()),
		FilledAvgPrice: decimalPtr(o.FilledAvgPrice),
		SubmittedAt:    o.SubmittedAt,
		FilledAt:       o.FilledAt,
	}
	if o.Qty != nil {
		out.Qty = int(o.Qty.IntPart())
	}
	for _, leg := range o.Legs {
		out.Legs = append(out.Legs, toOrder(leg))
	}
	return out
}

func priceDecimal(f *float64) *decimal.Decimal {
	if f == nil {
		return nil
	}
	d := decimal.NewFromFloat(*f)
	return &d
}
