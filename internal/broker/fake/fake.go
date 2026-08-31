// Package fake is an in-memory Broker and MarketData for tests. Market orders fill
// instantly at the symbol's price; limit orders rest until Fill is called.
package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

var (
	_ broker.Broker          = (*Broker)(nil)
	_ broker.MarketData      = (*Broker)(nil)
	_ broker.SessionCalendar = (*Broker)(nil)
)

type position struct {
	qty int
	avg float64
}

// Broker is a venue that never touches the network. Every exported field may be
// set before use; the methods are safe to call from several goroutines.
type Broker struct {
	mu     sync.Mutex
	seq    int
	prices map[string]float64
	orders map[string]*broker.Order
	held   map[string]*position
	// legs maps an entry order to its bracket children; parentOf is the reverse.
	legs     map[string][]string
	parentOf map[string]string
	// sessions is the venue calendar, keyed by session date.
	sessions map[string]hours

	// DefaultPrice prices any symbol SetPrice was not called for.
	DefaultPrice float64
	// Spread is the full bid-to-ask width around the price.
	Spread float64
	// Equity and Cash are the venue's own balances, which tape ignores for stats.
	Equity float64
	Cash   float64
	// MarketOpen and the next-session times drive Clock.
	MarketOpen bool
	NextOpen   time.Time
	NextClose  time.Time
	// SubmitErr, CloseErr and CancelErr force the matching call to fail.
	SubmitErr error
	CloseErr  error
	CancelErr error
}

func New() *Broker {
	now := time.Now().UTC()
	return &Broker{
		prices:       map[string]float64{},
		orders:       map[string]*broker.Order{},
		held:         map[string]*position{},
		legs:         map[string][]string{},
		parentOf:     map[string]string{},
		sessions:     map[string]hours{},
		DefaultPrice: 100,
		Spread:       0.02,
		Equity:       100000,
		Cash:         100000,
		MarketOpen:   true,
		NextOpen:     now.Add(18 * time.Hour),
		NextClose:    now.Add(6 * time.Hour),
	}
}

// SetPrice fixes what a symbol fills and quotes at.
func (b *Broker) SetPrice(symbol string, price float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prices[symbol] = price
}

// SetPosition puts qty shares of symbol on the venue's books without a matching
// journal order, which is how a manual trade or an older journal looks to tape.
func (b *Broker) SetPosition(symbol string, qty int, avg float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if qty == 0 {
		delete(b.held, symbol)
		return
	}
	b.held[symbol] = &position{qty: qty, avg: avg}
}

// Fill executes qty more shares of a resting order at price.
func (b *Broker) Fill(orderID string, qty int, price float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[orderID]
	if !ok {
		return fmt.Errorf("fake: fill order %s: %w", orderID, broker.ErrOrderNotFound)
	}
	b.fill(o, qty, price)
	return nil
}

func (b *Broker) Name() string { return "fake" }

func (b *Broker) Account(context.Context) (broker.Account, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return broker.Account{ID: "fake-account", Equity: b.Equity, Cash: b.Cash, BuyingPower: b.Cash * 2, Paper: true}, nil
}

func (b *Broker) Positions(context.Context) ([]broker.Position, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]broker.Position, 0, len(b.held))
	for _, symbol := range sortedKeys(b.held) {
		p := b.held[symbol]
		price := b.priceOf(symbol)
		out = append(out, broker.Position{
			Symbol:        symbol,
			Qty:           p.qty,
			AvgEntryPrice: p.avg,
			CurrentPrice:  price,
			MarketValue:   price * float64(p.qty),
			UnrealizedPL:  (price - p.avg) * float64(p.qty),
		})
	}
	return out, nil
}

func (b *Broker) SubmitOrder(_ context.Context, req broker.OrderRequest) (broker.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.SubmitErr != nil {
		return broker.Order{}, b.SubmitErr
	}
	b.seq++
	o := &broker.Order{
		ID:            fmt.Sprintf("fake-%d", b.seq),
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Qty:           req.Qty,
		Type:          req.Type,
		LimitPrice:    req.LimitPrice,
		Status:        broker.StatusAccepted,
		RawStatus:     "accepted",
		SubmittedAt:   time.Now().UTC(),
	}
	b.orders[o.ID] = o
	if req.StopLoss != nil || req.TakeProfit != nil {
		b.attachBracket(o, req)
	}
	if req.Type == broker.Market {
		b.fill(o, req.Qty, b.priceOf(req.Symbol))
	}
	return b.snapshot(o), nil
}

func (b *Broker) GetOrder(_ context.Context, id string) (broker.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return broker.Order{}, fmt.Errorf("fake: get order %s: %w", id, broker.ErrOrderNotFound)
	}
	return b.snapshot(o), nil
}

func (b *Broker) ListOrders(_ context.Context, f broker.ListOrdersFilter) ([]broker.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.orders))
	for id := range b.orders {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var out []broker.Order
	for _, id := range ids {
		o := b.orders[id]
		if f.OpenOnly && o.Status.Terminal() {
			continue
		}
		if !f.After.IsZero() && o.SubmittedAt.Before(f.After) {
			continue
		}
		out = append(out, b.snapshot(o))
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (b *Broker) CancelOrder(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.CancelErr != nil {
		return b.CancelErr
	}
	o, ok := b.orders[id]
	if !ok {
		return fmt.Errorf("fake: cancel order %s: %w", id, broker.ErrOrderNotFound)
	}
	if !o.Status.Terminal() {
		o.Status, o.RawStatus = broker.StatusCanceled, "canceled"
	}
	return nil
}

func (b *Broker) ClosePosition(_ context.Context, symbol string) (broker.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.CloseErr != nil {
		return broker.Order{}, b.CloseErr
	}
	p, ok := b.held[symbol]
	if !ok {
		return broker.Order{}, fmt.Errorf("fake: close position %s: no position", symbol)
	}
	return *b.closeOne(symbol, p), nil
}

// CloseAllPositions cancels resting orders and flattens every symbol at market,
// the way Alpaca's liquidation endpoint does.
func (b *Broker) CloseAllPositions(context.Context) ([]broker.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.CloseErr != nil {
		return nil, b.CloseErr
	}
	for _, o := range b.orders {
		if !o.Status.Terminal() {
			o.Status, o.RawStatus = broker.StatusCanceled, "canceled"
		}
	}
	symbols := sortedKeys(b.held)
	out := make([]broker.Order, 0, len(symbols))
	for _, symbol := range symbols {
		out = append(out, *b.closeOne(symbol, b.held[symbol]))
	}
	return out, nil
}

func (b *Broker) Clock(context.Context) (broker.Clock, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return broker.Clock{Now: time.Now().UTC(), IsOpen: b.MarketOpen, NextOpen: b.NextOpen, NextClose: b.NextClose}, nil
}
