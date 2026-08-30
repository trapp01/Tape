// Package broker defines the execution and market-data contracts every venue
// adapter implements. The CLI and trading packages only ever see these types,
// so swapping Alpaca paper for IBKR live is a new adapter, not a rewrite.
package broker

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotImplemented = errors.New("not implemented")
	ErrOrderNotFound  = errors.New("order not found")
)

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

type OrderType string

const (
	Market OrderType = "market"
	Limit  OrderType = "limit"
)

type OrderStatus string

// Statuses are normalised across venues; adapters map their own vocab onto these.
const (
	StatusNew             OrderStatus = "new"
	StatusAccepted        OrderStatus = "accepted"
	StatusPartiallyFilled OrderStatus = "partially_filled"
	StatusFilled          OrderStatus = "filled"
	StatusCanceled        OrderStatus = "canceled"
	StatusRejected        OrderStatus = "rejected"
	StatusExpired         OrderStatus = "expired"
)

// Terminal reports whether no further fills can arrive for this status.
func (s OrderStatus) Terminal() bool {
	switch s {
	case StatusFilled, StatusCanceled, StatusRejected, StatusExpired:
		return true
	}
	return false
}

type OrderRequest struct {
	Symbol     string
	Side       Side
	Qty        int
	Type       OrderType
	LimitPrice *float64
	// StopLoss and TakeProfit, when set, attach a bracket to the entry order.
	StopLoss   *float64
	TakeProfit *float64
	// ClientOrderID lets the journal correlate the venue's order with its own row.
	ClientOrderID string
}

type Order struct {
	ID            string
	ClientOrderID string
	Symbol        string
	Side          Side
	Qty           int
	Type          OrderType
	LimitPrice    *float64
	Status        OrderStatus
	// RawStatus is the venue's own status string, kept for the journal when the
	// normalised Status loses information (e.g. Alpaca's done_for_day).
	RawStatus      string
	FilledQty      int
	FilledAvgPrice *float64
	SubmittedAt    time.Time
	FilledAt       *time.Time
	// Legs holds bracket children (stop-loss / take-profit) when the venue returns them.
	Legs []Order
}

type Position struct {
	Symbol        string
	Qty           int
	AvgEntryPrice float64
	CurrentPrice  float64
	MarketValue   float64
	UnrealizedPL  float64
}

type Account struct {
	ID          string
	Equity      float64
	Cash        float64
	BuyingPower float64
	Paper       bool
}

type Quote struct {
	Symbol    string
	Bid       float64
	BidSize   int
	Ask       float64
	AskSize   int
	Last      float64
	Timestamp time.Time
}

type Clock struct {
	Now       time.Time
	IsOpen    bool
	NextOpen  time.Time
	NextClose time.Time
}

type ListOrdersFilter struct {
	// After limits results to orders submitted after this time. Zero means no bound.
	After time.Time
	// OpenOnly returns only non-terminal orders.
	OpenOnly bool
	Limit    int
}

// Broker is the execution contract. All methods must be safe to call from one goroutine
// at a time; adapters are not required to be concurrency-safe.
type Broker interface {
	Name() string
	Account(ctx context.Context) (Account, error)
	Positions(ctx context.Context) ([]Position, error)
	SubmitOrder(ctx context.Context, req OrderRequest) (Order, error)
	GetOrder(ctx context.Context, id string) (Order, error)
	ListOrders(ctx context.Context, f ListOrdersFilter) ([]Order, error)
	CancelOrder(ctx context.Context, id string) error
	// ClosePosition submits a market order to flatten one symbol.
	ClosePosition(ctx context.Context, symbol string) (Order, error)
	// CloseAllPositions flattens everything and cancels open orders.
	CloseAllPositions(ctx context.Context) ([]Order, error)
	Clock(ctx context.Context) (Clock, error)
}

// MarketData is the quote contract. Quote streams block until ctx is cancelled.
type MarketData interface {
	Quote(ctx context.Context, symbol string) (Quote, error)
	Quotes(ctx context.Context, symbols []string) (map[string]Quote, error)
	StreamQuotes(ctx context.Context, symbols []string, onQuote func(Quote)) error
}
