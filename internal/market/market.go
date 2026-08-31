// Package market defines the read-only data contracts the briefing is built
// from. Providers know nothing about the journal, the playbook, or the model.
package market

import (
	"context"
	"errors"
	"time"
)

// ErrNotConfigured means the provider has no key or endpoint; the briefing
// reports the source as unavailable instead of failing.
var ErrNotConfigured = errors.New("market: provider not configured")

// Bar is one daily candle. Volume is float64 because some venues report
// fractional aggregates.
type Bar struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Snapshot is the latest picture of one symbol: last trade, current quote, and
// the previous session's close so a pre-market change can be computed.
type Snapshot struct {
	Symbol    string
	Last      float64
	LastAt    time.Time
	Bid       float64
	Ask       float64
	PrevClose float64
	// TodayOpen is zero before the regular session opens.
	TodayOpen float64
	Volume    float64
}

// ChangePct is the move from the previous close to the last trade, in percent.
// Zero when either side is missing.
func (s Snapshot) ChangePct() float64 {
	if s.PrevClose == 0 || s.Last == 0 {
		return 0
	}
	return (s.Last - s.PrevClose) / s.PrevClose * 100
}

type Mover struct {
	Symbol     string
	Price      float64
	Change     float64
	PercentChg float64
}

type Active struct {
	Symbol     string
	Volume     float64
	TradeCount int64
}

type Headline struct {
	ID        string
	Headline  string
	Summary   string
	Source    string
	URL       string
	Symbols   []string
	CreatedAt time.Time
}

// Session is one regular-hours trading day, folded from the prints between the
// bells. Complete is false while the session is still running, which is the only
// state a call may not be graded against.
type Session struct {
	Symbol string
	// Day is the session date in the venue's zone, YYYY-MM-DD.
	Day    string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
	// Complete means the session has run to the bell.
	Complete bool
}

type SnapshotProvider interface {
	Snapshots(ctx context.Context, symbols []string) (map[string]Snapshot, error)
}

type SessionProvider interface {
	// Session returns one symbol's regular-hours session for day, which is a
	// venue-zone date in DayLayout.
	Session(ctx context.Context, symbol, day string) (Session, error)
}

type BarsProvider interface {
	// DailyBars returns up to `days` completed daily bars, oldest first.
	DailyBars(ctx context.Context, symbol string, days int) ([]Bar, error)
}

type MoversProvider interface {
	// TopMovers returns the market-wide top gainers and losers for the session.
	TopMovers(ctx context.Context, top int) (gainers, losers []Mover, err error)
	MostActives(ctx context.Context, top int) ([]Active, error)
}

type NewsProvider interface {
	// News returns headlines since `since` for the symbols (all symbols when
	// empty), newest first, at most `limit`.
	News(ctx context.Context, symbols []string, since time.Time, limit int) ([]Headline, error)
}
