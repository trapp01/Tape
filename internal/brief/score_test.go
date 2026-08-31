package brief

import (
	"math"
	"strings"
	"testing"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name          string
		direction     Direction
		threshold     *float64
		defaultPct    float64
		open, close   float64
		wantActual    float64
		wantThreshold float64
		wantCorrect   bool
	}{
		{
			name: "up call clears its own threshold", direction: DirUp, threshold: ptr(0.5),
			open: 100, close: 101, wantActual: 1, wantThreshold: 0.5, wantCorrect: true,
		},
		{
			name: "up call falls short", direction: DirUp, threshold: ptr(2),
			open: 100, close: 101, wantActual: 1, wantThreshold: 2, wantCorrect: false,
		},
		{
			name: "up call on exactly the threshold is correct", direction: DirUp, threshold: ptr(1),
			open: 100, close: 101, wantActual: 1, wantThreshold: 1, wantCorrect: true,
		},
		{
			name: "down call clears the mirror threshold", direction: DirDown, threshold: ptr(0.5),
			open: 100, close: 99, wantActual: -1, wantThreshold: 0.5, wantCorrect: true,
		},
		{
			name: "down call on exactly the threshold is correct", direction: DirDown, threshold: ptr(1),
			open: 100, close: 99, wantActual: -1, wantThreshold: 1, wantCorrect: true,
		},
		{
			name: "down call on an up day", direction: DirDown, threshold: ptr(0.5),
			open: 100, close: 101, wantActual: 1, wantThreshold: 0.5, wantCorrect: false,
		},
		{
			name: "flat call inside the threshold", direction: DirFlat, threshold: ptr(2),
			open: 100, close: 101, wantActual: 1, wantThreshold: 2, wantCorrect: true,
		},
		{
			name: "flat call on exactly the threshold is wrong", direction: DirFlat, threshold: ptr(1),
			open: 100, close: 101, wantActual: 1, wantThreshold: 1, wantCorrect: false,
		},
		{
			name: "flat call on exactly the down threshold is wrong", direction: DirFlat, threshold: ptr(1),
			open: 100, close: 99, wantActual: -1, wantThreshold: 1, wantCorrect: false,
		},
		{
			name: "a null threshold takes the configured default", direction: DirUp, threshold: nil,
			defaultPct: 0.5, open: 200, close: 201, wantActual: 0.5, wantThreshold: 0.5, wantCorrect: true,
		},
		{
			name: "an unchanged close is flat under any positive threshold", direction: DirFlat, threshold: ptr(0.3),
			open: 100, close: 100, wantActual: 0, wantThreshold: 0.3, wantCorrect: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := Call{Instrument: "SPY", Direction: tc.direction, ThresholdPct: tc.threshold}
			got, err := Score(c, tc.open, tc.close, tc.defaultPct)
			if err != nil {
				t.Fatalf("Score: %v", err)
			}
			if math.Abs(got.ActualPct-tc.wantActual) > 1e-9 {
				t.Errorf("ActualPct = %v, want %v", got.ActualPct, tc.wantActual)
			}
			if got.ThresholdPct != tc.wantThreshold {
				t.Errorf("ThresholdPct = %v, want %v", got.ThresholdPct, tc.wantThreshold)
			}
			if got.Correct != tc.wantCorrect {
				t.Errorf("Correct = %v, want %v (%.4f%% against %.4f%%)", got.Correct, tc.wantCorrect, got.ActualPct, got.ThresholdPct)
			}
			if got.Open != tc.open || got.Close != tc.close {
				t.Errorf("Outcome kept open/close %v/%v, want %v/%v", got.Open, got.Close, tc.open, tc.close)
			}
		})
	}
}

// The three directions are one question, so no session can make two of them
// right. A threshold of zero used to make an unchanged close both up and down.
func TestScoreDirectionsAreMutuallyExclusive(t *testing.T) {
	thresholds := []float64{0.001, 0.3, 1, MaxThresholdPct}
	closes := []float64{95, 99.7, 99.999, 100, 100.001, 100.3, 105}

	for _, threshold := range thresholds {
		for _, close := range closes {
			var right []Direction
			for _, dir := range []Direction{DirUp, DirDown, DirFlat} {
				out, err := Score(Call{Instrument: "SPY", Direction: dir, ThresholdPct: &threshold}, 100, close, 0)
				if err != nil {
					t.Fatalf("Score %s at %v on close %v: %v", dir, threshold, close, err)
				}
				if out.Correct {
					right = append(right, dir)
				}
			}
			if len(right) != 1 {
				t.Errorf("close %v against %v%%: %v correct, want exactly one", close, threshold, right)
			}
		}
	}
}

func TestScoreRejectsBadInput(t *testing.T) {
	tests := []struct {
		name        string
		call        Call
		open, close float64
		defaultPct  float64
		wantErr     string
	}{
		{"zero open", Call{Instrument: "SPY", Direction: DirUp}, 0, 100, 0.3, "open"},
		{"negative open", Call{Instrument: "SPY", Direction: DirUp}, -1, 100, 0.3, "open"},
		{"zero close", Call{Instrument: "SPY", Direction: DirUp}, 100, 0, 0.3, "close"},
		{"negative close", Call{Instrument: "SPY", Direction: DirUp}, 100, -1, 0.3, "close"},
		{"unknown direction", Call{Instrument: "SPY", Direction: "sideways"}, 100, 101, 0.3, "direction"},
		{"empty direction", Call{Instrument: "SPY"}, 100, 101, 0.3, "direction"},
		{"negative call threshold", Call{Instrument: "SPY", Direction: DirUp, ThresholdPct: ptr(-0.5)}, 100, 101, 0.3, "threshold"},
		{"negative default threshold", Call{Instrument: "SPY", Direction: DirUp}, 100, 101, -0.3, "threshold"},
		{"zero call threshold", Call{Instrument: "SPY", Direction: DirUp, ThresholdPct: ptr(0)}, 100, 101, 0.3, "threshold must be positive"},
		{"zero default threshold", Call{Instrument: "SPY", Direction: DirUp}, 100, 101, 0, "threshold must be positive"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Score(tc.call, tc.open, tc.close, tc.defaultPct)
			if err == nil {
				t.Fatal("Score succeeded, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "SPY") {
				t.Errorf("error %q does not name the instrument", err)
			}
		})
	}
}
