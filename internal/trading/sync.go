package trading

import (
	"context"
	"errors"
	"fmt"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// Sync reconciles every journal order in this mode that can still take a fill.
// Orders the venue has forgotten are reported, not treated as an error, so one
// stale row cannot block the rest of the sweep.
func (e *Engine) Sync(ctx context.Context) (SyncReport, error) {
	open, err := e.jnl.ListOrders(ctx, journal.ListFilter{Mode: e.mode, OpenOnly: true})
	if err != nil {
		return SyncReport{}, fmt.Errorf("listing open orders: %w", err)
	}

	var rep SyncReport
	// A bracket child is both an open row of its own and a leg of its parent; one
	// pass must not apply the same venue delta from both directions.
	seen := make(map[string]bool, len(open))
	for i := range open {
		jo := open[i]
		if jo.BrokerOrderID == "" || seen[jo.BrokerOrderID] {
			continue
		}
		seen[jo.BrokerOrderID] = true
		rep.Checked++

		bo, err := e.broker.GetOrder(ctx, jo.BrokerOrderID)
		if errors.Is(err, broker.ErrOrderNotFound) {
			rep.Missing = append(rep.Missing, jo.BrokerOrderID)
			continue
		}
		if err != nil {
			return rep, fmt.Errorf("fetching order %s: %w", jo.BrokerOrderID, err)
		}

		before := jo.Status
		fills, err := e.applyBrokerOrder(ctx, &jo, bo)
		rep.Fills = append(rep.Fills, fills...)
		if err != nil {
			return rep, err
		}
		if jo.Status != before || len(fills) > 0 {
			rep.Updated = append(rep.Updated, jo)
		}

		legFills, legOrders, err := e.syncLegs(ctx, jo, bo.Legs, seen)
		rep.Fills = append(rep.Fills, legFills...)
		rep.Updated = append(rep.Updated, legOrders...)
		if err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// syncLegs journals bracket children the first time the venue returns them, so a
// stop or target that fills reaches the ledger like any other exit.
func (e *Engine) syncLegs(ctx context.Context, parent journal.Order, legs []broker.Order, seen map[string]bool) ([]journal.Fill, []journal.Order, error) {
	var (
		fills   []journal.Fill
		updated []journal.Order
	)
	for _, leg := range legs {
		if leg.ID == "" || leg.Qty <= 0 || seen[leg.ID] {
			continue
		}
		seen[leg.ID] = true
		jo, err := e.jnl.OrderByBrokerID(ctx, leg.ID)
		switch {
		case errors.Is(err, journal.ErrNotFound):
			jo = journal.Order{
				BrokerOrderID: leg.ID,
				ClientOrderID: leg.ClientOrderID,
				Symbol:        leg.Symbol,
				Side:          string(leg.Side),
				Qty:           leg.Qty,
				Type:          string(leg.Type),
				LimitPrice:    leg.LimitPrice,
				Status:        string(leg.Status),
				Source:        parent.Source,
				Mode:          e.mode,
				Note:          "bracket leg of " + parent.BrokerOrderID,
				SubmittedAt:   leg.SubmittedAt,
			}
			if jo.SubmittedAt.IsZero() {
				jo.SubmittedAt = e.now()
			}
			if err := e.jnl.InsertOrder(ctx, &jo); err != nil {
				return fills, updated, fmt.Errorf("journalling bracket leg %s: %w", leg.ID, err)
			}
		case err != nil:
			return fills, updated, fmt.Errorf("looking up bracket leg %s: %w", leg.ID, err)
		}

		before := jo.Status
		legFills, err := e.applyBrokerOrder(ctx, &jo, leg)
		fills = append(fills, legFills...)
		if err != nil {
			return fills, updated, err
		}
		if jo.Status != before || len(legFills) > 0 {
			updated = append(updated, jo)
		}
	}
	return fills, updated, nil
}
