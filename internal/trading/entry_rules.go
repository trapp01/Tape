package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

// capTolerance is the drift the risk cap forgives, in dollars. Sizing rounds
// against an equity figure the ledger has already moved past.
const capTolerance = 0.005

// checkEntry runs the co-pilot guardrails every buy clears before it can reach a
// venue. Sells are exits and run none of them: a rule that blocks an exit traps
// the trader in a position.
func (e *Engine) checkEntry(ctx context.Context, at attempt) error {
	price, err := e.entryPrice(ctx, at.req)
	if err != nil {
		return err
	}
	at.price = price
	if at.held, err = e.jnl.OpenPositions(ctx, e.mode); err != nil {
		return fmt.Errorf("reading tape positions: %w", err)
	}

	for _, rule := range []func(context.Context, attempt) error{
		e.checkRequireStop,
		e.checkTargetSide,
		e.checkStaleEntry,
		e.checkRiskCap,
		e.checkMaxPositions,
		e.checkAveragingDown,
		e.checkFlatByClose,
		e.checkDailyHalt,
		e.checkOverspend,
	} {
		if err := rule(ctx, at); err != nil {
			return err
		}
	}
	return nil
}

// checkRequireStop keeps every entry carrying its exit. Without a stop below the
// entry the loss is whatever the market decides.
func (e *Engine) checkRequireStop(ctx context.Context, at attempt) error {
	if !e.limits.RequireStop {
		return nil
	}
	switch {
	case at.req.StopLoss == nil:
		return e.refuse(ctx, at, ruleNoStop, fmt.Sprintf("%d %s at %s carries no stop, so nothing bounds the loss",
			at.req.Qty, at.req.Symbol, usd(at.price)))
	case *at.req.StopLoss <= 0:
		return e.refuse(ctx, at, ruleNoStop, fmt.Sprintf("the stop on %d %s must be positive, got %s",
			at.req.Qty, at.req.Symbol, usd(*at.req.StopLoss)))
	case *at.req.StopLoss >= at.price:
		return e.refuse(ctx, at, ruleNoStop, fmt.Sprintf("the %s stop on %d %s is not below the %s entry",
			usd(*at.req.StopLoss), at.req.Qty, at.req.Symbol, usd(at.price)))
	}
	return nil
}

// checkTargetSide keeps a bracket's profit leg above the price it is opened at.
// A target under the entry rests a sell below the market the moment it lands.
func (e *Engine) checkTargetSide(ctx context.Context, at attempt) error {
	if at.req.TakeProfit == nil || *at.req.TakeProfit > at.price {
		return nil
	}
	return e.refuse(ctx, at, ruleTargetSide, fmt.Sprintf("the %s target on %d %s is not above the %s entry",
		usd(*at.req.TakeProfit), at.req.Qty, at.req.Symbol, usd(at.price)))
}

// checkStaleEntry measures a priced entry against the tape. A limit written from
// last night's levels is a number, not a plan, and one already under its own stop
// opens a position the stop leg exits at once.
func (e *Engine) checkStaleEntry(ctx context.Context, at attempt) error {
	if at.req.LimitPrice == nil || at.req.StopLoss == nil || e.limits.MaxEntryDeviationPct <= 0 {
		return nil
	}
	q, err := e.data.Quote(ctx, at.req.Symbol)
	if err != nil {
		return fmt.Errorf("quoting %s to check the entry is still current: %w", at.req.Symbol, err)
	}
	if q.Last <= 0 {
		return nil
	}

	if q.Last <= *at.req.StopLoss {
		return e.refuse(ctx, at, ruleStaleEntry, fmt.Sprintf("%s last traded at %s, already through the stop at %s",
			at.req.Symbol, usd(q.Last), usd(*at.req.StopLoss)))
	}
	deviation := math.Abs(*at.req.LimitPrice-q.Last) * 100 / q.Last
	if deviation <= e.limits.MaxEntryDeviationPct {
		return nil
	}
	return e.refuse(ctx, at, ruleStaleEntry, fmt.Sprintf(
		"the %s entry on %s is %.2f%% from the %s last price, over the %s%% limit",
		usd(*at.req.LimitPrice), at.req.Symbol, deviation, usd(q.Last), trimFloat(e.limits.MaxEntryDeviationPct)))
}

// checkRiskCap bounds what one trade loses at its stop. An entry with no stop is
// only here when stops are optional, and then there is no distance to measure.
func (e *Engine) checkRiskCap(ctx context.Context, at attempt) error {
	if e.limits.PerTradePct <= 0 || at.req.StopLoss == nil {
		return nil
	}
	equity, err := e.Equity(ctx)
	if err != nil {
		return err
	}
	risked := float64(at.req.Qty) * (at.price - *at.req.StopLoss)
	limit := equity * e.limits.PerTradePct / 100
	// Half a cent of slack: the size was rounded against the cap at an equity
	// that has since moved, and "$25.00 is over the $25.00 cap" reads as a bug.
	if risked <= limit+capTolerance {
		return nil
	}
	return e.refuse(ctx, at, ruleRiskCap, fmt.Sprintf(
		"%d %s at %s stopping at %s risks %s, over the %s cap for one trade (%s%% of %s equity)",
		at.req.Qty, at.req.Symbol, usd(at.price), usd(*at.req.StopLoss), usd(risked), usd(limit),
		trimFloat(e.limits.PerTradePct), usd(equity)))
}

// checkMaxPositions counts what is held plus what open buys will hold. Adding to
// a symbol already in that set takes no new slot.
func (e *Engine) checkMaxPositions(ctx context.Context, at attempt) error {
	if e.limits.MaxPositions <= 0 {
		return nil
	}
	open := map[string]bool{}
	for _, p := range at.held {
		if p.Qty != 0 {
			open[p.Symbol] = true
		}
	}
	orders, err := e.openOrders(ctx)
	if err != nil {
		return err
	}
	for _, o := range orders {
		if o.Side == string(broker.Buy) && remainingQty(o) > 0 {
			open[o.Symbol] = true
		}
	}
	if open[at.req.Symbol] || len(open) < e.limits.MaxPositions {
		return nil
	}
	return e.refuse(ctx, at, ruleMaxPositions, fmt.Sprintf("%s would be open position %d against a limit of %d (%s already open)",
		at.req.Symbol, len(open)+1, e.limits.MaxPositions, strings.Join(sortedNames(open), ", ")))
}

// checkAveragingDown refuses a second entry below the average already paid.
// Buying a loser cheaper is how a small loss becomes the account.
func (e *Engine) checkAveragingDown(ctx context.Context, at attempt) error {
	for _, p := range at.held {
		if p.Symbol != at.req.Symbol || p.Qty <= 0 || at.price >= p.AvgEntryPrice {
			continue
		}
		return e.refuse(ctx, at, ruleNoAveragingDown, fmt.Sprintf(
			"%d %s are held at %s and this entry at %s averages down",
			p.Qty, p.Symbol, usd(p.AvgEntryPrice), usd(at.price)))
	}
	return nil
}

// checkFlatByClose stops new entries inside the window before the bell. A closed
// market is not inside it: the order queues for the next open.
func (e *Engine) checkFlatByClose(ctx context.Context, at attempt) error {
	window := time.Duration(e.limits.NoEntriesBeforeCloseMinutes) * time.Minute
	if window <= 0 {
		return nil
	}
	clk, err := e.broker.Clock(ctx)
	if err != nil {
		return fmt.Errorf("reading the venue clock to measure the close: %w", err)
	}
	if !clk.IsOpen || clk.NextClose.IsZero() {
		return nil
	}
	now := clk.Now
	if now.IsZero() {
		now = e.now()
	}
	left := clk.NextClose.Sub(now)
	if left > window {
		return nil
	}
	return e.refuse(ctx, at, ruleFlatByClose, fmt.Sprintf(
		"the session closes in %d minutes and new entries stop %d minutes before the close",
		int(left.Round(time.Minute)/time.Minute), e.limits.NoEntriesBeforeCloseMinutes))
}

// checkDailyHalt stops the day after the loss limit. The trade after two stops is
// the one that turns a bad morning into a bad month.
func (e *Engine) checkDailyHalt(ctx context.Context, at attempt) error {
	halted, why, err := e.Halted(ctx)
	if err != nil || !halted {
		return err
	}
	return e.refuse(ctx, at, ruleDailyHalt, why)
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// trimFloat renders a percentage without trailing zeros, so 0.5 reads as "0.5".
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
