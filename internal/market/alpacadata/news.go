package alpacadata

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"

	"github.com/trapp01/tape/internal/market"
)

// News returns headlines published since `since`, newest first. Article bodies are
// left out: the briefing shows the headline and summary, and the bodies are large
// enough to crowd a prompt.
func (c *Client) News(ctx context.Context, symbols []string, since time.Time, limit int) ([]market.Headline, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("alpacadata: news %s: limit must be positive, got %d", newsScope(symbols), limit)
	}
	raw, err := call(ctx, func() ([]marketdata.News, error) {
		return c.data.GetNews(marketdata.GetNewsRequest{
			Symbols:    symbols,
			Start:      since,
			Sort:       marketdata.SortDesc,
			TotalLimit: limit,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("alpacadata: news %s since %s: %w", newsScope(symbols), since.UTC().Format(time.RFC3339), err)
	}

	out := make([]market.Headline, 0, len(raw))
	for _, n := range raw {
		out = append(out, market.Headline{
			ID:       strconv.Itoa(n.ID),
			Headline: n.Headline,
			Summary:  n.Summary,
			// The SDK's News carries no source field, so the byline stands in for it.
			Source:    n.Author,
			URL:       n.URL,
			Symbols:   n.Symbols,
			CreatedAt: n.CreatedAt,
		})
	}
	return out, nil
}

// newsScope names what an error was about: a symbol list, or the whole wire.
func newsScope(symbols []string) string {
	if len(symbols) == 0 {
		return "all symbols"
	}
	return strings.Join(symbols, ",")
}
