package regime

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/market"
)

// barsFrom turns closes into daily bars, oldest first.
func barsFrom(closes []float64) []market.Bar {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]market.Bar, len(closes))
	for i, c := range closes {
		bars[i] = market.Bar{Time: start.AddDate(0, 0, i), Open: c, High: c, Low: c, Close: c, Volume: 1_000_000}
	}
	return bars
}

// series builds n closes from f(i).
func series(n int, f func(i int) float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = f(i)
	}
	return out
}

func TestClassify(t *testing.T) {
	spike := series(60, func(int) float64 { return 100 })
	spike[59] = 112

	tests := []struct {
		name      string
		closes    []float64
		wantTrend Trend
		wantVol   Vol
		wantClose float64
		wantShort float64
		wantLong  float64
	}{
		{
			name:      "rising ramp trends up on quiet tape",
			closes:    series(60, func(i int) float64 { return 100 + float64(i) }),
			wantTrend: TrendUp,
			wantVol:   VolLow,
			wantClose: 159,
			wantShort: 149.5,
			wantLong:  134.5,
		},
		{
			name:      "falling ramp trends down",
			closes:    series(60, func(i int) float64 { return 200 - float64(i) }),
			wantTrend: TrendDown,
			wantVol:   VolLow,
			wantClose: 141,
			wantShort: 150.5,
			wantLong:  165.5,
		},
		{
			name:      "chop with the averages level is sideways",
			closes:    series(60, func(i int) float64 { return 100 + float64(i%2) }),
			wantTrend: TrendSideways,
			wantVol:   VolNormal,
			wantClose: 101,
			wantShort: 100.5,
			wantLong:  100.5,
		},
		{
			name:      "one spike lifts price over both averages and blows out vol",
			closes:    spike,
			wantTrend: TrendUp,
			wantVol:   VolHigh,
			wantClose: 112,
			wantShort: 100.6,
			wantLong:  100.24,
		},
		{
			name:      "a flat tape has no trend and no vol",
			closes:    series(60, func(int) float64 { return 100 }),
			wantTrend: TrendSideways,
			wantVol:   VolLow,
			wantClose: 100,
			wantShort: 100,
			wantLong:  100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Classify("SPY", barsFrom(tc.closes), DefaultOptions())
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if r.Trend != tc.wantTrend {
				t.Errorf("Trend = %q, want %q", r.Trend, tc.wantTrend)
			}
			if r.Vol != tc.wantVol {
				t.Errorf("Vol = %q (%.2f%%), want %q", r.Vol, r.RealizedVolPct, tc.wantVol)
			}
			if !closeTo(r.Close, tc.wantClose, 1e-9) {
				t.Errorf("Close = %v, want %v", r.Close, tc.wantClose)
			}
			if !closeTo(r.SMAShort, tc.wantShort, 1e-9) {
				t.Errorf("SMAShort = %v, want %v", r.SMAShort, tc.wantShort)
			}
			if !closeTo(r.SMALong, tc.wantLong, 1e-9) {
				t.Errorf("SMALong = %v, want %v", r.SMALong, tc.wantLong)
			}
			if !strings.HasPrefix(r.Summary, string(tc.wantTrend)+", "+string(tc.wantVol)+" vol (SPY ") {
				t.Errorf("Summary = %q", r.Summary)
			}
		})
	}
}

func TestClassifySummaryReadsAsOneLine(t *testing.T) {
	closes := series(60, func(i int) float64 { return 100 + float64(i) })
	r, err := Classify("SPY", barsFrom(closes), DefaultOptions())
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	want := "uptrend, low vol (SPY 159.00 above 20d 149.50 and 50d 134.50; 20d vol 0.4%)"
	if r.Summary != want {
		t.Errorf("Summary =\n%q\nwant\n%q", r.Summary, want)
	}

	down, err := Classify("QQQ", barsFrom(series(60, func(i int) float64 { return 200 - float64(i) })), DefaultOptions())
	if err != nil {
		t.Fatalf("Classify falling: %v", err)
	}
	if !strings.Contains(down.Summary, "QQQ 141.00 below 20d 150.50 and 50d 165.50") {
		t.Errorf("downtrend summary = %q", down.Summary)
	}

	flat, err := Classify("IWM", barsFrom(series(60, func(i int) float64 { return 100 + float64(i%2) })), DefaultOptions())
	if err != nil {
		t.Fatalf("Classify chop: %v", err)
	}
	if !strings.Contains(flat.Summary, "IWM 101.00 with 20d 100.50 and 50d 100.50") {
		t.Errorf("sideways summary = %q", flat.Summary)
	}
}

// TestRealizedVolPctMatchesTheClosedForm checks the annualised number against the
// stddev of an alternating +/- ln(1.01) return series worked out by hand.
func TestRealizedVolPctMatchesTheClosedForm(t *testing.T) {
	closes := []float64{100, 101, 100, 101, 100}
	opts := Options{ShortWindow: 2, LongWindow: 5, VolWindow: 4, LowVolPct: 12, HighVolPct: 25}

	r, err := Classify("SPY", barsFrom(closes), opts)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	// Four returns of +/- ln(1.01) about a zero mean: stddev = 2*ln(1.01)/sqrt(3).
	want := 2 * math.Log(1.01) / math.Sqrt(3) * math.Sqrt(252) * 100
	if !closeTo(r.RealizedVolPct, want, 1e-9) {
		t.Errorf("RealizedVolPct = %v, want %v", r.RealizedVolPct, want)
	}
	if !closeTo(r.RealizedVolPct, 18.239258, 1e-6) {
		t.Errorf("RealizedVolPct = %v, want 18.239258", r.RealizedVolPct)
	}
	if r.Vol != VolNormal {
		t.Errorf("Vol = %q, want %q at 18.2%% inside a 12/25 band", r.Vol, VolNormal)
	}
}

func TestClassifyInsufficientBars(t *testing.T) {
	short := barsFrom(series(49, func(i int) float64 { return 100 + float64(i) }))
	_, err := Classify("SPY", short, DefaultOptions())
	if !errors.Is(err, ErrInsufficientBars) {
		t.Fatalf("error = %v, want ErrInsufficientBars", err)
	}
	for _, want := range []string{"SPY", "49", "50"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	// A vol window longer than the long average needs the extra bar for its first return.
	opts := Options{ShortWindow: 3, LongWindow: 5, VolWindow: 5, LowVolPct: 12, HighVolPct: 25}
	if _, err := Classify("SPY", barsFrom(series(5, func(int) float64 { return 100 })), opts); !errors.Is(err, ErrInsufficientBars) {
		t.Errorf("error = %v, want ErrInsufficientBars", err)
	}
	if _, err := Classify("SPY", barsFrom(series(6, func(int) float64 { return 100 })), opts); err != nil {
		t.Errorf("Classify with the extra bar: %v", err)
	}
}

func TestClassifyRejectsBadInput(t *testing.T) {
	bars := barsFrom(series(60, func(i int) float64 { return 100 + float64(i) }))

	tests := []struct {
		name   string
		symbol string
		opts   Options
	}{
		{"no symbol", "", DefaultOptions()},
		{"zero short window", "SPY", Options{ShortWindow: 0, LongWindow: 50, VolWindow: 20, HighVolPct: 25}},
		{"zero long window", "SPY", Options{ShortWindow: 20, LongWindow: 0, VolWindow: 20, HighVolPct: 25}},
		{"vol window of one", "SPY", Options{ShortWindow: 20, LongWindow: 50, VolWindow: 1, HighVolPct: 25}},
		{"inverted vol band", "SPY", Options{ShortWindow: 20, LongWindow: 50, VolWindow: 20, LowVolPct: 30, HighVolPct: 25}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Classify(tc.symbol, bars, tc.opts); err == nil {
				t.Fatal("Classify succeeded, want an error")
			}
		})
	}

	zeroed := barsFrom(series(60, func(i int) float64 { return 100 + float64(i) }))
	zeroed[59].Close = 0
	if _, err := Classify("SPY", zeroed, DefaultOptions()); err == nil {
		t.Error("Classify accepted a bar with a zero close")
	}
}

func closeTo(got, want, tol float64) bool {
	return math.Abs(got-want) <= tol
}
