package alpacadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/trapp01/tape/internal/market"
)

const (
	// errBodyLimit caps how much of a failed response is quoted back in an error.
	errBodyLimit = 512
	// The screener rejects a `top` outside these documented ranges with a 400.
	moversMaxTop  = 50
	activesMaxTop = 100
	defaultTop    = 10
)

// moverJSON is the screener's shape for one gainer or loser.
type moverJSON struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	Change        float64 `json:"change"`
	PercentChange float64 `json:"percent_change"`
}

// TopMovers returns the session's biggest gainers and losers. The SDK has no
// screener call, so this goes straight to the data API.
func (c *Client) TopMovers(ctx context.Context, top int) (gainers, losers []market.Mover, err error) {
	n := clampTop(top, moversMaxTop)
	var body struct {
		Gainers []moverJSON `json:"gainers"`
		Losers  []moverJSON `json:"losers"`
	}
	q := url.Values{"top": {strconv.Itoa(n)}}
	if err = c.getJSON(ctx, "/v1beta1/screener/stocks/movers", q, &body); err != nil {
		return nil, nil, fmt.Errorf("alpacadata: top movers (top %d): %w", n, err)
	}
	return toMovers(body.Gainers), toMovers(body.Losers), nil
}

// MostActives returns the session's highest-volume names.
func (c *Client) MostActives(ctx context.Context, top int) ([]market.Active, error) {
	n := clampTop(top, activesMaxTop)
	var body struct {
		MostActives []struct {
			Symbol     string  `json:"symbol"`
			Volume     float64 `json:"volume"`
			TradeCount int64   `json:"trade_count"`
		} `json:"most_actives"`
	}
	q := url.Values{"by": {"volume"}, "top": {strconv.Itoa(n)}}
	if err := c.getJSON(ctx, "/v1beta1/screener/stocks/most-actives", q, &body); err != nil {
		return nil, fmt.Errorf("alpacadata: most actives (top %d): %w", n, err)
	}

	out := make([]market.Active, 0, len(body.MostActives))
	for _, a := range body.MostActives {
		out = append(out, market.Active{Symbol: a.Symbol, Volume: a.Volume, TradeCount: a.TradeCount})
	}
	return out, nil
}

func toMovers(raw []moverJSON) []market.Mover {
	out := make([]market.Mover, 0, len(raw))
	for _, m := range raw {
		out = append(out, market.Mover{
			Symbol:     m.Symbol,
			Price:      m.Price,
			Change:     m.Change,
			PercentChg: m.PercentChange,
		})
	}
	return out
}

// clampTop keeps `top` inside the venue's range; an out-of-range value is a 400
// from the screener, not a shorter list.
func clampTop(top, ceiling int) int {
	switch {
	case top <= 0:
		return defaultTop
	case top > ceiling:
		return ceiling
	default:
		return top
	}
}

// getJSON runs one data-API request, retrying a rate limit or a server fault once.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.dataBaseURL + path + "?" + query.Encode()

	body, retry, err := c.get(ctx, endpoint)
	if err != nil && retry {
		body, _, err = c.get(ctx, endpoint)
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding the response: %w", err)
	}
	return nil
}

// get returns the response body and whether the failure is worth one retry: a
// rate limit or a server fault, never a rejected key.
func (c *Client) get(ctx context.Context, endpoint string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", c.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", c.apiSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		return nil, retry, fmt.Errorf("venue returned %s: %s", resp.Status, strings.TrimSpace(string(excerpt)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("reading the response: %w", err)
	}
	return body, false, nil
}
