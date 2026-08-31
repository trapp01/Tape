package trading

import (
	"context"
	"errors"
	"fmt"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// CancelOpen cancels the given journal orders at the venue and records what the
// venue then reports. An empty ids cancels every order this mode still has open.
func (e *Engine) CancelOpen(ctx context.Context, ids []int64) ([]journal.Order, error) {
	orders, err := e.cancelTargets(ctx, ids)
	if err != nil {
		return nil, err
	}
	return e.cancelOrders(ctx, orders)
}

// cancelTargets resolves what to cancel and refuses an order that can take no
// more fills: cancelling a filled order is a mistake, not a no-op.
func (e *Engine) cancelTargets(ctx context.Context, ids []int64) ([]journal.Order, error) {
	if len(ids) == 0 {
		return e.openOrders(ctx)
	}
	out := make([]journal.Order, 0, len(ids))
	for _, id := range ids {
		o, err := e.jnl.OrderByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if broker.OrderStatus(o.Status).Terminal() {
			return nil, fmt.Errorf("order #%d (%s %d %s) is already %s and can take no more fills", o.ID, o.Side, o.Qty, o.Symbol, o.Status)
		}
		if o.BrokerOrderID == "" {
			return nil, fmt.Errorf("order #%d (%s %d %s) never reached the venue, so there is nothing to cancel", o.ID, o.Side, o.Qty, o.Symbol)
		}
		out = append(out, o)
	}
	return out, nil
}

// cancelOrders sends each cancellation and syncs the journal to whatever the
// venue says afterwards; a leg that filled in the meantime is journalled as filled.
func (e *Engine) cancelOrders(ctx context.Context, orders []journal.Order) ([]journal.Order, error) {
	var done []journal.Order
	for _, o := range orders {
		if o.BrokerOrderID == "" {
			continue
		}
		err := e.broker.CancelOrder(ctx, o.BrokerOrderID)
		if err != nil && !errors.Is(err, broker.ErrOrderNotFound) {
			return done, fmt.Errorf("cancelling order #%d (%s %d %s): %w", o.ID, o.Side, o.Qty, o.Symbol, err)
		}
		bo, err := e.broker.GetOrder(ctx, o.BrokerOrderID)
		if errors.Is(err, broker.ErrOrderNotFound) {
			continue
		}
		if err != nil {
			return done, fmt.Errorf("reading order #%d after cancelling it: %w", o.ID, err)
		}
		row := o
		if _, err := e.applyBrokerOrder(ctx, &row, bo); err != nil {
			return done, err
		}
		done = append(done, row)
	}
	return done, nil
}

// freeSharesForExit takes the resting legs off a symbol so a manual exit can
// reach the shares they are holding. An exit is always allowed: a rule that
// traps the trader in a position is worse than the one it enforces.
func (e *Engine) freeSharesForExit(ctx context.Context, req broker.OrderRequest) ([]journal.Order, error) {
	// An order the validity rules will refuse anyway must not cost a live stop.
	if req.Side != broker.Sell || req.Qty <= 0 || req.Symbol == "" {
		return nil, nil
	}
	if req.LimitPrice != nil && *req.LimitPrice <= 0 {
		return nil, nil
	}
	held, err := e.heldQty(ctx, req.Symbol)
	if err != nil || req.Qty > held {
		return nil, err
	}
	committed, err := e.committedSellQty(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	if req.Qty <= held-committed {
		return nil, nil
	}

	resting, err := e.restingSells(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	cancelled, err := e.cancelOrders(ctx, resting)
	if err != nil {
		return cancelled, err
	}
	return cancelled, nil
}

func (e *Engine) heldQty(ctx context.Context, symbol string) (int, error) {
	positions, err := e.jnl.OpenPositions(ctx, e.mode)
	if err != nil {
		return 0, fmt.Errorf("reading tape positions: %w", err)
	}
	for _, p := range positions {
		if p.Symbol == symbol {
			return p.Qty, nil
		}
	}
	return 0, nil
}
