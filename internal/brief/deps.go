package brief

import (
	"context"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/risk"
)

// Deps is everything a briefing reads. Every data source is optional: a nil or
// failing one becomes a warning on the Input, not a failed morning.
type Deps struct {
	Snapshots market.SnapshotProvider
	Bars      market.BarsProvider
	Movers    market.MoversProvider
	News      market.NewsProvider

	Calendar calendar.Sources
	// CalendarWarnings carries sources the caller could not even construct, such
	// as an unkeyed provider.
	CalendarWarnings []string

	Clock  func(context.Context) (broker.Clock, error)
	Ledger func(context.Context) (journal.Ledger, error)
	// Equity is the account value proposals are sized against. Nil falls back to
	// the ledger: cash plus what the open positions cost.
	Equity func(context.Context) (float64, error)
	// Cash is the free cash an idea may spend: ledger cash less what open orders
	// already claim. Nil falls back to the ledger's cash.
	Cash func(context.Context) (float64, error)
	// Limits are the walls the model plans inside and Go sizes against.
	Limits risk.Limits

	// Playbook is the strategy file verbatim; it is the last block of the prompt.
	Playbook string
	Journal  *journal.Store
	Mode     string
	Loc      *time.Location
	// Now is the clock the briefing is dated from. Nil means time.Now.
	Now func() time.Time
	Cfg config.BriefConfig
	// Force archives a new briefing even when the day already has one. The day's
	// first call still stands.
	Force bool
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// equity is what the slate is sized against. An unreadable account sizes
// nothing: risk.Size refuses a non-positive equity and every idea is filed with
// that reason instead of a share count.
func (d Deps) equity(ctx context.Context) (float64, error) {
	if d.Equity != nil {
		return d.Equity(ctx)
	}
	if d.Ledger == nil {
		return 0, nil
	}
	led, err := d.Ledger(ctx)
	if err != nil {
		return 0, err
	}
	total := led.Cash
	for _, p := range led.OpenPositions {
		total += p.CostBasis
	}
	return total, nil
}

// cash is the ceiling one idea's notional may reach. Zero means unknown, which
// sizes on the risk budget alone rather than refusing the whole slate.
func (d Deps) cash(ctx context.Context) (float64, error) {
	if d.Cash != nil {
		return d.Cash(ctx)
	}
	if d.Ledger == nil {
		return 0, nil
	}
	led, err := d.Ledger(ctx)
	if err != nil {
		return 0, err
	}
	return led.Cash, nil
}

func (d Deps) loc() *time.Location {
	if d.Loc == nil {
		return time.UTC
	}
	return d.Loc
}

// venue is the session a briefing is for and whether its bell has rung. The call
// of the day locks at the open, so both answers come from one clock read.
type venue struct {
	day    string
	opened bool
}

// session resolves the session the briefing is about: today while the market is
// open, otherwise the next one to open. An unreadable clock counts as opened, so
// no forced re-run replaces a call on a session that may already be running.
func (d Deps) session(ctx context.Context) venue {
	today := venue{day: market.SessionDate(d.now()), opened: true}
	if d.Clock == nil {
		return today
	}
	clk, err := d.Clock(ctx)
	if err != nil || clk.IsOpen || clk.NextOpen.IsZero() {
		return today
	}
	return venue{day: market.SessionDate(clk.NextOpen)}
}
