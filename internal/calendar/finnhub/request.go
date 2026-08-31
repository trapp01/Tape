package finnhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// get runs the request with one retry on 429 or 5xx.
func (p *Provider) get(ctx context.Context, q url.Values, out any) error {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			if err := sleep(ctx, p.delay); err != nil {
				return fmt.Errorf("finnhub: %w (last error: %v)", err, lastErr)
			}
		}
		retryable, err := p.attempt(ctx, q, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}
	return lastErr
}

// attempt performs one round trip. The bool reports whether a retry is worth it.
// Errors name the bare endpoint, never the query, which carries the token.
func (p *Provider) attempt(ctx context.Context, q url.Values, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"?"+q.Encode(), nil)
	if err != nil {
		return false, fmt.Errorf("finnhub: building the request: %w", err)
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return ctx.Err() == nil, fmt.Errorf("finnhub: calling %s: %w", endpoint, redact(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return true, fmt.Errorf("finnhub: reading the response from %s: %w", endpoint, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return retryable, fmt.Errorf("finnhub: %s returned %s: %s", endpoint, resp.Status, excerpt(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("finnhub: decoding the response from %s: %w (body: %s)", endpoint, err, excerpt(body))
	}
	return false, nil
}

// redact drops the URL a *url.Error carries, because the query holds the token.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func excerpt(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= excerptLimit {
		return s
	}
	return s[:excerptLimit] + "..."
}

func eastern() (*time.Location, error) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		return nil, fmt.Errorf("finnhub: loading America/New_York: %w", err)
	}
	return et, nil
}
