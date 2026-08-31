package trading

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
)

// Submit runs the guardrails, sends the order, journals it, and waits out the poll
// window for fills. The journal row exists from the moment the venue accepts the
// order, so a crash mid-poll loses timing, never the order itself.
func (e *Engine) Submit(ctx context.Context, req broker.OrderRequest, source, note string) (Result, error) {
	return e.SubmitFor(ctx, req, source, note, nil)
}

// SubmitFor is Submit for an order that executes a proposal, so the record links
// the idea to the fills it produced.
func (e *Engine) SubmitFor(ctx context.Context, req broker.OrderRequest, source, note string, proposalID *int64) (Result, error) {
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	if req.Type == "" {
		req.Type = broker.Market
		if req.LimitPrice != nil {
			req.Type = broker.Limit
		}
	}
	// The guardrails read the journal, so it has to match the venue first: a stop
	// that fired since the last command is the difference between a sell and a short.
	if _, err := e.Sync(ctx); err != nil {
		return Result{}, fmt.Errorf("reconciling the journal before %s %d %s: %w", req.Side, req.Qty, req.Symbol, err)
	}
	cancelled, err := e.freeSharesForExit(ctx, req)
	if err != nil {
		return Result{}, err
	}
	if err := e.check(ctx, req, source); err != nil {
		return Result{Cancelled: cancelled}, err
	}
	if req.ClientOrderID == "" {
		id, err := newClientOrderID(e.now(), proposalID)
		if err != nil {
			return Result{Cancelled: cancelled}, err
		}
		req.ClientOrderID = id
	}

	bo, err := e.broker.SubmitOrder(ctx, req)
	if err != nil {
		return Result{Cancelled: cancelled}, fmt.Errorf("submitting %s %d %s: %w", req.Side, req.Qty, req.Symbol, err)
	}

	jo := journal.Order{
		BrokerOrderID: bo.ID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          string(req.Side),
		Qty:           req.Qty,
		Type:          string(req.Type),
		LimitPrice:    req.LimitPrice,
		StopLoss:      req.StopLoss,
		TakeProfit:    req.TakeProfit,
		Status:        string(bo.Status),
		Source:        source,
		ProposalID:    proposalID,
		Mode:          e.mode,
		Note:          note,
		SubmittedAt:   e.now(),
	}
	if err := e.jnl.InsertOrder(ctx, &jo); err != nil {
		return Result{BrokerOrder: bo, Cancelled: cancelled}, fmt.Errorf("journalling %s %d %s: %w", req.Side, req.Qty, req.Symbol, err)
	}

	fills, final, err := e.settle(ctx, &jo, bo)
	return Result{Order: jo, BrokerOrder: final, Fills: fills, Cancelled: cancelled}, err
}

// settle records what the venue already reported, then polls until the order can
// take no more fills or the window closes. The loop is bounded by time, never by
// status alone: venue statuses outside the normalised set are not terminal here.
func (e *Engine) settle(ctx context.Context, jo *journal.Order, bo broker.Order) ([]journal.Fill, broker.Order, error) {
	fills, err := e.applyWithLegs(ctx, jo, bo)
	if err != nil || bo.Status.Terminal() {
		return fills, bo, err
	}

	pollCtx, cancel := context.WithTimeout(ctx, e.pollWindow)
	defer cancel()
	tick := time.NewTicker(e.pollInterval)
	defer tick.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return fills, bo, ctx.Err()
		case <-tick.C:
		}

		cur, err := e.broker.GetOrder(pollCtx, bo.ID)
		if err != nil {
			if pollCtx.Err() != nil {
				return fills, bo, ctx.Err()
			}
			return fills, bo, fmt.Errorf("polling order %s: %w", bo.ID, err)
		}
		bo = cur
		more, err := e.applyWithLegs(ctx, jo, cur)
		fills = append(fills, more...)
		if err != nil {
			return fills, bo, err
		}
		if cur.Status.Terminal() {
			return fills, bo, nil
		}
	}
}

// applyWithLegs updates the journal row and journals any bracket children the
// venue named, so a stop or target is in the record from birth and is reconciled
// later by its own id even after the parent goes terminal.
func (e *Engine) applyWithLegs(ctx context.Context, jo *journal.Order, bo broker.Order) ([]journal.Fill, error) {
	fills, err := e.applyBrokerOrder(ctx, jo, bo)
	if err != nil {
		return fills, err
	}
	legFills, _, err := e.syncLegs(ctx, *jo, bo.Legs, map[string]bool{})
	return append(fills, legFills...), err
}

// applyBrokerOrder brings the journal row up to the venue's latest view and books
// the newly filled quantity as one fill. Alpaca reports a running total per order
// rather than per execution, so each observed delta becomes a single fill row.
func (e *Engine) applyBrokerOrder(ctx context.Context, jo *journal.Order, bo broker.Order) ([]journal.Fill, error) {
	if bo.ID == "" {
		return nil, fmt.Errorf("venue returned no order id for %s %s", jo.Side, jo.Symbol)
	}
	if jo.ID == 0 {
		return nil, fmt.Errorf("journal order for %s %s is not saved yet", jo.Side, jo.Symbol)
	}

	var out []journal.Fill
	delta := bo.FilledQty - jo.FilledQty
	if delta > 0 {
		// The watermark stays put until the fill can be priced, so the next sync
		// retries the same delta instead of losing it.
		if bo.FilledAvgPrice == nil {
			return out, fmt.Errorf("order %s: venue reports %d of %s %s filled with no average price; "+
				"the journal stays at %d until it reports one", bo.ID, bo.FilledQty, jo.Side, jo.Symbol, jo.FilledQty)
		}
		raw := deltaPrice(*jo, bo, delta)
		if raw <= 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
			return out, fmt.Errorf("order %s: the %d newly filled %s price out at %v, which is not a price; "+
				"the venue's average %v over %d shares contradicts the %d already journalled",
				bo.ID, delta, jo.Symbol, raw, *bo.FilledAvgPrice, bo.FilledQty, jo.FilledQty)
		}
		priced := e.costs.Apply(broker.Side(jo.Side), delta, raw)
		f := journal.Fill{
			OrderID:      jo.ID,
			BrokerFillID: fmt.Sprintf("%s:%d", bo.ID, bo.FilledQty),
			Symbol:       jo.Symbol,
			Side:         jo.Side,
			Qty:          delta,
			RawPrice:     raw,
			ModeledPrice: priced.ModeledPrice,
			Commission:   priced.Commission,
			Fees:         priced.Fees,
			FilledAt:     fillTime(bo, e.now),
		}
		if err := e.jnl.InsertFill(ctx, &f); err != nil {
			return nil, fmt.Errorf("recording fill for order %s: %w", bo.ID, err)
		}
		out = append(out, f)
	}

	if string(bo.Status) == jo.Status && bo.FilledQty == jo.FilledQty {
		return out, nil
	}
	if err := e.jnl.UpdateOrder(ctx, bo.ID, string(bo.Status), bo.FilledQty, bo.FilledAvgPrice); err != nil {
		return out, fmt.Errorf("updating order %s: %w", bo.ID, err)
	}
	jo.Status = string(bo.Status)
	jo.FilledQty = bo.FilledQty
	jo.FilledAvgPrice = bo.FilledAvgPrice
	jo.UpdatedAt = e.now()
	return out, nil
}

// deltaPrice backs the newly filled shares' own average out of the venue's running
// average, so a second partial is not priced at the whole order's blend.
func deltaPrice(jo journal.Order, bo broker.Order, delta int) float64 {
	if jo.FilledQty <= 0 || jo.FilledAvgPrice == nil {
		return *bo.FilledAvgPrice
	}
	prior := *jo.FilledAvgPrice * float64(jo.FilledQty)
	return (*bo.FilledAvgPrice*float64(bo.FilledQty) - prior) / float64(delta)
}

func fillTime(bo broker.Order, now func() time.Time) time.Time {
	if bo.FilledAt != nil && !bo.FilledAt.IsZero() {
		return *bo.FilledAt
	}
	return now()
}

// newClientOrderID correlates the venue's order with the journal row before the
// venue has assigned an id of its own. An order executing a proposal carries the
// proposal in its id, so a live order can be traced back to the idea by hand.
func newClientOrderID(now time.Time, proposalID *int64) (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("generating a client order id: " + err.Error())
	}
	nonce := hex.EncodeToString(b[:])
	if proposalID != nil {
		return fmt.Sprintf("tape-p%d-%s", *proposalID, nonce), nil
	}
	return fmt.Sprintf("tape-%d-%s", now.UnixNano(), nonce), nil
}
