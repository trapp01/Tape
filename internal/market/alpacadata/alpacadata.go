// Package alpacadata reads Alpaca's market-data API for the morning briefing.
// It is read-only: nothing here can reach the trading API or place an order.
package alpacadata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/trapp01/tape/internal/market"
)

const (
	// defaultDataBaseURL is the market-data host. The SDK defaults to it, but the
	// screener calls build their own URLs and need it spelled out.
	defaultDataBaseURL = "https://data.alpaca.markets"
	// requestTimeout bounds the screener calls the SDK does not implement.
	requestTimeout = 15 * time.Second
)

var (
	_ market.SnapshotProvider = (*Client)(nil)
	_ market.BarsProvider     = (*Client)(nil)
	_ market.SessionProvider  = (*Client)(nil)
	_ market.IntradayProvider = (*Client)(nil)
	_ market.MoversProvider   = (*Client)(nil)
	_ market.NewsProvider     = (*Client)(nil)
)

// Options configures the reader with the same Alpaca keys the broker uses.
type Options struct {
	APIKey    string
	APISecret string
	// DataFeed is "iex" (free) or "sip" (paid). Empty means "iex".
	DataFeed string

	// Test seam. Empty means Alpaca's real data host.
	dataBaseURL string
}

// Client reads snapshots, daily bars, news, and the screener from one data feed.
type Client struct {
	data *marketdata.Client
	// http serves the screener endpoints, which the SDK does not implement.
	http *http.Client

	apiKey      string
	apiSecret   string
	feed        string
	dataBaseURL string
}

// New builds a data client. It never reaches the network.
func New(opts Options) (*Client, error) {
	if opts.APIKey == "" || opts.APISecret == "" {
		return nil, fmt.Errorf("alpacadata: ALPACA_API_KEY / ALPACA_API_SECRET not set (free paper keys: https://app.alpaca.markets): %w", market.ErrNotConfigured)
	}

	feed := opts.DataFeed
	if feed == "" {
		feed = marketdata.IEX
	}
	// Always setting BaseURL keeps APCA_API_DATA_URL from redirecting us.
	base := opts.dataBaseURL
	if base == "" {
		base = defaultDataBaseURL
	}

	return &Client{
		data: marketdata.NewClient(marketdata.ClientOpts{
			APIKey:    opts.APIKey,
			APISecret: opts.APISecret,
			BaseURL:   base,
			Feed:      feed,
		}),
		http:        &http.Client{Timeout: requestTimeout},
		apiKey:      opts.APIKey,
		apiSecret:   opts.APISecret,
		feed:        feed,
		dataBaseURL: strings.TrimSuffix(base, "/"),
	}, nil
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
