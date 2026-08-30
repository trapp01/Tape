package trading

import (
	"context"
	"errors"
	"fmt"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// Flatten cancels resting orders, sells what the tape ledger holds, and journals
// the closes as end-of-day. Per-symbol failures land in Problems and the sweep
// carries on; the caller decides how loudly to say the day did not end flat.
func (e *Engine) Flatten(ctx context.Context) (FlattenReport, error) {
	var rep FlattenReport

	resting, err := e.broker.ListOrders(ctx, broker.ListOrdersFilter{OpenOnly: true})
	if err != nil {
		rep.Problems = append(rep.Problems, fmt.Sprintf("listing open orders: %v", err))
	}
	for _, o := range resting {
		if err := e.broker.CancelOrder(ctx, o.ID); err != nil {
			if errors.Is(err, broker.ErrOrderNotFound) {
				continue
			}
			rep.Problems = append(rep.Problems, fmt.Sprintf("cancelling order %s: %v", o.ID, err))
			continue
		}
		rep.Canceled = append(rep.Canceled, o.ID)
	}

	// The cancels above only reached the venue; this writes them down, so the
	// guardrails see the shares those orders held come free.
	if _, err := e.Sync(ctx); err != nil {
		rep.Problems = append(rep.Problems, fmt.Sprintf("syncing before flatten: %v", err))
	}

	held, err := e.jnl.OpenPositions(ctx, e.mode)
	if err != nil {
		return rep, fmt.Errorf("reading tape positions to flatten: %w", err)
	}
	venue, err := e.broker.Positions(ctx)
	if err != nil {
		return rep, fmt.Errorf("reading broker positions to flatten: %w", err)
	}
	rep.Problems = append(rep.Problems, divergences(held, venue)...)

	for _, p := range held {
		jo, fills, err := e.closeLedgerPosition(ctx, p)
		if err != nil {
			rep.Problems = append(rep.Problems, err.Error())
			continue
		}
		rep.Orders = append(rep.Orders, jo)
		rep.Fills = append(rep.Fills, fills...)
	}

	after, err := e.broker.Positions(ctx)
	if err != nil {
		return rep, fmt.Errorf("reading positions after flatten: %w", err)
	}
	rep.StillOpen = after
	return rep, nil
}

// closeLedgerPosition sells exactly what the ledger holds, through the same
// guardrails a manual exit runs. The venue's own quantity is never journalled: a
// broker holding more would otherwise write the ledger short.
func (e *Engine) closeLedgerPosition(ctx context.Context, p journal.OpenPosition) (journal.Order, []journal.Fill, error) {
	if p.Qty <= 0 {
		return journal.Order{}, nil, fmt.Errorf("the ledger holds %d %s, which eod cannot close (rule: no shorting)", p.Qty, p.Symbol)
	}
	req := broker.OrderRequest{Symbol: p.Symbol, Side: broker.Sell, Qty: p.Qty, Type: broker.Market}
	res, err := e.Submit(ctx, req, journal.SourceEOD, "flattened at end of day")
	if err != nil {
		return journal.Order{}, nil, fmt.Errorf("closing %d %s: %w", p.Qty, p.Symbol, err)
	}
	return res.Order, res.Fills, nil
}

// divergences names every symbol where the venue and the ledger disagree. tape
// trades only what its own record holds, so the rest is the human's to reconcile
// at the broker.
func divergences(ledger []journal.OpenPosition, venue []broker.Position) []string {
	held := make(map[string]int, len(ledger))
	for _, p := range ledger {
		held[p.Symbol] = p.Qty
	}

	var out []string
	seen := make(map[string]bool, len(venue))
	for _, v := range venue {
		seen[v.Symbol] = true
		if v.Qty != held[v.Symbol] {
			out = append(out, fmt.Sprintf("%s: the venue holds %d but the ledger holds %d, a %d share divergence tape will not trade away",
				v.Symbol, v.Qty, held[v.Symbol], v.Qty-held[v.Symbol]))
		}
	}
	for _, p := range ledger {
		if !seen[p.Symbol] {
			out = append(out, fmt.Sprintf("%s: the ledger holds %d but the venue holds none", p.Symbol, p.Qty))
		}
	}
	return out
}
