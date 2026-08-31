package regime

import (
	"fmt"
	"math"

	"github.com/trapp01/tape/internal/market"
)

// tradingDays is the annualisation factor for close-to-close daily returns.
const tradingDays = 252

func classify(symbol string, bars []market.Bar, opts Options) (Regime, error) {
	if symbol == "" {
		return Regime{}, fmt.Errorf("regime: classify: symbol is empty")
	}
	if err := validateOptions(symbol, opts); err != nil {
		return Regime{}, err
	}

	// One return costs one extra bar, so a long vol window can outrun the long SMA.
	needed := opts.LongWindow
	if opts.VolWindow+1 > needed {
		needed = opts.VolWindow + 1
	}
	if len(bars) < needed {
		return Regime{}, fmt.Errorf("regime: %s: %w: have %d bars, need %d", symbol, ErrInsufficientBars, len(bars), needed)
	}

	closes := make([]float64, len(bars))
	for i, b := range bars {
		if b.Close <= 0 && i >= len(bars)-needed {
			return Regime{}, fmt.Errorf("regime: %s: bar %s has close %v, must be positive", symbol, b.Time.Format("2006-01-02"), b.Close)
		}
		closes[i] = b.Close
	}

	last := closes[len(closes)-1]
	r := Regime{
		Close:          last,
		SMAShort:       sma(closes, opts.ShortWindow),
		SMALong:        sma(closes, opts.LongWindow),
		RealizedVolPct: realizedVolPct(closes, opts.VolWindow),
	}
	r.Trend = trendOf(last, r.SMAShort, r.SMALong)
	r.Vol = volOf(r.RealizedVolPct, opts)
	r.Summary = summarise(symbol, r, opts)
	return r, nil
}

func validateOptions(symbol string, opts Options) error {
	if opts.ShortWindow < 1 || opts.LongWindow < 1 {
		return fmt.Errorf("regime: %s: moving-average windows must be at least 1, got short %d and long %d", symbol, opts.ShortWindow, opts.LongWindow)
	}
	// Sample stddev divides by n-1, so one return is not a volatility.
	if opts.VolWindow < 2 {
		return fmt.Errorf("regime: %s: vol window must be at least 2, got %d", symbol, opts.VolWindow)
	}
	if opts.LowVolPct < 0 || opts.HighVolPct < opts.LowVolPct {
		return fmt.Errorf("regime: %s: vol bounds must be non-negative and ordered, got low %v and high %v", symbol, opts.LowVolPct, opts.HighVolPct)
	}
	return nil
}

// sma averages the most recent window closes.
func sma(closes []float64, window int) float64 {
	var sum float64
	for _, c := range closes[len(closes)-window:] {
		sum += c
	}
	return sum / float64(window)
}

// realizedVolPct is the sample stddev of the last window log returns, annualised
// and expressed in percent.
func realizedVolPct(closes []float64, window int) float64 {
	rets := make([]float64, 0, window)
	for i := len(closes) - window; i < len(closes); i++ {
		rets = append(rets, math.Log(closes[i]/closes[i-1]))
	}

	var sum float64
	for _, r := range rets {
		sum += r
	}
	mean := sum / float64(len(rets))

	var ss float64
	for _, r := range rets {
		d := r - mean
		ss += d * d
	}
	stddev := math.Sqrt(ss / float64(len(rets)-1))
	return stddev * math.Sqrt(tradingDays) * 100
}

// trendOf names a trend only when price and both averages line up in the same
// order. Everything else is sideways.
func trendOf(last, short, long float64) Trend {
	switch {
	case last > short && short > long:
		return TrendUp
	case last < short && short < long:
		return TrendDown
	default:
		return TrendSideways
	}
}

func volOf(volPct float64, opts Options) Vol {
	switch {
	case volPct < opts.LowVolPct:
		return VolLow
	case volPct > opts.HighVolPct:
		return VolHigh
	default:
		return VolNormal
	}
}

func summarise(symbol string, r Regime, opts Options) string {
	rel := "with"
	switch r.Trend {
	case TrendUp:
		rel = "above"
	case TrendDown:
		rel = "below"
	}
	return fmt.Sprintf("%s, %s vol (%s %.2f %s %dd %.2f and %dd %.2f; %dd vol %.1f%%)",
		r.Trend, r.Vol, symbol, r.Close, rel,
		opts.ShortWindow, r.SMAShort, opts.LongWindow, r.SMALong,
		opts.VolWindow, r.RealizedVolPct)
}
