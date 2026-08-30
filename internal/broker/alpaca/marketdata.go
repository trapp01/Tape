package alpaca

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata/stream"

	"github.com/trapp01/tape/internal/broker"
)

// ErrNoQuote means the feed returned nothing for a symbol, usually a bad ticker or
// a name the iex feed does not carry.
var ErrNoQuote = errors.New("no quote available")

func (c *Client) Quote(ctx context.Context, symbol string) (broker.Quote, error) {
	quotes, err := c.Quotes(ctx, []string{symbol})
	if err != nil {
		return broker.Quote{}, err
	}
	q, ok := quotes[symbol]
	if !ok {
		return broker.Quote{}, fmt.Errorf("alpaca: get quote %s (feed %s): %w", symbol, c.feed, ErrNoQuote)
	}
	return q, nil
}

func (c *Client) Quotes(ctx context.Context, symbols []string) (map[string]broker.Quote, error) {
	if len(symbols) == 0 {
		return map[string]broker.Quote{}, nil
	}
	raw, err := call(ctx, func() (map[string]marketdata.Quote, error) {
		return c.data.GetLatestQuotes(symbols, marketdata.GetLatestQuoteRequest{Feed: c.feed})
	})
	if err != nil {
		return nil, fmt.Errorf("alpaca: get quotes %s (feed %s): %w", strings.Join(symbols, ","), c.feed, err)
	}
	out := make(map[string]broker.Quote, len(raw))
	for symbol, q := range raw {
		out[symbol] = broker.Quote{
			Symbol:    symbol,
			Bid:       q.BidPrice,
			BidSize:   int(q.BidSize),
			Ask:       q.AskPrice,
			AskSize:   int(q.AskSize),
			Last:      midpoint(q.BidPrice, q.AskPrice),
			Timestamp: q.Timestamp,
		}
	}
	return out, nil
}

// StreamQuotes blocks until ctx is cancelled or the websocket gives up. onQuote runs
// on the SDK's processor goroutine, so it must not block.
func (c *Client) StreamQuotes(ctx context.Context, symbols []string, onQuote func(broker.Quote)) error {
	if len(symbols) == 0 {
		return errors.New("alpaca: stream quotes: no symbols given")
	}
	if onQuote == nil {
		return errors.New("alpaca: stream quotes: onQuote is nil")
	}
	what := fmt.Sprintf("stream quotes %s (feed %s)", strings.Join(symbols, ","), c.feed)

	opts := []stream.StockOption{
		stream.WithCredentials(c.apiKey, c.apiSecret),
		stream.WithQuotes(func(q stream.Quote) {
			onQuote(broker.Quote{
				Symbol:    q.Symbol,
				Bid:       q.BidPrice,
				BidSize:   int(q.BidSize),
				Ask:       q.AskPrice,
				AskSize:   int(q.AskSize),
				Last:      midpoint(q.BidPrice, q.AskPrice),
				Timestamp: q.Timestamp,
			})
		}, symbols...),
	}
	if c.streamBaseURL != "" {
		opts = append(opts, stream.WithBaseURL(c.streamBaseURL))
	}

	client := stream.NewStocksClient(c.feed, opts...)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("alpaca: %s: %w", what, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-client.Terminated():
		if err != nil {
			return fmt.Errorf("alpaca: %s: %w", what, err)
		}
		return ctx.Err()
	}
}

// midpoint stands in for a trade price; the quote endpoints carry no last trade.
func midpoint(bid, ask float64) float64 {
	if bid <= 0 || ask <= 0 {
		return 0
	}
	return (bid + ask) / 2
}
