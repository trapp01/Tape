package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/brief"
	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/regime"
)

// Most providers do not report what a call cost. The column stays NULL and the
// footer drops the segment rather than claiming the briefing was free.
func TestBriefFooterOmitsAnUnreportedCost(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	st, err := journal.Open(cfg.DBPath(), cfg.Account.StartingEquity)
	if err != nil {
		t.Fatalf("opening journal: %v", err)
	}
	defer st.Close()
	b, err := st.BriefingByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("BriefingByID: %v", err)
	}
	if b.CostUSD != nil {
		t.Fatalf("cost_usd = %v, want NULL from a provider that reports none", *b.CostUSD)
	}

	var buf bytes.Buffer
	a := &app{cfg: cfg, loc: time.UTC, out: &buf, style: styler{}}
	renderFooter(a, brief.Result{Briefing: b})

	const want = "briefing #1 · fake-model-1 · 41.2k in / 1.1k out · under a second\n"
	if got := buf.String(); got != want {
		t.Fatalf("footer = %q, want %q", got, want)
	}
}

func TestBriefRenderGolden(t *testing.T) {
	loc := time.FixedZone("MDT", -6*3600)
	at := time.Date(2026, 8, 28, 6, 52, 0, 0, loc)
	threshold := 0.3
	actual := 0.42
	correct := true
	cost := 0.1234

	cfg := config.Default()
	cfg.Account.Timezone = "MDT"
	var buf bytes.Buffer
	a := &app{cfg: cfg, loc: loc, out: &buf, style: styler{}}

	res := brief.Result{
		Input: brief.Input{
			GeneratedAt: at,
			Timezone:    "MDT",
			Mode:        "paper",
			LedgerCash:  5000,
			NextOpen:    at.Add(38 * time.Minute),
			NextClose:   at.Add(7 * time.Hour),
			Indexes: []brief.SymbolRead{
				{Symbol: "SPY", ChangePct: 0.41}, {Symbol: "QQQ", ChangePct: 0.62},
				{Symbol: "IWM", ChangePct: -0.10}, {Symbol: "DIA", ChangePct: 0.20},
			},
			Regime: regime.Regime{Summary: "uptrend, low vol (SPY 512.10 above 20d 505.30 and 50d 498.80; 20d vol 11.2%)"},
			Calendar: []calendar.Event{
				{Kind: calendar.KindEconomic, Title: "CPI", At: at.Add(98 * time.Minute), Impact: calendar.ImpactHigh},
			},
			Watchlist: []brief.SymbolRead{{Symbol: "NVDA", ChangePct: 3.08}},
			Gainers:   []market.Mover{{Symbol: "ABCD", PercentChg: 12.3}},
			Losers:    []market.Mover{{Symbol: "WXYZ", PercentChg: -8.4}},
			Warnings:  []string{"FRED calendar unavailable: FRED_API_KEY not set"},
		},
		Output: brief.Output{
			MarketRead:   "Breadth is narrow.",
			RegimeNote:   "M2 continuations are live at normal size.",
			CalendarNote: "CPI is the session's only scheduled risk.",
			Call: brief.Call{
				Instrument: "SPY", Direction: brief.DirUp, ThresholdPct: &threshold,
				Rationale: "M2: price above the 20d.", Invalidation: "SPY trades below 509.80.",
			},
			Watchlist: []brief.WatchNote{{Symbol: "NVDA", Bias: "bullish", Note: "Holds above 118.40."}},
			Risks:     []string{"A quiet open can fade."},
		},
		Briefing: journal.Briefing{ID: 12, Model: "fake-model-1", InputTokens: 41200, OutputTokens: 1100, LatencyMs: 38000, CostUSD: &cost},
		Call:     &journal.Call{ActualPct: &actual, Correct: &correct},
	}
	renderBriefing(a, res, true)

	const want = `TAPE · Fri Aug 28 · 06:52 MDT · market opens in 38m · cash $5,000.00
MARKET   SPY +0.41%  QQQ +0.62%  IWM -0.10%  DIA +0.20%
         Breadth is narrow.
REGIME   uptrend, low vol (SPY 512.10 above 20d 505.30 and 50d 498.80; 20d vol 11.2%)
         M2 continuations are live at normal size.
CALL     SPY up ≥0.3% open→close   [✓ +0.42%]
         M2: price above the 20d.
         invalid if: SPY trades below 509.80.
CALENDAR
  08:30     CPI (high)
         CPI is the session's only scheduled risk.
WATCHLIST
  NVDA  +3.1%  bullish  Holds above 118.40.
MOVERS   gainers: ABCD +12.3%   losers: WXYZ -8.4%
RISKS    • A quiet open can fade.
SOURCES  FRED calendar unavailable: FRED_API_KEY not set
briefing #12 · fake-model-1 · 41.2k in / 1.1k out · 38s · est. $0.12
`
	if got := buf.String(); got != want {
		t.Fatalf("rendered briefing drifted:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
