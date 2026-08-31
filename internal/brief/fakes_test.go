package brief

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/llm"
	"github.com/trapp01/tape/internal/market"
)

// fakeFeed stands in for the whole market data client. Each source has its own
// error so a test can knock out one and watch the briefing degrade.
type fakeFeed struct {
	snaps   map[string]market.Snapshot
	bars    map[string][]market.Bar
	stories []market.Headline
	gainers []market.Mover
	losers  []market.Mover
	actives []market.Active
	// sessions is keyed by sessionKey; asked holds every key Session was called with.
	sessions map[string]market.Session
	asked    []string
	// minutes is one session's intraday bars, keyed by sessionKey.
	minutes map[string][]market.Bar

	snapErr    error
	barErr     error
	newsErr    error
	moverErr   error
	activeErr  error
	sessionErr error

	// newsScopes records the symbol list of every News call, in order.
	newsScopes [][]string
}

func (f *fakeFeed) Snapshots(_ context.Context, symbols []string) (map[string]market.Snapshot, error) {
	if f.snapErr != nil {
		return nil, f.snapErr
	}
	out := make(map[string]market.Snapshot, len(symbols))
	for _, s := range symbols {
		if snap, ok := f.snaps[s]; ok {
			out[s] = snap
		}
	}
	return out, nil
}

func (f *fakeFeed) DailyBars(_ context.Context, symbol string, _ int) ([]market.Bar, error) {
	if f.barErr != nil {
		return nil, f.barErr
	}
	return f.bars[symbol], nil
}

func (f *fakeFeed) Session(_ context.Context, symbol, day string) (market.Session, error) {
	f.asked = append(f.asked, sessionKey(symbol, day))
	if f.sessionErr != nil {
		return market.Session{}, f.sessionErr
	}
	s, ok := f.sessions[sessionKey(symbol, day)]
	if !ok {
		return market.Session{}, fmt.Errorf("no prints for %s on %s", symbol, day)
	}
	return s, nil
}

func (f *fakeFeed) SessionBars(_ context.Context, symbol, day string) ([]market.Bar, error) {
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	return f.minutes[sessionKey(symbol, day)], nil
}

func sessionKey(symbol, day string) string { return symbol + " " + day }

func (f *fakeFeed) TopMovers(_ context.Context, _ int) ([]market.Mover, []market.Mover, error) {
	if f.moverErr != nil {
		return nil, nil, f.moverErr
	}
	return f.gainers, f.losers, nil
}

func (f *fakeFeed) MostActives(_ context.Context, _ int) ([]market.Active, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	return f.actives, nil
}

func (f *fakeFeed) News(_ context.Context, symbols []string, _ time.Time, limit int) ([]market.Headline, error) {
	f.newsScopes = append(f.newsScopes, symbols)
	if f.newsErr != nil {
		return nil, f.newsErr
	}
	out := f.stories
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// fakeProvider replies with a canned string and counts how often it was asked.
type fakeProvider struct {
	reply string
	err   error
	calls int
}

func (p *fakeProvider) Name() string  { return "fake" }
func (p *fakeProvider) Model() string { return "fake-model-1" }

func (p *fakeProvider) Complete(context.Context, llm.Request) (llm.Response, error) {
	p.calls++
	if p.err != nil {
		return llm.Response{}, p.err
	}
	return llm.Response{Text: p.reply, Model: p.Model(), InputTokens: 1200, OutputTokens: 340}, nil
}

func testJournal(t *testing.T) *journal.Store {
	t.Helper()
	st, err := journal.Open(filepath.Join(t.TempDir(), "tape.db"), 5000)
	if err != nil {
		t.Fatalf("opening journal: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func testConfig() config.BriefConfig {
	return config.BriefConfig{
		Watchlist:         []string{"NVDA", "AAPL"},
		IndexSymbols:      []string{"SPY", "QQQ"},
		RegimeSymbol:      "SPY",
		CallThresholdPct:  0.3,
		NewsLookbackHours: 18,
		MoversTop:         2,
		CalendarDays:      2,
	}
}

// dailyBars builds a rising series so Classify has something to label.
func dailyBars(start time.Time, n int, from float64) []market.Bar {
	bars := make([]market.Bar, 0, n)
	price := from
	for i := range n {
		price += 0.5
		bars = append(bars, market.Bar{
			Time:  venueStamp(start.AddDate(0, 0, i)),
			Open:  price - 0.2,
			High:  price + 0.4,
			Low:   price - 0.5,
			Close: price,
		})
	}
	return bars
}

// venueStamp dates a daily bar the way Alpaca does: midnight Eastern, which is
// 04:00Z in daylight time and 05:00Z in standard time.
func venueStamp(t time.Time) time.Time {
	et := t.In(market.Eastern())
	return time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, market.Eastern())
}

func testDeps(t *testing.T, feed *fakeFeed, now time.Time, loc *time.Location) Deps {
	t.Helper()
	return Deps{
		Snapshots: feed,
		Bars:      feed,
		Movers:    feed,
		News:      feed,
		Clock: func(context.Context) (broker.Clock, error) {
			return broker.Clock{
				IsOpen:    false,
				NextOpen:  now.Add(38 * time.Minute),
				NextClose: now.Add(7 * time.Hour),
			}, nil
		},
		Ledger:   func(context.Context) (journal.Ledger, error) { return journal.Ledger{Cash: 5000}, nil },
		Playbook: "# Playbook\n\nM1 gap-and-go.\n",
		Journal:  testJournal(t),
		Mode:     journal.ModePaper,
		Loc:      loc,
		Now:      func() time.Time { return now },
		Cfg:      testConfig(),
	}
}

func hasWarning(warnings []string, prefix string) bool {
	for _, w := range warnings {
		if len(w) >= len(prefix) && w[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
