// Package regime classifies the market from daily bars with fixed rules, so
// the label the briefing prints is reproducible and never the model's opinion.
package regime

import (
	"errors"

	"github.com/trapp01/tape/internal/market"
)

// ErrInsufficientBars means fewer bars were supplied than Options.LongWindow.
var ErrInsufficientBars = errors.New("regime: insufficient bars")

type Trend string

const (
	TrendUp       Trend = "uptrend"
	TrendDown     Trend = "downtrend"
	TrendSideways Trend = "sideways"
)

type Vol string

const (
	VolLow    Vol = "low"
	VolNormal Vol = "normal"
	VolHigh   Vol = "high"
)

type Options struct {
	// ShortWindow and LongWindow are the moving-average lengths in bars.
	ShortWindow int
	LongWindow  int
	// VolWindow is the lookback for realized volatility in bars.
	VolWindow int
	// LowVolPct and HighVolPct bound "normal" annualised realized vol, in percent.
	LowVolPct  float64
	HighVolPct float64
}

func DefaultOptions() Options {
	return Options{ShortWindow: 20, LongWindow: 50, VolWindow: 20, LowVolPct: 12, HighVolPct: 25}
}

type Regime struct {
	Trend Trend   `json:"trend"`
	Vol   Vol     `json:"vol"`
	Close float64 `json:"close"`
	// SMAShort and SMALong are the moving averages the trend call was made from.
	SMAShort float64 `json:"sma_short"`
	SMALong  float64 `json:"sma_long"`
	// RealizedVolPct is annualised close-to-close volatility over VolWindow, in percent.
	RealizedVolPct float64 `json:"realized_vol_pct"`
	// Summary is one line, e.g. "uptrend, low vol (SPY 512.10 above 20d 505.30 and 50d 498.80; 20d vol 11.2%)".
	Summary string `json:"summary"`
}

// Classify labels the regime from daily bars, oldest first. Implemented in classify.go.
func Classify(symbol string, bars []market.Bar, opts Options) (Regime, error) {
	return classify(symbol, bars, opts)
}
