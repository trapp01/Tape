// Package brief assembles the morning briefing: a snapshot of everything the
// model was shown, the model's structured read of it, and the one falsifiable
// call that gets scored against the close.
package brief

import (
	"encoding/json"
	"time"

	"github.com/trapp01/tape/internal/calendar"
	"github.com/trapp01/tape/internal/market"
	"github.com/trapp01/tape/internal/regime"
)

// Input is the complete context handed to the model, archived verbatim so a
// briefing can be re-read later next to what actually happened.
type Input struct {
	GeneratedAt time.Time
	Timezone    string
	Mode        string
	LedgerCash  float64
	// MarketOpen and NextOpen come from the venue clock.
	MarketOpen bool
	NextOpen   time.Time
	NextClose  time.Time
	Indexes    []SymbolRead
	Regime     regime.Regime
	Calendar   []calendar.Event
	Watchlist  []SymbolRead
	// MarketHeadlines are the market-wide stories, separate from the per-symbol
	// ones on a SymbolRead.
	MarketHeadlines []market.Headline
	Gainers         []market.Mover
	Losers          []market.Mover
	Actives         []market.Active
	// Playbook is the user's strategy file, verbatim.
	Playbook string
	// Warnings lists sources that were unavailable, so the model and the reader
	// both know what the briefing was written without.
	Warnings []string
}

// Location resolves Timezone, falling back to whatever zone GeneratedAt carries.
// A briefing read back from the archive has only the name to go on.
func (in Input) Location() *time.Location {
	if in.Timezone != "" {
		if loc, err := time.LoadLocation(in.Timezone); err == nil {
			return loc
		}
	}
	if in.GeneratedAt.Location() != nil {
		return in.GeneratedAt.Location()
	}
	return time.UTC
}

type SymbolRead struct {
	Symbol    string
	Last      float64
	PrevClose float64
	ChangePct float64
	Headlines []market.Headline
}

// Output is the model's reply, validated against Schema before it is trusted.
// Numeric fields are pointers so a model can say "unknown" instead of inventing.
type Output struct {
	MarketRead   string      `json:"market_read"`
	RegimeNote   string      `json:"regime_note"`
	CalendarNote string      `json:"calendar_note"`
	Call         Call        `json:"call"`
	Watchlist    []WatchNote `json:"watchlist"`
	Risks        []string    `json:"risks"`
}

type WatchNote struct {
	Symbol string `json:"symbol"`
	// Bias is "bullish", "bearish", or "neutral".
	Bias string `json:"bias"`
	Note string `json:"note"`
}

// Direction is the model's falsifiable call for the session.
type Direction string

const (
	DirUp   Direction = "up"
	DirDown Direction = "down"
	DirFlat Direction = "flat"
)

// Call is graded at the close: "up" is correct when the instrument closes at
// least ThresholdPct above its open, "down" the mirror, "flat" when the move
// stays inside the threshold either way.
type Call struct {
	Instrument   string    `json:"instrument"`
	Direction    Direction `json:"direction"`
	ThresholdPct *float64  `json:"threshold_pct"`
	Rationale    string    `json:"rationale"`
	Invalidation string    `json:"invalidation"`
}

// Outcome is the result of scoring a Call against the session's open and close.
type Outcome struct {
	Open         float64
	Close        float64
	ActualPct    float64
	ThresholdPct float64
	Correct      bool
}

// Schema is the JSON schema Output is validated against. Implemented in schema.go.
func Schema() json.RawMessage {
	return schema()
}

// Score grades a call; defaultThresholdPct applies when the call left it null.
// Implemented in score.go.
func Score(c Call, open, close, defaultThresholdPct float64) (Outcome, error) {
	return score(c, open, close, defaultThresholdPct)
}
