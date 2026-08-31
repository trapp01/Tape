package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/broker/fake"
	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/llm"
	"github.com/trapp01/tape/internal/market"
)

const briefReply = `{
  "market_read": "Breadth is narrow; the index is carried by semis.",
  "regime_note": "Uptrend low vol: M2 continuations are live at normal size.",
  "calendar_note": "Nothing scheduled, so the tape is the only input.",
  "call": {
    "instrument": "SPY",
    "direction": "up",
    "threshold_pct": 0.3,
    "rationale": "M2: price is above the 20d and took out yesterday's high.",
    "invalidation": "SPY trades below 509.80, yesterday's low."
  },
  "watchlist": [{"symbol": "NVDA", "bias": "bullish", "note": "Holds above 118.40."}],
  "risks": ["A quiet open can turn into an afternoon fade."]
}`

// briefFeed is the whole market data client in memory: snapshots for every
// configured symbol, a rising bar series for the regime, and one finished
// session for the scorer.
type briefFeed struct {
	snaps map[string]market.Snapshot
	bars  []market.Bar
	// sessionDone is false to make the scorer see a session still in progress.
	sessionDone bool
}

func newBriefFeed() *briefFeed {
	f := &briefFeed{snaps: map[string]market.Snapshot{}, sessionDone: true}
	prices := map[string]float64{
		"SPY": 512.10, "QQQ": 430.00, "IWM": 218.40, "DIA": 402.10,
		"AAPL": 226.40, "MSFT": 418.20, "NVDA": 120.50, "AMZN": 178.30,
		"GOOGL": 165.10, "META": 512.60,
	}
	for symbol, last := range prices {
		f.snaps[symbol] = market.Snapshot{Symbol: symbol, Last: last, PrevClose: last * 0.996}
	}

	// Bars end the session before the pinned day, stamped at midnight Eastern the
	// way the venue does.
	et := market.Eastern()
	last := time.Date(2026, 8, 27, 0, 0, 0, 0, et)
	price := 460.0
	for i := 79; i >= 0; i-- {
		price += 0.6
		f.bars = append(f.bars, market.Bar{
			Time: last.AddDate(0, 0, -i), Open: price - 0.4, High: price + 0.5, Low: price - 0.6, Close: price,
		})
	}
	return f
}

// Session closes 0.5% above its open, so an "up" call at 0.3% grades correct.
func (f *briefFeed) Session(_ context.Context, symbol, day string) (market.Session, error) {
	return market.Session{
		Symbol: symbol, Day: day,
		Open: 510, High: 513, Low: 508, Close: 512.55,
		Volume: 90_000_000, Complete: f.sessionDone,
	}, nil
}

// SessionBars is a rising intraday path for every symbol the feed quotes: the
// first minute dips under a proposed entry and the session runs to the target,
// so a replay has a whole round trip to price.
func (f *briefFeed) SessionBars(_ context.Context, symbol, day string) ([]market.Bar, error) {
	snap, ok := f.snaps[symbol]
	if !ok {
		return nil, nil
	}
	open, err := time.ParseInLocation(market.DayLayout, day, market.Eastern())
	if err != nil {
		return nil, err
	}
	open = open.Add(9*time.Hour + 30*time.Minute)

	const minutes = 30
	from, to := snap.Last*0.996, snap.Last*1.03
	bars := make([]market.Bar, 0, minutes)
	for i := range minutes {
		price := from + (to-from)*float64(i)/float64(minutes-1)
		bars = append(bars, market.Bar{
			Time: open.Add(time.Duration(i) * time.Minute),
			Open: price, High: price + 0.3, Low: price - 0.3, Close: price,
		})
	}
	return bars, nil
}

func (f *briefFeed) Snapshots(_ context.Context, symbols []string) (map[string]market.Snapshot, error) {
	out := make(map[string]market.Snapshot, len(symbols))
	for _, s := range symbols {
		if snap, ok := f.snaps[s]; ok {
			out[s] = snap
		}
	}
	return out, nil
}

func (f *briefFeed) DailyBars(context.Context, string, int) ([]market.Bar, error) { return f.bars, nil }

func (f *briefFeed) TopMovers(context.Context, int) ([]market.Mover, []market.Mover, error) {
	return []market.Mover{{Symbol: "ABCD", Price: 4.10, PercentChg: 12.3}},
		[]market.Mover{{Symbol: "WXYZ", Price: 9.90, PercentChg: -8.4}}, nil
}

func (f *briefFeed) MostActives(context.Context, int) ([]market.Active, error) {
	return []market.Active{{Symbol: "TSLA", Volume: 90_000_000}}, nil
}

func (f *briefFeed) News(context.Context, []string, time.Time, int) ([]market.Headline, error) {
	return []market.Headline{{
		ID: "1", Headline: "Nvidia holds its gains", Source: "Benzinga",
		Symbols: []string{"NVDA"}, CreatedAt: time.Now().Add(-2 * time.Hour),
	}}, nil
}

type fakeLLM struct {
	reply string
	calls int
}

func (p *fakeLLM) Name() string  { return "fake" }
func (p *fakeLLM) Model() string { return "fake-model-1" }

func (p *fakeLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	p.calls++
	return llm.Response{Text: p.reply, Model: p.Model(), InputTokens: 41200, OutputTokens: 1100}, nil
}

// The suite runs on a fixed Friday: 08:52 Eastern is before the bell, and
// 17:00 Eastern is past the 16:30 grading cutoff.
var (
	briefMorning = time.Date(2026, 8, 28, 12, 52, 0, 0, time.UTC)
	briefEvening = time.Date(2026, 8, 28, 21, 0, 0, 0, time.UTC)
	briefSession = "2026-08-28"
	// briefZone is the desk's zone: two hours behind the venue.
	briefZone = "America/Edmonton"
)

// atClock pins the CLI's wall clock; calling it again moves time on.
func atClock(t *testing.T, at time.Time) {
	t.Helper()
	previous := timeNow
	timeNow = func() time.Time { return at }
	t.Cleanup(func() { timeNow = previous })
}

// newBriefHome wires a temp home to an in-memory venue, feed, and model, with
// the clock pinned to the morning of the session. No test reaches the network.
func newBriefHome(t *testing.T) (*fake.Broker, *briefFeed, *fakeLLM) {
	t.Helper()
	if _, err := time.LoadLocation(briefZone); err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	// A fixed zone keeps the briefing's local day, and the footer's wording, off
	// whatever machine the suite runs on.
	t.Setenv("TZ", briefZone)
	newHome(t, "5000")
	atClock(t, briefMorning)

	fb := fake.New()
	fb.MarketOpen = false
	fb.NextOpen = time.Date(2026, 8, 28, 13, 30, 0, 0, time.UTC)
	fb.NextClose = time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC)
	useFake(t, fb)

	feed := newBriefFeed()
	provider := &fakeLLM{reply: briefReply}

	previousFeed, previousCal, previousLLM := newMarketFeed, newCalendarSources, newLLMProvider
	newMarketFeed = func(config.Config) (marketFeed, error) { return feed, nil }
	newCalendarSources = func(config.Config) (calendar.Sources, []string) {
		return calendar.Sources{}, []string{calendar.Warning("FRED", calendar.NotConfigured("FRED_API_KEY not set"))}
	}
	newLLMProvider = func(config.Config) (llm.Provider, error) { return provider, nil }
	t.Cleanup(func() {
		newMarketFeed, newCalendarSources, newLLMProvider = previousFeed, previousCal, previousLLM
	})
	return fb, feed, provider
}

func TestBriefDryRunPrintsEverySection(t *testing.T) {
	_, _, provider := newBriefHome(t)

	out, err := run(t, "brief", "--dry-run")
	if err != nil {
		t.Fatalf("brief --dry-run: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("a dry run must not ask the model, calls = %d", provider.calls)
	}
	for _, want := range []string{
		"[paper] brief", "TAPE ·", "MARKET ", "REGIME ", "CALENDAR", "WATCHLIST", "MOVERS ", "SOURCES ",
		"--- system prompt", "--- user prompt", "PLAYBOOK", "nothing was archived",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry run output missing %q:\n%s", want, out)
		}
	}

	briefs, err := run(t, "briefs")
	if err != nil {
		t.Fatalf("briefs: %v", err)
	}
	if !strings.Contains(briefs, "no briefings yet") {
		t.Fatalf("a dry run must archive nothing:\n%s", briefs)
	}
}

func TestBriefArchivesThenReuses(t *testing.T) {
	_, _, provider := newBriefHome(t)

	first, err := run(t, "brief")
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	for _, want := range []string{
		"[paper] brief", "CALL", "SPY up ≥0.3% open→close", "invalid if: SPY trades below 509.80",
		"scored after close", "RISKS", "briefing #1", "fake-model-1", "41.2k in / 1.1k out",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("brief output missing %q:\n%s", want, first)
		}
	}

	second, err := run(t, "brief")
	if err != nil {
		t.Fatalf("second brief: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("the model was asked %d times, want 1", provider.calls)
	}
	if !strings.Contains(second, "archived earlier today (#1); use --force to re-run") {
		t.Fatalf("a second run must say it reused the archive:\n%s", second)
	}
}

// Before the bell the call is still open, so a forced re-run swaps it.
func TestBriefForceReplacesTheCallBeforeTheOpen(t *testing.T) {
	_, _, provider := newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	forced, err := run(t, "brief", "--force")
	if err != nil {
		t.Fatalf("brief --force: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("--force must ask again, calls = %d", provider.calls)
	}
	if !strings.Contains(forced, "briefing #2") {
		t.Fatalf("--force must archive a second briefing:\n%s", forced)
	}
	if !strings.Contains(forced, "replaced the earlier call and expired the earlier slate") {
		t.Fatalf("--force before the open must replace the call:\n%s", forced)
	}
}

// After the bell the session's call is the record; a second briefing is a
// second read.
func TestBriefForceKeepsTheCallAfterTheOpen(t *testing.T) {
	fb, _, _ := newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}
	fb.MarketOpen = true

	forced, err := run(t, "brief", "--force")
	if err != nil {
		t.Fatalf("brief --force: %v", err)
	}
	if !strings.Contains(forced, "the session's first call and slate stand") {
		t.Fatalf("--force after the open must keep the call:\n%s", forced)
	}
}

// A briefing written for a session that has not opened is the one the next
// morning reads, and the footer says when it was written.
func TestBriefReusesAnEveningBriefingTheNextMorning(t *testing.T) {
	_, _, provider := newBriefHome(t)

	// Thursday evening: the market is shut and next opens on the pinned Friday.
	atClock(t, time.Date(2026, 8, 28, 1, 40, 0, 0, time.UTC))
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("evening brief: %v", err)
	}

	atClock(t, briefMorning)
	morning, err := run(t, "brief")
	if err != nil {
		t.Fatalf("morning brief: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("the morning must reuse the evening briefing, calls = %d", provider.calls)
	}
	if !strings.Contains(morning, "(#1); use --force to re-run") {
		t.Fatalf("the morning must say it reused the archive:\n%s", morning)
	}
	if strings.Contains(morning, "archived earlier today") {
		t.Fatalf("a briefing written the night before was not archived today:\n%s", morning)
	}
}

func TestBriefJSON(t *testing.T) {
	newBriefHome(t)

	out, err := run(t, "brief", "--json")
	if err != nil {
		t.Fatalf("brief --json: %v", err)
	}
	for _, want := range []string{`"input"`, `"output"`, `"briefing_id": 1`, `"market_read"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("json output missing %q:\n%s", want, out)
		}
	}
}

func TestBriefsListAndShow(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	list, err := run(t, "briefs")
	if err != nil {
		t.Fatalf("briefs: %v", err)
	}
	for _, want := range []string{"[paper] briefs", "ID", "SPY up ≥0.3%", "fake-model-1"} {
		if !strings.Contains(list, want) {
			t.Fatalf("briefs list missing %q:\n%s", want, list)
		}
	}

	for _, ref := range []string{"today", "1"} {
		shown, err := run(t, "briefs", "show", ref)
		if err != nil {
			t.Fatalf("briefs show %s: %v", ref, err)
		}
		if !strings.Contains(shown, "SPY up ≥0.3% open→close") || !strings.Contains(shown, "Breadth is narrow") {
			t.Fatalf("briefs show %s did not re-render the archive:\n%s", ref, shown)
		}
	}

	if _, err := run(t, "briefs", "show", "99"); err == nil {
		t.Fatal("showing a briefing that does not exist must fail")
	}
}
