package brief

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/market"
)

func mountain(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	return loc
}

// fullFeed carries every source, so a test can break exactly one.
func fullFeed(now time.Time) *fakeFeed {
	return &fakeFeed{
		snaps: map[string]market.Snapshot{
			"SPY":  {Symbol: "SPY", Last: 512.10, PrevClose: 510.00},
			"QQQ":  {Symbol: "QQQ", Last: 430.00, PrevClose: 427.35},
			"NVDA": {Symbol: "NVDA", Last: 120.50, PrevClose: 116.90},
			"AAPL": {Symbol: "AAPL", Last: 226.40, PrevClose: 227.10},
		},
		bars: map[string][]market.Bar{
			"SPY": dailyBars(now.AddDate(0, 0, -80), 80, 460),
		},
		stories: []market.Headline{
			{ID: "2", Headline: "Nvidia beats", Source: "Benzinga", Symbols: []string{"NVDA"}, CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "1", Headline: "Futures slip", Source: "Reuters", Symbols: []string{"SPY"}, CreatedAt: now.Add(-3 * time.Hour)},
		},
		gainers: []market.Mover{{Symbol: "ABCD", Price: 4.10, PercentChg: 12.3}},
		losers:  []market.Mover{{Symbol: "WXYZ", Price: 9.90, PercentChg: -8.4}},
		actives: []market.Active{{Symbol: "TSLA", Volume: 90_000_000}},
	}
}

func TestAssembleReadsEverySource(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	feed := fullFeed(now)

	in, err := Assemble(context.Background(), testDeps(t, feed, now, loc))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(in.Warnings) != 0 {
		t.Fatalf("a healthy feed warns about nothing, got %q", in.Warnings)
	}
	if got := []string{in.Indexes[0].Symbol, in.Indexes[1].Symbol}; got[0] != "SPY" || got[1] != "QQQ" {
		t.Fatalf("indexes must keep config order, got %q", got)
	}
	if got := in.Watchlist[0].Symbol; got != "NVDA" {
		t.Fatalf("watchlist must keep config order, got %q", got)
	}
	if in.Regime.Trend == "" {
		t.Fatalf("regime was not classified: %+v", in.Regime)
	}
	if in.LedgerCash != 5000 || in.MarketOpen {
		t.Fatalf("clock and ledger did not land: cash %v open %v", in.LedgerCash, in.MarketOpen)
	}
	if len(in.Watchlist[0].Headlines) != 1 || in.Watchlist[0].Headlines[0].ID != "2" {
		t.Fatalf("NVDA headlines = %+v", in.Watchlist[0].Headlines)
	}
	if len(in.MarketHeadlines) != 2 {
		t.Fatalf("market headlines = %+v", in.MarketHeadlines)
	}
	if len(feed.newsScopes) != 2 || len(feed.newsScopes[0]) != 2 || feed.newsScopes[1] != nil {
		t.Fatalf("news must be one watchlist call and one market-wide call, got %v", feed.newsScopes)
	}
	if len(in.Gainers) != 1 || len(in.Losers) != 1 || len(in.Actives) != 1 {
		t.Fatalf("movers did not land: %+v %+v %+v", in.Gainers, in.Losers, in.Actives)
	}
}

// Snapshots are the one source a briefing cannot be written without.
func TestAssembleFailsWithoutSnapshots(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	feed := fullFeed(now)
	feed.snapErr = errors.New("403 forbidden")

	if _, err := Assemble(context.Background(), testDeps(t, feed, now, loc)); err == nil {
		t.Fatal("Assemble must fail when no quotes came back")
	} else if !strings.Contains(err.Error(), "403 forbidden") || !strings.Contains(err.Error(), "SPY") {
		t.Fatalf("the error must name the symbols and the cause, got: %v", err)
	}
}

func TestAssembleDegradesPerSource(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)

	cases := map[string]struct {
		breakIt func(*fakeFeed)
		prefix  string
	}{
		"bars":    {func(f *fakeFeed) { f.barErr = errors.New("rate limited") }, "regime unavailable: "},
		"news":    {func(f *fakeFeed) { f.newsErr = errors.New("timeout") }, "news unavailable: "},
		"movers":  {func(f *fakeFeed) { f.moverErr = errors.New("500") }, "movers unavailable: "},
		"actives": {func(f *fakeFeed) { f.activeErr = errors.New("500") }, "most actives unavailable: "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			feed := fullFeed(now)
			tc.breakIt(feed)

			in, err := Assemble(context.Background(), testDeps(t, feed, now, loc))
			if err != nil {
				t.Fatalf("one broken source must not fail the briefing: %v", err)
			}
			if !hasWarning(in.Warnings, tc.prefix) {
				t.Fatalf("warnings %q must carry %q", in.Warnings, tc.prefix)
			}
			if len(in.Indexes) != 2 {
				t.Fatalf("the rest of the briefing must survive, indexes = %+v", in.Indexes)
			}
		})
	}
}

// A bar dated today is an in-progress session: it would move the averages the
// regime label is read from. The venue stamps it at midnight Eastern, which is
// still yesterday in Mountain time.
func TestAssembleDropsTodaysBar(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 7, 29, 11, 0, 0, 0, loc)
	feed := fullFeed(now)

	bars := dailyBars(time.Date(2026, 5, 10, 0, 0, 0, 0, loc), 60, 460)
	settled := bars[len(bars)-1].Close
	bars = append(bars, market.Bar{
		Time:  time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC),
		Open:  settled,
		High:  settled + 90,
		Low:   settled,
		Close: settled + 90,
	})
	feed.bars["SPY"] = bars

	in, err := Assemble(context.Background(), testDeps(t, feed, now, loc))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if in.Regime.Close != settled {
		t.Fatalf("regime closed at %v, want the last settled bar %v", in.Regime.Close, settled)
	}
}

func TestAssembleWarnsOnInsufficientBars(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	feed := fullFeed(now)
	feed.bars["SPY"] = dailyBars(time.Date(2026, 5, 10, 0, 0, 0, 0, loc), 12, 460)

	in, err := Assemble(context.Background(), testDeps(t, feed, now, loc))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	const want = "regime unavailable: insufficient bars (have 12, need 50)"
	if !hasWarning(in.Warnings, want) {
		t.Fatalf("warnings %q must carry %q", in.Warnings, want)
	}
}

func TestAssembleCarriesCallerCalendarWarnings(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	d := testDeps(t, fullFeed(now), now, loc)
	d.CalendarWarnings = []string{"FRED calendar unavailable: FRED_API_KEY not set"}

	in, err := Assemble(context.Background(), d)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if !hasWarning(in.Warnings, "FRED calendar unavailable: ") {
		t.Fatalf("the caller's warnings must reach the briefing, got %q", in.Warnings)
	}
}

func TestAssembleTruncatesSummaries(t *testing.T) {
	loc := mountain(t)
	now := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	feed := fullFeed(now)
	feed.stories = []market.Headline{{
		ID: "9", Headline: "Long one", Symbols: []string{"NVDA"},
		Summary:   strings.Repeat("é", 600),
		CreatedAt: now.Add(-time.Hour),
	}}

	in, err := Assemble(context.Background(), testDeps(t, feed, now, loc))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	got := []rune(in.Watchlist[0].Headlines[0].Summary)
	if len(got) != summaryRunes+1 || got[len(got)-1] != '…' {
		t.Fatalf("summary is %d runes, want %d plus an ellipsis", len(got), summaryRunes)
	}
}
