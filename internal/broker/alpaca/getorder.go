package alpaca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"

	"github.com/trapp01/tape/internal/broker"
)

// errBodyLimit caps how much of a failed response is quoted back in an error.
const errBodyLimit = 512

// getOrderNested fetches one order with its bracket children. The SDK's GetOrder
// sends no nested flag, so a stop or target that fired would never reach the
// journal and the position would look open forever.
func (c *Client) getOrderNested(ctx context.Context, id string) (sdk.Order, error) {
	endpoint := fmt.Sprintf("%s/v2/orders/%s?nested=true",
		strings.TrimSuffix(c.tradingBaseURL, "/"), url.PathEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sdk.Order{}, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", c.apiKey)
	req.Header.Set("APCA-API-SECRET-KEY", c.apiSecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return sdk.Order{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return sdk.Order{}, broker.ErrOrderNotFound
	case resp.StatusCode >= http.StatusMultipleChoices:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		return sdk.Order{}, fmt.Errorf("venue returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var order sdk.Order
	if err := json.NewDecoder(resp.Body).Decode(&order); err != nil {
		return sdk.Order{}, fmt.Errorf("decoding the order: %w", err)
	}
	return order, nil
}
