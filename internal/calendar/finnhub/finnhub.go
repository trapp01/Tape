// Package finnhub reads the earnings calendar from Finnhub and keeps only the
// watchlist symbols. One request per call; the free tier is metered per minute.
package finnhub

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trapp01/tape/internal/calendar"
)

const (
	endpoint       = "https://finnhub.io/api/v1/calendar/earnings"
	requestTimeout = 10 * time.Second
	attempts       = 2
	baseDelay      = 500 * time.Millisecond
	maxBodyBytes   = 8 << 20
	excerptLimit   = 512
	dateLayout     = "2006-01-02"
	source         = "finnhub.io"
)

var _ calendar.EarningsProvider = (*Provider)(nil)

// Provider queries Finnhub once per call and filters symbols in memory, because
// the endpoint takes no symbol list.
type Provider struct {
	token   string
	baseURL string
	http    *http.Client
	delay   time.Duration
}

// New builds a provider. An empty key is ErrNotConfigured, not a failure saved
// for the middle of a briefing.
func New(apiKey string) (*Provider, error) {
	if apiKey == "" {
		return nil, calendar.NotConfigured("FINNHUB_API_KEY not set (free key: https://finnhub.io/register)")
	}
	return &Provider{
		token:   apiKey,
		baseURL: endpoint,
		http:    &http.Client{Timeout: requestTimeout},
		delay:   baseDelay,
	}, nil
}

func (p *Provider) Name() string { return "Finnhub" }

// earningsRow is the documented shape of a calendar/earnings entry. Estimates
// are pointers so a missing one stays out of the detail line.
type earningsRow struct {
	Date            string   `json:"date"`
	Symbol          string   `json:"symbol"`
	Hour            string   `json:"hour"`
	Year            int      `json:"year"`
	Quarter         int      `json:"quarter"`
	EPSEstimate     *float64 `json:"epsEstimate"`
	RevenueEstimate *float64 `json:"revenueEstimate"`
}

type calendarResponse struct {
	EarningsCalendar []earningsRow `json:"earningsCalendar"`
}

// Earnings returns reports for the given symbols dated inside [from, to]. An
// empty symbol list skips the request entirely.
func (p *Provider) Earnings(ctx context.Context, symbols []string, from, to time.Time) ([]calendar.Event, error) {
	wanted := upperSet(symbols)
	if len(wanted) == 0 {
		return nil, nil
	}
	et, err := eastern()
	if err != nil {
		return nil, err
	}
	first, last := from.In(et).Format(dateLayout), to.In(et).Format(dateLayout)

	var payload calendarResponse
	q := url.Values{"from": {first}, "to": {last}, "token": {p.token}}
	if err := p.get(ctx, q, &payload); err != nil {
		return nil, err
	}

	out := make([]calendar.Event, 0, len(wanted))
	for _, row := range payload.EarningsCalendar {
		symbol := strings.ToUpper(row.Symbol)
		if !wanted[symbol] || row.Date < first || row.Date > last {
			continue
		}
		day, err := time.ParseInLocation(dateLayout, row.Date, et)
		if err != nil {
			return nil, fmt.Errorf("finnhub: %s: unreadable date %q", symbol, row.Date)
		}
		out = append(out, newEvent(symbol, row, day, et))
	}
	return out, nil
}

func newEvent(symbol string, row earningsRow, day time.Time, et *time.Location) calendar.Event {
	ev := calendar.Event{
		Kind:   calendar.KindEarnings,
		Title:  symbol + " earnings",
		Symbol: symbol,
		Impact: calendar.ImpactHigh,
		Source: source,
	}

	var parts []string
	switch strings.ToLower(row.Hour) {
	case "bmo":
		ev.At = at(day, 7, 0, et)
		parts = append(parts, "before open")
	case "amc":
		ev.At = at(day, 16, 30, et)
		parts = append(parts, "after close")
	default:
		// Noon Eastern keeps the calendar day the same once the time is UTC.
		ev.At = at(day, 12, 0, et)
		ev.AllDay = true
		if strings.EqualFold(row.Hour, "dmh") {
			parts = append(parts, "during market hours")
		}
	}
	if row.Quarter > 0 && row.Year > 0 {
		parts = append(parts, fmt.Sprintf("Q%d %d", row.Quarter, row.Year))
	}
	if row.EPSEstimate != nil {
		parts = append(parts, fmt.Sprintf("EPS est %.2f", *row.EPSEstimate))
	}
	if row.RevenueEstimate != nil {
		parts = append(parts, "revenue est "+compact(*row.RevenueEstimate))
	}
	ev.Detail = strings.Join(parts, ", ")
	return ev
}

func at(day time.Time, hour, minute int, et *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, et).UTC()
}

// compact renders a revenue estimate short enough for a calendar line.
func compact(v float64) string {
	switch abs := math.Abs(v); {
	case abs >= 1e9:
		return fmt.Sprintf("$%.1fB", v/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("$%.0fM", v/1e6)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}

func upperSet(symbols []string) map[string]bool {
	set := make(map[string]bool, len(symbols))
	for _, s := range symbols {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			set[s] = true
		}
	}
	return set
}
