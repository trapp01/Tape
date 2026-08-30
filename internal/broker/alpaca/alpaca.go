// Package alpaca adapts Alpaca's paper-trading and market-data APIs to the
// broker contract. Live trading is refused here, not just in config.
package alpaca

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/shopspring/decimal"

	"github.com/trapp01/tape/internal/broker"
)

const (
	paperBaseURL = "https://paper-api.alpaca.markets"
	// requestTimeout matches the SDK's own default so both clients give up together.
	requestTimeout = 10 * time.Second
)

var (
	_ broker.Broker     = (*Client)(nil)
	_ broker.MarketData = (*Client)(nil)
)

// Options configures the adapter. Paper must be true; live is not built.
type Options struct {
	APIKey    string
	APISecret string
	Paper     bool
	// DataFeed is "iex" (free) or "sip". Empty means "iex".
	DataFeed string

	// Test seams. Empty means Alpaca's real hosts.
	tradingBaseURL string
	dataBaseURL    string
	streamBaseURL  string
}

// Client talks to one Alpaca paper account and its market-data feed.
type Client struct {
	trading *sdk.Client
	data    *marketdata.Client
	// http serves the one trading call the SDK cannot express: a single order
	// fetched with nested=true.
	http *http.Client

	apiKey         string
	apiSecret      string
	feed           string
	tradingBaseURL string
	streamBaseURL  string
}

// New builds a paper-trading client. It never reaches the network.
func New(opts Options) (*Client, error) {
	if opts.APIKey == "" || opts.APISecret == "" {
		return nil, errors.New("alpaca: ALPACA_API_KEY / ALPACA_API_SECRET not set (free paper keys: https://app.alpaca.markets)")
	}
	if !opts.Paper {
		return nil, errors.New("alpaca: live trading is not implemented and the real-money gate is closed; run with mode = \"paper\"")
	}

	feed := opts.DataFeed
	if feed == "" {
		feed = marketdata.IEX
	}
	// Always setting BaseURL keeps APCA_API_BASE_URL from redirecting us at live.
	tradingURL := opts.tradingBaseURL
	if tradingURL == "" {
		tradingURL = paperBaseURL
	}

	return &Client{
		trading: sdk.NewClient(sdk.ClientOpts{
			APIKey:    opts.APIKey,
			APISecret: opts.APISecret,
			BaseURL:   tradingURL,
		}),
		data: marketdata.NewClient(marketdata.ClientOpts{
			APIKey:    opts.APIKey,
			APISecret: opts.APISecret,
			BaseURL:   opts.dataBaseURL,
			Feed:      feed,
		}),
		http:           &http.Client{Timeout: requestTimeout},
		apiKey:         opts.APIKey,
		apiSecret:      opts.APISecret,
		feed:           feed,
		tradingBaseURL: tradingURL,
		streamBaseURL:  opts.streamBaseURL,
	}, nil
}

func (c *Client) Name() string { return "alpaca" }

func (c *Client) Account(ctx context.Context) (broker.Account, error) {
	acct, err := call(ctx, c.trading.GetAccount)
	if err != nil {
		return broker.Account{}, fmt.Errorf("alpaca: get account: %w", err)
	}
	return broker.Account{
		ID:          acct.ID,
		Equity:      acct.Equity.InexactFloat64(),
		Cash:        acct.Cash.InexactFloat64(),
		BuyingPower: acct.BuyingPower.InexactFloat64(),
		Paper:       true,
	}, nil
}

func (c *Client) Positions(ctx context.Context) ([]broker.Position, error) {
	raw, err := call(ctx, c.trading.GetPositions)
	if err != nil {
		return nil, fmt.Errorf("alpaca: list positions: %w", err)
	}
	out := make([]broker.Position, 0, len(raw))
	for _, p := range raw {
		out = append(out, toPosition(p))
	}
	return out, nil
}

// ClosePosition liquidates the whole position at market.
func (c *Client) ClosePosition(ctx context.Context, symbol string) (broker.Order, error) {
	order, err := call(ctx, func() (*sdk.Order, error) {
		return c.trading.ClosePosition(symbol, sdk.ClosePositionRequest{})
	})
	if err != nil {
		return broker.Order{}, fmt.Errorf("alpaca: close position %s: %w", symbol, err)
	}
	return toOrder(*order), nil
}

// CloseAllPositions flattens everything and cancels resting orders. Alpaca reports
// per-symbol failures, so the orders that did go through come back alongside the error.
func (c *Client) CloseAllPositions(ctx context.Context) ([]broker.Order, error) {
	raw, err := call(ctx, func() ([]sdk.Order, error) {
		return c.trading.CloseAllPositions(sdk.CloseAllPositionsRequest{CancelOrders: true})
	})
	out := make([]broker.Order, 0, len(raw))
	for _, o := range raw {
		out = append(out, toOrder(o))
	}
	if err != nil {
		return out, fmt.Errorf("alpaca: close all positions: %w", err)
	}
	return out, nil
}

func (c *Client) Clock(ctx context.Context) (broker.Clock, error) {
	clk, err := call(ctx, c.trading.GetClock)
	if err != nil {
		return broker.Clock{}, fmt.Errorf("alpaca: get clock: %w", err)
	}
	return broker.Clock{
		Now:       clk.Timestamp,
		IsOpen:    clk.IsOpen,
		NextOpen:  clk.NextOpen,
		NextClose: clk.NextClose,
	}, nil
}

func toPosition(p sdk.Position) broker.Position {
	return broker.Position{
		Symbol:        p.Symbol,
		Qty:           int(p.Qty.IntPart()),
		AvgEntryPrice: p.AvgEntryPrice.InexactFloat64(),
		CurrentPrice:  decimalValue(p.CurrentPrice),
		MarketValue:   decimalValue(p.MarketValue),
		UnrealizedPL:  decimalValue(p.UnrealizedPL),
	}
}

// call runs an SDK request on its own goroutine because the SDK takes no context.
// A cancelled caller returns immediately; the stranded request ends at the HTTP timeout.
func call[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{value: v, err: err}
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case r := <-done:
		return r.value, r.err
	}
}

// notFound reports whether err is Alpaca's 404 for a missing resource.
func notFound(err error) bool {
	var apiErr *sdk.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func decimalValue(d *decimal.Decimal) float64 {
	if d == nil {
		return 0
	}
	return d.InexactFloat64()
}

func decimalPtr(d *decimal.Decimal) *float64 {
	if d == nil {
		return nil
	}
	f := d.InexactFloat64()
	return &f
}
