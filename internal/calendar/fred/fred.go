// Package fred reads scheduled US economic releases from the St. Louis Fed's
// FRED API and keeps the handful that move the open.
package fred

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/trapp01/tape/internal/calendar"
)

const (
	endpoint       = "https://api.stlouisfed.org/fred/releases/dates"
	requestTimeout = 10 * time.Second
	attempts       = 2
	baseDelay      = 500 * time.Millisecond
	maxBodyBytes   = 4 << 20
	excerptLimit   = 512
	dateLayout     = "2006-01-02"
	source         = "fred.stlouisfed.org"
)

// release is one FRED release worth putting in front of a trader.
type release struct {
	title  string
	impact calendar.Impact
	// hour and minute are the usual publication time in Eastern. Hour 0 means the
	// source publishes no fixed time, which makes the event all-day.
	hour   int
	minute int
}

// releases is keyed by FRED release_id, each verified at
// https://fred.stlouisfed.org/release?rid=<id>. FRED carries no ISM series.
var releases = map[int]release{
	9:   {"Retail Sales (advance)", calendar.ImpactHigh, 8, 30},
	10:  {"Consumer Price Index (CPI)", calendar.ImpactHigh, 8, 30},
	46:  {"Producer Price Index (PPI)", calendar.ImpactMedium, 8, 30},
	50:  {"Employment Situation (nonfarm payrolls)", calendar.ImpactHigh, 8, 30},
	53:  {"Gross Domestic Product (GDP)", calendar.ImpactHigh, 8, 30},
	54:  {"Personal Income and Outlays (PCE)", calendar.ImpactHigh, 8, 30},
	91:  {"Michigan Consumer Sentiment", calendar.ImpactMedium, 10, 0},
	192: {"Job Openings and Labor Turnover (JOLTS)", calendar.ImpactMedium, 10, 0},
}

var _ calendar.EconomicProvider = (*Provider)(nil)

// Provider queries FRED once per call and caches nothing.
type Provider struct {
	apiKey  string
	baseURL string
	http    *http.Client
	delay   time.Duration
}

// New builds a provider. An empty key is ErrNotConfigured, not a failure saved
// for the middle of a briefing.
func New(apiKey string) (*Provider, error) {
	if apiKey == "" {
		return nil, calendar.NotConfigured("FRED_API_KEY not set (free key: https://fredaccount.stlouisfed.org/apikeys)")
	}
	return &Provider{
		apiKey:  apiKey,
		baseURL: endpoint,
		http:    &http.Client{Timeout: requestTimeout},
		delay:   baseDelay,
	}, nil
}

func (p *Provider) Name() string { return "FRED" }

// datesResponse is the documented shape of fred/releases/dates.
type datesResponse struct {
	ReleaseDates []struct {
		ReleaseID   int    `json:"release_id"`
		ReleaseName string `json:"release_name"`
		Date        string `json:"date"`
	} `json:"release_dates"`
}

// Economic returns the curated releases dated inside [from, to]. The range is
// read as Eastern calendar days, matching how FRED publishes dates.
func (p *Provider) Economic(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	et, err := eastern()
	if err != nil {
		return nil, err
	}
	first, last := from.In(et).Format(dateLayout), to.In(et).Format(dateLayout)

	// sort_order is ascending so a truncated page keeps the soonest releases.
	q := url.Values{
		"api_key":                            {p.apiKey},
		"file_type":                          {"json"},
		"realtime_start":                     {first},
		"realtime_end":                       {last},
		"include_release_dates_with_no_data": {"true"},
		"sort_order":                         {"asc"},
	}

	var payload datesResponse
	if err := p.get(ctx, q, &payload); err != nil {
		return nil, err
	}

	out := make([]calendar.Event, 0, len(payload.ReleaseDates))
	for _, d := range payload.ReleaseDates {
		r, ok := releases[d.ReleaseID]
		if !ok || d.Date < first || d.Date > last {
			continue
		}
		day, err := time.ParseInLocation(dateLayout, d.Date, et)
		if err != nil {
			return nil, fmt.Errorf("fred: release %d (%s): unreadable date %q", d.ReleaseID, d.ReleaseName, d.Date)
		}
		out = append(out, newEvent(r, day, et))
	}
	return out, nil
}

func newEvent(r release, day time.Time, et *time.Location) calendar.Event {
	ev := calendar.Event{
		Kind:   calendar.KindEconomic,
		Title:  r.title,
		Impact: r.impact,
		Source: source,
	}
	if r.hour == 0 {
		// Noon Eastern keeps the calendar day the same once the time is UTC.
		ev.At = time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, et).UTC()
		ev.AllDay = true
		return ev
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), r.hour, r.minute, 0, 0, et)
	ev.At = at.UTC()
	ev.Detail = at.Format("3:04 PM") + " ET"
	return ev
}
