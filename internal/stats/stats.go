// Package stats turns the journal into the numbers the gate is judged on.
// Everything here is computed from records; nothing reads a venue.
package stats

import (
	"context"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// Source is the slice of the journal the report reads. *journal.Store
// satisfies it; tests use a fake.
type Source interface {
	ClosedTrades(ctx context.Context, from, to time.Time, mode string) ([]journal.Trade, error)
	Fills(ctx context.Context, from, to time.Time, mode string) ([]journal.Fill, error)
	Ledger(ctx context.Context, mode string) (journal.Ledger, error)
	ProposalsInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.Proposal, error)
	OutcomesInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.ProposalOutcome, error)
	CallsInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.Call, error)
	RefusalsInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.Refusal, error)
	OrdersByIDs(ctx context.Context, ids []int64) (map[int64]journal.Order, error)
	ProposalsByIDs(ctx context.Context, ids []int64) (map[int64]journal.Proposal, error)
	// BriefingsInRange supplies the archived regime per day for the by-regime cut.
	BriefingsInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.Briefing, error)
	NoteScoresInRange(ctx context.Context, mode, fromDay, toDay string) ([]journal.NoteScore, error)
	// LatestPlaybookVersion returns journal.ErrNotFound when the playbook has
	// never changed; the gate window then starts at the report window.
	LatestPlaybookVersion(ctx context.Context) (journal.PlaybookVersion, error)
}

// Window is the span a report covers, in the user's zone.
type Window struct {
	From time.Time
	To   time.Time
	Loc  *time.Location
	Mode string
}

// Gate is the real-money threshold from config. Every field is a floor or a
// ceiling the report checks itself against.
type Gate struct {
	MinMonths            int
	MinSessions          int
	MinTrades            int
	MinProfitFactor      float64
	MaxDrawdownPct       float64
	MinExpectancyUSD     float64
	MaxRefusalsLastMonth int
	// MaxNullPassRate caps how often a zero-edge trader with this record's trade
	// structure and sample size would clear the thresholds above. A gate a coin
	// flip passes is not a gate.
	MaxNullPassRate float64
}

// Significance is the answer to "could noise have produced this record".
type Significance struct {
	// NullWinRate is the break-even win probability implied by the record's
	// average win and loss sizes; the null trader wins that often.
	NullWinRate float64
	// NullPassRate is the fraction of simulated zero-edge records, at this sample
	// size, that clear the profit-factor and drawdown thresholds.
	NullPassRate float64
	// ExpectancyCI95Low is the bootstrap lower bound on expectancy (USD/trade).
	ExpectancyCI95Low float64
	Paths             int
}

// RegimeStats is one row of the by-regime cut, keyed by the archived label.
type RegimeStats struct {
	Label          string
	Sessions       int
	Calls          CallStats
	Notes          CallStats
	Trades         TradeStats
	Counterfactual CounterfactualStats
}

// Report is what `tape stats` prints and what the gate reads.
type Report struct {
	Window   Window
	Sessions int

	Trades   TradeStats
	BySetup  []SetupStats
	ByRegime []RegimeStats
	Calls    CallStats
	// Notes grades the watchlist bias notes the same way as the call.
	Notes        CallStats
	Proposals    ProposalStats
	Refusals     RefusalStats
	Equity       EquityStats
	Significance Significance
	// GateWindowFrom is where the gate actually starts reading: the later of the
	// report window and the last playbook or config change. Sessions before a
	// change were traded under different rules and are fitted, not evidence.
	GateWindowFrom time.Time
	GateResetAt    *time.Time
	GateChecks     []GateCheck
	GateOpen       bool
}

type TradeStats struct {
	Count   int
	Wins    int
	Losses  int
	WinRate float64
	// AvgLossUSD is the mean of the losing trades, so it is negative.
	AvgWinUSD  float64
	AvgLossUSD float64
	// ExpectancyUSD is mean net P&L per trade, wins and losses together.
	ExpectancyUSD float64
	// ProfitFactor is gross profit over gross loss. A record with no losing trade
	// has no divisor, so the field carries gross profit instead of a ratio.
	ProfitFactor float64
	GrossPL      float64
	Costs        float64
	NetPL        float64
}

type SetupStats struct {
	SetupID string
	Trades  TradeStats
	// Counterfactual is the replay of every proposal citing this setup,
	// taken or not.
	Counterfactual CounterfactualStats
}

type CallStats struct {
	Total    int
	Correct  int
	Accuracy float64
	Pending  int
	// WithinNoiseBand counts grades whose actual move sat within 5 bps of the
	// threshold: decided inside the feed's measurement error, not by the read.
	WithinNoiseBand int
}

type ProposalStats struct {
	Proposed, Taken, Passed, Rejected, Expired, Unfilled int
	// PassesThatWouldHaveProfited counts passes whose replay ended positive;
	// MissedNetUSD is their summed net P&L (what the vetoes left on the table).
	PassesThatWouldHaveProfited int
	MissedNetUSD                float64
	// VetoedLossesAvoidedUSD is the summed replayed loss of passes that would
	// have lost: what the vetoes saved.
	VetoedLossesAvoidedUSD float64
	// ExecutionDragUSD is Σ(replayed net − actual net) over taken proposals.
	ExecutionDragUSD float64
	Counterfactual   CounterfactualStats
}

type CounterfactualStats struct {
	Replayed     int
	Filled       int
	Wins         int
	Losses       int
	NetPL        float64
	AvgRMultiple float64
	// Ambiguous counts replays whose exit the stop-first convention decided.
	Ambiguous int
}

type RefusalStats struct {
	Total     int
	ByRule    map[string]int
	LastMonth int
}

// EquityStats is the account, which no report window may move: the curve runs
// over the whole record through the window's end. Only the two Window fields
// describe the slice the report covers.
type EquityStats struct {
	StartingEquity float64
	EndingEquity   float64
	ReturnPct      float64
	MaxDrawdownUSD float64
	MaxDrawdownPct float64
	// WindowNetPL is what the report window's own closed trades netted.
	WindowNetPL float64
	// WindowReturnPct is WindowNetPL against the equity the window opened at.
	WindowReturnPct float64
}

type GateCheck struct {
	Name   string
	Passed bool
	Actual string
	Needed string
}

// Compute builds the report for w and checks it against g. Implemented in compute.go.
func Compute(ctx context.Context, src Source, w Window, g Gate) (Report, error) {
	return compute(ctx, src, w, g)
}
