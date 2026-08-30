package fake

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

func (b *Broker) Quote(ctx context.Context, symbol string) (broker.Quote, error) {
	quotes, err := b.Quotes(ctx, []string{symbol})
	if err != nil {
		return broker.Quote{}, err
	}
	return quotes[symbol], nil
}

func (b *Broker) Quotes(_ context.Context, symbols []string) (map[string]broker.Quote, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]broker.Quote, len(symbols))
	for _, s := range symbols {
		out[s] = b.quote(s)
	}
	return out, nil
}

// StreamQuotes emits one quote per symbol and then blocks, so a watcher exits only
// when its context is cancelled.
func (b *Broker) StreamQuotes(ctx context.Context, symbols []string, onQuote func(broker.Quote)) error {
	if len(symbols) == 0 {
		return errors.New("fake: stream quotes: no symbols given")
	}
	if onQuote == nil {
		return errors.New("fake: stream quotes: onQuote is nil")
	}
	for _, s := range symbols {
		b.mu.Lock()
		q := b.quote(s)
		b.mu.Unlock()
		onQuote(q)
	}
	<-ctx.Done()
	return ctx.Err()
}

// quote builds a two-sided quote around the symbol's price. Callers hold the lock.
func (b *Broker) quote(symbol string) broker.Quote {
	price := b.priceOf(symbol)
	half := b.Spread / 2
	return broker.Quote{
		Symbol:    symbol,
		Bid:       price - half,
		BidSize:   100,
		Ask:       price + half,
		AskSize:   100,
		Last:      price,
		Timestamp: time.Now().UTC(),
	}
}

// priceOf is the symbol's set price, or DefaultPrice. Callers hold the lock.
func (b *Broker) priceOf(symbol string) float64 {
	if p, ok := b.prices[symbol]; ok {
		return p
	}
	return b.DefaultPrice
}

// fill books qty more shares on o at price and moves the position. Callers hold
// the lock.
func (b *Broker) fill(o *broker.Order, qty int, price float64) {
	if remaining := o.Qty - o.FilledQty; qty > remaining {
		qty = remaining
	}
	if qty <= 0 {
		return
	}

	prior := 0.0
	if o.FilledAvgPrice != nil {
		prior = *o.FilledAvgPrice * float64(o.FilledQty)
	}
	o.FilledQty += qty
	avg := (prior + price*float64(qty)) / float64(o.FilledQty)
	o.FilledAvgPrice = &avg

	if o.FilledQty >= o.Qty {
		now := time.Now().UTC()
		o.Status, o.RawStatus, o.FilledAt = broker.StatusFilled, "filled", &now
	} else {
		o.Status, o.RawStatus = broker.StatusPartiallyFilled, "partially_filled"
	}
	b.movePosition(o.Symbol, o.Side, qty, price)
	b.cancelSiblingLegs(o)
}

// closeOne submits and fills a market order flattening one symbol. Callers hold
// the lock.
func (b *Broker) closeOne(symbol string, p *position) *broker.Order {
	side, qty := broker.Sell, p.qty
	if qty < 0 {
		side, qty = broker.Buy, -qty
	}
	b.seq++
	o := &broker.Order{
		ID:          fmt.Sprintf("fake-%d", b.seq),
		Symbol:      symbol,
		Side:        side,
		Qty:         qty,
		Type:        broker.Market,
		Status:      broker.StatusAccepted,
		RawStatus:   "accepted",
		SubmittedAt: time.Now().UTC(),
	}
	b.orders[o.ID] = o
	b.fill(o, qty, b.priceOf(symbol))
	return o
}

func (b *Broker) movePosition(symbol string, side broker.Side, qty int, price float64) {
	p, ok := b.held[symbol]
	if !ok {
		p = &position{}
		b.held[symbol] = p
	}
	if side == broker.Buy {
		notional := p.avg*float64(p.qty) + price*float64(qty)
		p.qty += qty
		if p.qty != 0 {
			p.avg = notional / float64(p.qty)
		}
	} else {
		p.qty -= qty
	}
	if p.qty == 0 {
		delete(b.held, symbol)
	}
}

func sortedKeys(m map[string]*position) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
