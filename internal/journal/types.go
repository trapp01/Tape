// Package journal is the SQLite record of every order, fill, and (later) every
// AI proposal and human decision. Stats are computed from this file, never from
// the broker, so the numbers reflect tape's cost model and ledger size.
package journal

import "time"

// Source records who originated an order.
const (
	SourceHuman    = "human"
	SourceProposal = "proposal"
	SourceEOD      = "eod"
)

// Mode keeps paper and live records in one file without mixing their stats.
const (
	ModePaper = "paper"
	ModeLive  = "live"
)

type Order struct {
	ID            int64
	BrokerOrderID string
	ClientOrderID string
	Symbol        string
	Side          string
	Qty           int
	Type          string
	LimitPrice    *float64
	StopLoss      *float64
	TakeProfit    *float64
	Status        string
	// FilledQty and FilledAvgPrice mirror the venue's running total for the order;
	// the fills table holds the executions the stats are actually built from.
	FilledQty      int
	FilledAvgPrice *float64
	Source         string
	// Mode is "paper" or "live" so a single journal can hold both records apart.
	Mode        string
	Note        string
	SubmittedAt time.Time
	UpdatedAt   time.Time
}

// Fill is one execution with the cost model already applied. RawPrice is what the
// venue reported; ModeledPrice includes slippage, and Commission/Fees are what a
// real broker would have charged.
type Fill struct {
	ID           int64
	OrderID      int64
	BrokerFillID string
	Symbol       string
	Side         string
	Qty          int
	RawPrice     float64
	ModeledPrice float64
	Commission   float64
	Fees         float64
	FilledAt     time.Time
}

// Trade is a completed round trip, built FIFO from fills.
type Trade struct {
	ID            int64
	Symbol        string
	Qty           int
	EntryAvgPrice float64
	ExitAvgPrice  float64
	OpenedAt      time.Time
	ClosedAt      time.Time
	GrossPL       float64
	Costs         float64
	NetPL         float64
}

type OpenPosition struct {
	Symbol        string
	Qty           int
	AvgEntryPrice float64
	CostBasis     float64
	OpenedAt      time.Time
}

// Ledger is tape's own view of the account, derived from fills and the configured
// starting equity. Alpaca's $100k paper balance never enters these numbers.
type Ledger struct {
	StartingEquity float64
	Cash           float64
	RealizedPL     float64
	Commissions    float64
	Fees           float64
	OpenPositions  []OpenPosition
}

type DayRecap struct {
	Day         time.Time
	Trades      []Trade
	OrdersCount int
	GrossPL     float64
	// Costs is the share of commissions and fees allocated to trades closed today.
	Costs float64
	// FillCosts is commissions and fees on every fill in the day, including the
	// ones that opened a position still held tonight.
	FillCosts float64
	NetPL     float64
	Wins      int
	Losses    int
}
