package brief

import (
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/playbook"
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
		Proposals: []Proposal{proposal("NVDA")},
		Watchlist: []WatchNote{{Symbol: "NVDA", Bias: "bullish", Note: "Holding the gap."}},
		Risks:     []string{"Thin volume into a holiday."},
	}
}

// proposal is one complete idea; the cases below break one field at a time.
func proposal(symbol string) Proposal {
	return Proposal{
		Symbol:       symbol,
		Side:         SideLong,
		SetupID:      "M2",
		Entry:        128.40,
		Stop:         126.90,
		Target:       131.40,
		Thesis:       "Yesterday's high held on the retest.",
		Invalidation: "A five-minute close back under 127.80.",
		Confidence:   ConfidenceMedium,
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

		{"no proposals is a valid morning", func(o *Output) { o.Proposals = nil }, ""},
		{"the full slate is allowed", func(o *Output) { o.Proposals = slate(MaxProposals) }, ""},
		{"one proposal too many", func(o *Output) { o.Proposals = slate(MaxProposals + 1) }, "proposals"},
		{"no symbol", func(o *Output) { o.Proposals[0].Symbol = "" }, "symbol"},
		{"a lowercase symbol", func(o *Output) { o.Proposals[0].Symbol = "nvda" }, "uppercase"},
		{"a short", func(o *Output) { o.Proposals[0].Side = "short" }, "shorting is not enabled"},
		{"no side", func(o *Output) { o.Proposals[0].Side = "" }, "side"},
		{"no setup id", func(o *Output) { o.Proposals[0].SetupID = " " }, "setup id"},
		{"no entry", func(o *Output) { o.Proposals[0].Entry = 0 }, "entry must be positive"},
		{"no stop", func(o *Output) { o.Proposals[0].Stop = 0 }, "stop must be positive"},
		{"a negative stop", func(o *Output) { o.Proposals[0].Stop = -1 }, "stop must be positive"},
		{"no target", func(o *Output) { o.Proposals[0].Target = 0 }, "target must be positive"},
		{"a stop above the entry", func(o *Output) { o.Proposals[0].Stop = 129 }, "not below entry"},
		{"a stop at the entry", func(o *Output) { o.Proposals[0].Stop = 128.40 }, "not below entry"},
		{"a target below the entry", func(o *Output) { o.Proposals[0].Target = 127 }, "not above entry"},
		{"a target at the entry", func(o *Output) { o.Proposals[0].Target = 128.40 }, "not above entry"},
		{"no thesis", func(o *Output) { o.Proposals[0].Thesis = "" }, "thesis"},
		{"no invalidation", func(o *Output) { o.Proposals[0].Invalidation = "\t" }, "invalidation"},
		{"an unknown confidence", func(o *Output) { o.Proposals[0].Confidence = "certain" }, "confidence"},
		{"no confidence", func(o *Output) { o.Proposals[0].Confidence = "" }, "confidence"},
		{"the same symbol twice", func(o *Output) {
			o.Proposals = []Proposal{proposal("NVDA"), proposal("NVDA")}
		}, "one idea per symbol"},
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
		Indexes:   []SymbolRead{{Symbol: "SPY", Last: 512.10}, {Symbol: "QQQ", Last: 440.00}},
		Watchlist: []SymbolRead{{Symbol: "NVDA", Last: 128.10}, {Symbol: "AAPL", Last: 225.00}},
		Playbook:  playbook.DefaultTemplate,
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

		{"a proposal on an index passes", func(o *Output) { o.Proposals[0].Symbol = "SPY" }, ""},
		{"every entry setup is citable", func(o *Output) {
			o.Proposals = []Proposal{proposal("NVDA"), proposal("AAPL"), proposal("SPY")}
			o.Proposals[0].SetupID = "M1"
			o.Proposals[1].SetupID = "R1"
			o.Proposals[2].SetupID = "M2"
		}, ""},

		// N1 is a heading, and a rule, but citing it argues for the trade it
		// forbids. Nothing downstream would ever catch that.
		{"a proposal citing a no-trade rule", func(o *Output) { o.Proposals[0].SetupID = "N1" }, "N1"},

		{"a proposal on a symbol not in the data", func(o *Output) { o.Proposals[0].Symbol = "DOGEUSD" }, "DOGEUSD"},
		{"a proposal citing a setup the playbook never defines", func(o *Output) { o.Proposals[0].SetupID = "Z9" }, "Z9"},
		{"a proposal citing a setup in the wrong case", func(o *Output) { o.Proposals[0].SetupID = "m2" }, "m2"},
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

// A playbook with no setup ids leaves nothing to cite, so no proposal can stand.
func TestValidateAgainstAPlaybookWithNoSetups(t *testing.T) {
	in := briefingData()
	in.Playbook = "# Playbook\n\nTrade well.\n"

	o := validOutput()
	err := ValidateAgainst(o, in)
	if err == nil {
		t.Fatal("ValidateAgainst accepted a setup id no playbook defines")
	}
	if !strings.Contains(err.Error(), "playbook defines none") {
		t.Errorf("error %q does not say the playbook defines no setups", err)
	}

	o.Proposals = nil
	if err := ValidateAgainst(o, in); err != nil {
		t.Errorf("a briefing with no proposals needs no setups: %v", err)
	}
}

// slate is n proposals on distinct symbols the briefing data carries.
func slate(n int) []Proposal {
	symbols := []string{"NVDA", "AAPL", "SPY", "QQQ"}
	out := make([]Proposal, n)
	for i := range out {
		out[i] = proposal(symbols[i%len(symbols)])
	}
	return out
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
