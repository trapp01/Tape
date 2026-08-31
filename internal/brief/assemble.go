package brief

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/regime"
)

const (
	dayLayout = "2006-01-02"
	// regimeBarCount is the daily history Classify is fed. DefaultOptions needs 50.
	regimeBarCount = 80
	// perSymbolHeadlines and marketHeadlines bound how much news reaches the prompt.
	perSymbolHeadlines = 5
	marketHeadlines    = 15
	// summaryRunes truncates a story summary. The prompt wants the gist, not the article.
	summaryRunes = 300
)

// Assemble reads every source into one archived snapshot. Only the snapshot call
// is fatal: without quotes there is no briefing. Everything else degrades to a
// warning the model and the reader both see.
func Assemble(ctx context.Context, d Deps) (Input, error) {
	now, loc := d.now(), d.loc()
	var warn warnings

	in := Input{
		GeneratedAt: now,
		Timezone:    loc.String(),
		Mode:        d.Mode,
		Playbook:    d.Playbook,
		Limits:      d.Limits,
	}

	if d.Clock != nil {
		if clk, err := d.Clock(ctx); err != nil {
			warn.add("market clock unavailable: %v", err)
		} else {
			in.MarketOpen, in.NextOpen, in.NextClose = clk.IsOpen, clk.NextOpen, clk.NextClose
		}
	}
	if d.Ledger != nil {
		if led, err := d.Ledger(ctx); err != nil {
			warn.add("ledger unavailable: %v", err)
		} else {
			in.LedgerCash = led.Cash
		}
	}
	if equity, err := d.equity(ctx); err != nil {
		warn.add("account value unavailable, so nothing can be sized: %v", err)
	} else {
		in.Equity = equity
	}
	if cash, err := d.cash(ctx); err != nil {
		warn.add("free cash unavailable, so sizes are not capped on what the account can pay: %v", err)
	} else {
		in.FreeCash = cash
	}

	indexes := dedupeUpper(d.Cfg.IndexSymbols)
	watch := dedupeUpper(d.Cfg.Watchlist)
	snaps, err := fetchSnapshots(ctx, d, indexes, watch)
	if err != nil {
		return Input{}, err
	}
	in.Indexes = symbolReads("index", indexes, snaps, &warn)
	in.Watchlist = symbolReads("watchlist", watch, snaps, &warn)

	in.Regime = classifyRegime(ctx, d, now, &warn)
	in.Calendar = collectCalendar(ctx, d, now, loc, watch, &warn)
	attachNews(ctx, d, &in, now, watch, &warn)
	attachMovers(ctx, d, &in, &warn)

	in.Warnings = warn
	return in, nil
}

// fetchSnapshots reads every quoted symbol in one call. A failure here is the
// only fatal one: a briefing without prices is not a briefing.
func fetchSnapshots(ctx context.Context, d Deps, indexes, watch []string) (map[string]market.Snapshot, error) {
	symbols := dedupeUpper(slices.Concat(indexes, watch))
	if len(symbols) == 0 {
		return nil, errors.New("brief: no index or watchlist symbols configured (set brief.index_symbols in config.toml)")
	}
	if d.Snapshots == nil {
		return nil, errors.New("brief: no snapshot provider configured")
	}
	snaps, err := d.Snapshots.Snapshots(ctx, symbols)
	if err != nil {
		return nil, fmt.Errorf("brief: snapshots for %s: %w", strings.Join(symbols, ", "), err)
	}
	return snaps, nil
}

// symbolReads keeps config order and names the symbols the feed had nothing for.
func symbolReads(label string, symbols []string, snaps map[string]market.Snapshot, w *warnings) []SymbolRead {
	out := make([]SymbolRead, 0, len(symbols))
	var missing []string
	for _, sym := range symbols {
		s, ok := snaps[sym]
		if !ok {
			missing = append(missing, sym)
			continue
		}
		out = append(out, SymbolRead{Symbol: sym, Last: s.Last, PrevClose: s.PrevClose, ChangePct: s.ChangePct()})
	}
	if len(missing) > 0 {
		w.add("%s quotes unavailable: %s", label, strings.Join(missing, ", "))
	}
	return out
}

// classifyRegime drops a bar dated today, which is an in-progress session and
// would move the averages the label is read from. Bar dates are the venue's, not
// the reader's: Alpaca stamps a daily bar at midnight Eastern.
func classifyRegime(ctx context.Context, d Deps, now time.Time, w *warnings) regime.Regime {
	symbol := strings.ToUpper(strings.TrimSpace(d.Cfg.RegimeSymbol))
	if symbol == "" || d.Bars == nil {
		w.add("regime unavailable: no daily bars provider configured")
		return regime.Regime{}
	}
	bars, err := d.Bars.DailyBars(ctx, symbol, regimeBarCount)
	if err != nil {
		w.add("regime unavailable: %v", err)
		return regime.Regime{}
	}

	today := market.SessionDate(now)
	if n := len(bars); n > 0 && market.SessionDate(bars[n-1].Time) == today {
		bars = bars[:n-1]
	}

	opts := regime.DefaultOptions()
	reg, err := regime.Classify(symbol, bars, opts)
	if errors.Is(err, regime.ErrInsufficientBars) {
		w.add("regime unavailable: insufficient bars (have %d, need %d)", len(bars), regimeBarsNeeded(opts))
		return regime.Regime{}
	}
	if err != nil {
		w.add("regime unavailable: %v", err)
		return regime.Regime{}
	}
	return reg
}

// regimeBarsNeeded mirrors Classify's requirement: one return costs one extra bar.
func regimeBarsNeeded(o regime.Options) int {
	return max(o.LongWindow, o.VolWindow+1)
}

func collectCalendar(ctx context.Context, d Deps, now time.Time, loc *time.Location, watch []string, w *warnings) []calendar.Event {
	for _, warning := range d.CalendarWarnings {
		*w = append(*w, warning)
	}
	from := now.In(loc)
	to := from.AddDate(0, 0, max(d.Cfg.CalendarDays, 0))
	events, collected := d.Calendar.Collect(ctx, watch, from, to)
	*w = append(*w, collected...)
	return events
}

func attachMovers(ctx context.Context, d Deps, in *Input, w *warnings) {
	if d.Movers == nil || d.Cfg.MoversTop <= 0 {
		return
	}
	gainers, losers, err := d.Movers.TopMovers(ctx, d.Cfg.MoversTop)
	if err != nil {
		w.add("movers unavailable: %v", err)
	} else {
		in.Gainers, in.Losers = gainers, losers
	}
	actives, err := d.Movers.MostActives(ctx, d.Cfg.MoversTop)
	if err != nil {
		w.add("most actives unavailable: %v", err)
		return
	}
	in.Actives = actives
}

// dedupeUpper uppercases and drops repeats while keeping config order.
func dedupeUpper(symbols []string) []string {
	seen := make(map[string]bool, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

type warnings []string

func (w *warnings) add(format string, args ...any) {
	*w = append(*w, fmt.Sprintf(format, args...))
}
