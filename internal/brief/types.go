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
	"github.com/trapp01/tape/internal/risk"
)

// Input is the complete context handed to the model, archived verbatim so a
// briefing can be re-read later next to what actually happened.
type Input struct {
	GeneratedAt time.Time `json:"generated_at"`
	Timezone    string    `json:"timezone"`
	Mode        string    `json:"mode"`
	LedgerCash  float64   `json:"ledger_cash"`
	// Equity is what the slate was sized against, and Limits are the walls it was
	// sized inside. Both are archived so an old proposal's share count still adds up.
	Equity float64     `json:"equity"`
	Limits risk.Limits `json:"limits"`
	// FreeCash is ledger cash less what open orders already claim: the ceiling on
	// what one idea may cost, archived next to the size it produced.
	FreeCash float64 `json:"free_cash"`
	// MarketOpen and NextOpen come from the venue clock.
	MarketOpen bool             `json:"market_open"`
	NextOpen   time.Time        `json:"next_open"`
	NextClose  time.Time        `json:"next_close"`
	Indexes    []SymbolRead     `json:"indexes"`
	Regime     regime.Regime    `json:"regime"`
	Calendar   []calendar.Event `json:"calendar"`
	Watchlist  []SymbolRead     `json:"watchlist"`
	// MarketHeadlines are the market-wide stories, separate from the per-symbol
	// ones on a SymbolRead.
	MarketHeadlines []market.Headline `json:"market_headlines"`
	Gainers         []market.Mover    `json:"gainers"`
	Losers          []market.Mover    `json:"losers"`
	Actives         []market.Active   `json:"actives"`
	// Playbook is the user's strategy file, verbatim.
	Playbook string `json:"playbook"`
	// Warnings lists sources that were unavailable, so the model and the reader
	// both know what the briefing was written without.
	Warnings []string `json:"warnings"`
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
	Symbol    string            `json:"symbol"`
	Last      float64           `json:"last"`
	PrevClose float64           `json:"prev_close"`
	ChangePct float64           `json:"change_pct"`
	Headlines []market.Headline `json:"headlines"`
}

// Output is the model's reply, validated against Schema before it is trusted.
// Numeric fields are pointers so a model can say "unknown" instead of inventing.
type Output struct {
	MarketRead   string `json:"market_read"`
	RegimeNote   string `json:"regime_note"`
	CalendarNote string `json:"calendar_note"`
	Call         Call   `json:"call"`
	// Proposals are the session's trade ideas, unsized: SizeProposals turns each
	// one into a share count from the risk limits.
	Proposals []Proposal  `json:"proposals"`
	Watchlist []WatchNote `json:"watchlist"`
	Risks     []string    `json:"risks"`
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
