// Package calendar defines scheduled-event contracts: economic releases, FOMC
// decisions, and earnings for watchlist symbols. Each source is optional; the
// briefing lists what it could not reach.
package calendar

import (
	"context"
	"errors"
	"time"
)

var ErrNotConfigured = errors.New("calendar: provider not configured")

type Kind string

const (
	KindEconomic Kind = "economic"
	KindFOMC     Kind = "fomc"
	KindEarnings Kind = "earnings"
)

type Impact string

const (
	ImpactHigh   Impact = "high"
	ImpactMedium Impact = "medium"
	ImpactLow    Impact = "low"
)

type Event struct {
	Kind  Kind
	Title string
	// Symbol is set for earnings events only.
	Symbol string
	// At is the scheduled time in UTC. AllDay marks events with a date but no
	// published time, which are rendered on the day without a clock.
	At     time.Time
	AllDay bool
	Impact Impact
	Source string
	// Detail carries what the source knows beyond the title: "before open",
	// consensus, prior value. Free text, may be empty.
	Detail string
}

type EconomicProvider interface {
	// Economic returns releases and decisions scheduled in [from, to].
	Economic(ctx context.Context, from, to time.Time) ([]Event, error)
}

type EarningsProvider interface {
	// Earnings returns reports scheduled in [from, to] for the given symbols.
	Earnings(ctx context.Context, symbols []string, from, to time.Time) ([]Event, error)
}
