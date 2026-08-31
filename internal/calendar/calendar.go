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
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`
	// Symbol is set for earnings events only.
	Symbol string `json:"symbol"`
	// At is the scheduled time in UTC. AllDay marks events with a date but no
	// published time, which are rendered on the day without a clock.
	At     time.Time `json:"at"`
	AllDay bool      `json:"all_day"`
	Impact Impact    `json:"impact"`
	Source string    `json:"source"`
	// Detail carries what the source knows beyond the title: "before open",
	// consensus, prior value. Free text, may be empty.
	Detail string `json:"detail"`
}

type EconomicProvider interface {
	// Economic returns releases and decisions scheduled in [from, to].
	Economic(ctx context.Context, from, to time.Time) ([]Event, error)
}

type EarningsProvider interface {
	// Earnings returns reports scheduled in [from, to] for the given symbols.
	Earnings(ctx context.Context, symbols []string, from, to time.Time) ([]Event, error)
}
