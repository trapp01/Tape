package trading

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
)

// Rule names. They appear in every refusal message and in the record, so the
// journal can be counted by rule.
const (
	ruleValidOrder      = "valid order"
	ruleNoShorting      = "no shorting"
	ruleNoOverspend     = "no overspend"
	ruleNoStop          = "no entry without a stop"
	ruleTargetSide      = "target above entry"
	ruleStaleEntry      = "stale entry"
	ruleRiskCap         = "risk cap"
	ruleMaxPositions    = "max positions"
	ruleNoAveragingDown = "no averaging down"
	ruleFlatByClose     = "flat by close"
	ruleDailyHalt       = "daily halt"
	dayLayout           = "2006-01-02"
)

// intrinsicRules refuse the idea itself: its prices, its side, its shape. Every
// other rule refuses today's circumstances, and an unknown rule counts as one,
// so a new guardrail cannot silently decide a proposal forever.
var intrinsicRules = map[string]bool{
	ruleValidOrder: true,
	ruleNoStop:     true,
	ruleTargetSide: true,
	ruleNoShorting: true,
}

func situationalRule(rule string) bool { return !intrinsicRules[rule] }

// attempt is one order under inspection. The price is what the rules measure
// against: the limit when there is one, otherwise the quote the order would cross.
type attempt struct {
	req    broker.OrderRequest
	source string
	price  float64
	held   []journal.OpenPosition
}

// check runs the guardrails. Each refusal names the rule and the numbers, because
// a refusal the trader cannot verify is a refusal they will work around.
func (e *Engine) check(ctx context.Context, req broker.OrderRequest, source string) error {
	at := attempt{req: req, source: source}
	if req.Symbol == "" {
		return e.refuse(ctx, at, ruleValidOrder, "symbol is empty")
	}
	if req.Qty <= 0 {
		return e.refuse(ctx, at, ruleValidOrder, fmt.Sprintf("qty must be a positive whole number of shares, got %d", req.Qty))
	}
	// Both sides price off the limit when one is set, so both have to carry a real one.
	if req.LimitPrice != nil && *req.LimitPrice <= 0 {
		return e.refuse(ctx, at, ruleValidOrder, fmt.Sprintf("limit price must be positive, got %v", *req.LimitPrice))
	}
	switch req.Side {
	case broker.Buy:
		return e.checkEntry(ctx, at)
	case broker.Sell:
		return e.checkShorting(ctx, at)
	default:
		return e.refuse(ctx, at, ruleValidOrder, fmt.Sprintf("side must be %q or %q, got %q", broker.Buy, broker.Sell, req.Side))
	}
}

// RefusalError is a guardrail saying no. Callers that must tell a rule apart
// from a venue failure — a proposal is rejected on the first and left open on
// the second — reach it with errors.As.
type RefusalError struct {
	Rule   string
	Detail string
	// Situational is true when the rule refused today's circumstances rather
	// than the idea. Such a refusal decides nothing: the idea stays takeable.
	Situational bool
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("%s (rule: %s)", e.Detail, e.Rule)
}

// refuse names the rule and the numbers, writes the refusal to the record, and
// returns it. A sink that fails is joined into the error: a guardrail that fires
// unrecorded is a hole in the journal.
func (e *Engine) refuse(ctx context.Context, at attempt, rule, detail string) error {
	err := error(&RefusalError{Rule: rule, Detail: detail, Situational: situationalRule(rule)})
	if e.refusals == nil {
		return err
	}
	// The session a refusal belongs to is the venue's, not the reader's: an
	// evening refusal in Mountain time is already the next session's record.
	rec := journal.Refusal{
		Mode:   e.mode,
		Day:    market.SessionDate(e.now()),
		At:     e.now(),
		Rule:   rule,
		Symbol: at.req.Symbol,
		Detail: err.Error(),
		Source: at.source,
	}
	if sinkErr := e.refusals.Record(ctx, rec); sinkErr != nil {
		return errors.Join(err, fmt.Errorf("recording the %s refusal: %w", rule, sinkErr))
	}
	return err
}

// checkShorting keeps every sell covered by shares the tape ledger actually holds
// and has not already promised to a resting sell or a bracket leg.
func (e *Engine) checkShorting(ctx context.Context, at attempt) error {
	positions, err := e.jnl.OpenPositions(ctx, e.mode)
	if err != nil {
		return fmt.Errorf("reading tape positions: %w", err)
	}
	held := 0
	for _, p := range positions {
		if p.Symbol == at.req.Symbol {
			held = p.Qty
			break
		}
	}
	committed, err := e.committedSellQty(ctx, at.req.Symbol)
	if err != nil {
		return err
	}
	if free := held - committed; at.req.Qty > free {
		if committed > 0 {
			return e.refuse(ctx, at, ruleNoShorting, fmt.Sprintf(
				"selling %d %s but the ledger holds %d with %d already committed to open sells, leaving %d",
				at.req.Qty, at.req.Symbol, held, committed, free))
		}
		return e.refuse(ctx, at, ruleNoShorting, fmt.Sprintf("selling %d %s but the ledger holds %d", at.req.Qty, at.req.Symbol, held))
	}
	return nil
}

// checkOverspend measures the priced buy against the tape ledger's cash less what
// open buys already claim. Alpaca's paper balance is not the account.
func (e *Engine) checkOverspend(ctx context.Context, at attempt) error {
	cost := e.estimatedCost(broker.Buy, at.req.Qty, at.price)
	led, err := e.jnl.Ledger(ctx, e.mode)
	if err != nil {
		return fmt.Errorf("reading tape ledger: %w", err)
	}
	committed, err := e.committedCash(ctx)
	if err != nil {
		return err
	}
	if free := led.Cash - committed; cost > free {
		if committed > 0 {
			return e.refuse(ctx, at, ruleNoOverspend, fmt.Sprintf(
				"ledger cash %s less %s committed to open orders leaves %s < cost %s for %d %s at %s",
				usd(led.Cash), usd(committed), usd(free), usd(cost), at.req.Qty, at.req.Symbol, usd(at.price)))
		}
		return e.refuse(ctx, at, ruleNoOverspend, fmt.Sprintf("ledger cash %s < cost %s for %d %s at %s",
			usd(led.Cash), usd(cost), at.req.Qty, at.req.Symbol, usd(at.price)))
	}
	return nil
}

// estimatedCost prices an order the way the fill would be journalled: modeled
// price times quantity plus commission and fees, never the raw quote.
func (e *Engine) estimatedCost(side broker.Side, qty int, price float64) float64 {
	c := e.costs.Apply(side, qty, price)
	return float64(qty)*c.ModeledPrice + c.Commission + c.Fees
}

// entryPrice is the limit when one is set, otherwise the ask a market order would
// cross. check has already refused a non-positive limit.
func (e *Engine) entryPrice(ctx context.Context, req broker.OrderRequest) (float64, error) {
	if req.LimitPrice != nil {
		return *req.LimitPrice, nil
	}
	q, err := e.data.Quote(ctx, req.Symbol)
	if err != nil {
		return 0, fmt.Errorf("pricing %s to check the ledger: %w", req.Symbol, err)
	}
	switch {
	case q.Ask > 0:
		return q.Ask, nil
	case q.Last > 0:
		return q.Last, nil
	default:
		return 0, fmt.Errorf("no ask for %s, so the cost cannot be checked; pass a limit price (rule: no overspend)", req.Symbol)
	}
}

// usd renders money inside guardrail messages. User-facing tables format money
// through internal/cli/style.go; this exists so a refusal can quote its numbers.
func usd(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	digits := strconv.FormatFloat(v, 'f', 2, 64)
	whole, frac, _ := strings.Cut(digits, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + "$" + b.String() + "." + frac
}
