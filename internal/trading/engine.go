// Package trading is the orchestration layer between the CLI and the venue. It
// applies tape's sanity rules before an order leaves the machine, submits it, and
// keeps the journal in step with the broker.
package trading

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/costs"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
)

const (
	defaultPollWindow   = 5 * time.Second
	defaultPollInterval = 200 * time.Millisecond
)

// RefusalSink writes a guardrail refusal to the record. A nil sink still enforces
// every rule; it only means nothing is written down.
type RefusalSink interface {
	Record(ctx context.Context, r journal.Refusal) error
}

// Deps are an Engine's collaborators. Broker, Data and Journal are required.
type Deps struct {
	Broker  broker.Broker
	Data    broker.MarketData
	Journal *journal.Store
	Costs   costs.Model
	// Limits are the entry guardrails. A zero limit turns its own rule off.
	Limits risk.Limits
	// Refusals receives every refusal. Nil means refusals are not recorded.
	Refusals RefusalSink
	// Mode is "paper" or "live"; every journal row is tagged with it.
	Mode string
	// Loc is the zone day boundaries are measured in. Nil means time.Local.
	Loc *time.Location
	// Now is the clock used for journal timestamps. Nil means time.Now.
	Now func() time.Time
	// PollWindow bounds how long Submit and Flatten wait for fills. Zero means 5s.
	PollWindow time.Duration
	// PollInterval is the gap between GetOrder calls. Zero means 200ms.
	PollInterval time.Duration
}

// Engine owns every write that crosses the broker and the journal together.
type Engine struct {
	broker       broker.Broker
	data         broker.MarketData
	jnl          *journal.Store
	costs        costs.Model
	limits       risk.Limits
	refusals     RefusalSink
	mode         string
	loc          *time.Location
	now          func() time.Time
	pollWindow   time.Duration
	pollInterval time.Duration
}

// Result is one submission: the journal row, the venue's order, and the fills
// recorded while waiting for it to settle.
type Result struct {
	Order       journal.Order
	BrokerOrder broker.Order
	Fills       []journal.Fill
	// Cancelled are resting legs taken off the books to free the shares this
	// order sells. An exit is never trapped by the bracket that opened it.
	Cancelled []journal.Order
}

// SyncReport is what one reconciliation pass changed.
type SyncReport struct {
	Checked int
	Updated []journal.Order
	Fills   []journal.Fill
	// Missing are journal orders the venue no longer knows about.
	Missing []string
	// ReconciledProposals are ideas whose order reached the venue but whose
	// decision never landed, closed out by this pass.
	ReconciledProposals []int64
}

// FlattenReport is the end-of-day sweep. StillOpen and Problems both mean the
// sweep did not finish and the caller must say so.
type FlattenReport struct {
	Canceled  []string
	Orders    []journal.Order
	Fills     []journal.Fill
	StillOpen []broker.Position
	Problems  []string
}

// PositionView joins tape's cost basis with the venue's current price. Priced is
// false when neither the broker nor the feed had a price for the symbol.
type PositionView struct {
	Symbol        string
	Qty           int
	AvgEntryPrice float64
	CostBasis     float64
	CurrentPrice  float64
	MarketValue   float64
	UnrealizedPL  float64
	UnrealizedPct float64
	OpenedAt      time.Time
	Priced        bool
}

func New(d Deps) (*Engine, error) {
	if d.Broker == nil {
		return nil, errors.New("trading: new engine: broker is nil")
	}
	if d.Data == nil {
		return nil, errors.New("trading: new engine: market data is nil")
	}
	if d.Journal == nil {
		return nil, errors.New("trading: new engine: journal is nil")
	}
	if d.Mode != journal.ModePaper && d.Mode != journal.ModeLive {
		return nil, fmt.Errorf("trading: new engine: mode must be %q or %q, got %q", journal.ModePaper, journal.ModeLive, d.Mode)
	}

	e := &Engine{
		broker:       d.Broker,
		data:         d.Data,
		jnl:          d.Journal,
		costs:        d.Costs,
		limits:       d.Limits,
		refusals:     d.Refusals,
		mode:         d.Mode,
		loc:          d.Loc,
		now:          d.Now,
		pollWindow:   d.PollWindow,
		pollInterval: d.PollInterval,
	}
	if e.loc == nil {
		e.loc = time.Local
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.pollWindow <= 0 {
		e.pollWindow = defaultPollWindow
	}
	if e.pollInterval <= 0 {
		e.pollInterval = defaultPollInterval
	}
	return e, nil
}

// Mode is "paper" or "live".
func (e *Engine) Mode() string { return e.mode }

// Location is the zone the engine measures trading days in.
func (e *Engine) Location() *time.Location { return e.loc }

// Limits are the entry guardrails this engine enforces.
func (e *Engine) Limits() risk.Limits { return e.limits }
