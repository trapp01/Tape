package brief

import (
	"strings"
	"testing"
)

func ptr(v float64) *float64 { return &v }

// validOutput is the shape every case below starts from.
func validOutput() Output {
	return Output{
		MarketRead:   "SPY is up 0.3% pre-market on light volume.",
		RegimeNote:   "Uptrend, low vol.",
		CalendarNote: "Nothing scheduled before the close.",
		Call: Call{
			Instrument:   "SPY",
			Direction:    DirUp,
			ThresholdPct: ptr(0.3),
			Rationale:    "M2 continuation over yesterday's high.",
			Invalidation: "A break back under 511.00.",
		},
		Watchlist: []WatchNote{{Symbol: "NVDA", Bias: "bullish", Note: "Holding the gap."}},
		Risks:     []string{"Thin volume into a holiday."},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Output)
		wantErr string
	}{
		{"a complete briefing passes", func(*Output) {}, ""},
		{"a null threshold is allowed", func(o *Output) { o.Call.ThresholdPct = nil }, ""},
		{"the threshold cap is allowed", func(o *Output) { o.Call.ThresholdPct = ptr(MaxThresholdPct) }, ""},
		{"no watchlist is allowed", func(o *Output) { o.Watchlist = nil }, ""},
		{"no risks is allowed", func(o *Output) { o.Risks = nil }, ""},

		{"empty market read", func(o *Output) { o.MarketRead = "" }, "market_read"},
		{"blank market read", func(o *Output) { o.MarketRead = "   " }, "market_read"},
		{"no instrument", func(o *Output) { o.Call.Instrument = "" }, "instrument"},
		{"lowercase instrument", func(o *Output) { o.Call.Instrument = "spy" }, "uppercase"},
		{"mixed-case instrument", func(o *Output) { o.Call.Instrument = "Spy" }, "uppercase"},
		{"unknown direction", func(o *Output) { o.Call.Direction = "sideways" }, "direction"},
		{"empty direction", func(o *Output) { o.Call.Direction = "" }, "direction"},
		{"negative threshold", func(o *Output) { o.Call.ThresholdPct = ptr(-0.1) }, "threshold"},
		// A zero bar makes an unchanged close both up and down, and never flat.
		{"zero threshold", func(o *Output) { o.Call.ThresholdPct = ptr(0) }, "threshold"},
		{"threshold over the cap", func(o *Output) { o.Call.ThresholdPct = ptr(5.1) }, "threshold"},
		{"watch note without a symbol", func(o *Output) { o.Watchlist[0].Symbol = "" }, "symbol"},
		{"watch note with a bad bias", func(o *Output) { o.Watchlist[0].Bias = "long" }, "bias"},
		{"watch note with no bias", func(o *Output) { o.Watchlist[0].Bias = "" }, "bias"},
		{"too many watch notes", func(o *Output) { o.Watchlist = repeatNotes(MaxWatchNotes + 1) }, "watchlist notes"},
		{"too many risks", func(o *Output) { o.Risks = repeatRisks(MaxRisks + 1) }, "risks"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOutput()
			tc.mutate(&o)
			err := Validate(o)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate accepted the output, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// briefingData is the symbol set a reply is checked against: the indexes plus
// the watchlist the model was actually shown.
func briefingData() Input {
	return Input{
		Indexes:   []SymbolRead{{Symbol: "SPY"}, {Symbol: "QQQ"}},
		Watchlist: []SymbolRead{{Symbol: "NVDA"}, {Symbol: "AAPL"}},
	}
}

func TestValidateAgainstTheBriefingsData(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Output)
		wantErr string
	}{
		{"a call on an index passes", func(*Output) {}, ""},
		{"a call on a watchlist symbol passes", func(o *Output) { o.Call.Instrument = "NVDA" }, ""},

		{"a call on a symbol not in the data", func(o *Output) { o.Call.Instrument = "DOGEUSD" }, "DOGEUSD"},
		{"a watch note on a symbol not in the data", func(o *Output) { o.Watchlist[0].Symbol = "DOGEUSD" }, "DOGEUSD"},
		{"no rationale", func(o *Output) { o.Call.Rationale = "" }, "rationale"},
		{"blank rationale", func(o *Output) { o.Call.Rationale = "  " }, "rationale"},
		{"no invalidation", func(o *Output) { o.Call.Invalidation = "" }, "invalidation"},
		{"blank invalidation", func(o *Output) { o.Call.Invalidation = "\t" }, "invalidation"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOutput()
			tc.mutate(&o)
			err := ValidateAgainst(o, briefingData())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAgainst: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidateAgainst accepted the output, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// ValidateAgainst is the stricter of the two: the schema-level checks alone
// cannot know what the model was shown.
func TestValidateAloneDoesNotKnowTheData(t *testing.T) {
	o := validOutput()
	o.Call.Instrument = "DOGEUSD"
	if err := Validate(o); err != nil {
		t.Fatalf("Validate is shape only: %v", err)
	}
	if err := ValidateAgainst(o, briefingData()); err == nil {
		t.Fatal("ValidateAgainst must refuse a symbol the briefing never carried")
	}
}

func TestValidateAllowsTheFullWatchlist(t *testing.T) {
	o := validOutput()
	o.Watchlist = repeatNotes(MaxWatchNotes)
	o.Risks = repeatRisks(MaxRisks)
	if err := Validate(o); err != nil {
		t.Errorf("Validate at the caps: %v", err)
	}
}

func repeatNotes(n int) []WatchNote {
	out := make([]WatchNote, n)
	for i := range out {
		out[i] = WatchNote{Symbol: "SPY", Bias: "neutral", Note: "Range-bound."}
	}
	return out
}

func repeatRisks(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "The tape turns."
	}
	return out
}
