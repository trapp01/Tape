package calendar

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeEconomic struct {
	name   string
	events []Event
	err    error
}

func (f fakeEconomic) Name() string { return f.name }

func (f fakeEconomic) Economic(ctx context.Context, from, to time.Time) ([]Event, error) {
	return f.events, f.err
}

type fakeEarnings struct {
	name    string
	events  []Event
	err     error
	gotSyms []string
}

func (f *fakeEarnings) Name() string { return f.name }

func (f *fakeEarnings) Earnings(ctx context.Context, symbols []string, from, to time.Time) ([]Event, error) {
	f.gotSyms = symbols
	return f.events, f.err
}

// anonymous has no Name, so Collect has to fall back to its type.
type anonymous struct{}

func (anonymous) Economic(ctx context.Context, from, to time.Time) ([]Event, error) {
	return nil, errors.New("boom")
}

func at(t *testing.T, day string, hour, minute int) time.Time {
	t.Helper()
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	d, err := time.ParseInLocation("2006-01-02", day, et)
	if err != nil {
		t.Fatalf("ParseInLocation: %v", err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, et).UTC()
}

func TestCollectWarnsInsteadOfFailing(t *testing.T) {
	cpi := Event{Kind: KindEconomic, Title: "Consumer Price Index (CPI)", At: at(t, "2026-09-11", 8, 30), Impact: ImpactHigh}
	fomc := Event{Kind: KindFOMC, Title: "FOMC rate decision", At: at(t, "2026-09-16", 14, 0), Impact: ImpactHigh}
	aapl := Event{Kind: KindEarnings, Title: "AAPL earnings", Symbol: "AAPL", At: at(t, "2026-09-11", 16, 30), Impact: ImpactHigh}

	earnings := &fakeEarnings{name: "Finnhub", events: []Event{aapl}}
	sources := Sources{
		Economic: []EconomicProvider{
			fakeEconomic{name: "FRED", err: NotConfigured("FRED_API_KEY not set")},
			fakeEconomic{name: "FOMC", events: []Event{fomc, cpi}},
		},
		Earnings: []EarningsProvider{earnings},
	}

	events, warnings := sources.Collect(context.Background(), []string{"AAPL"}, at(t, "2026-09-01", 0, 0), at(t, "2026-09-30", 0, 0))

	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one", warnings)
	}
	if warnings[0] != "FRED calendar unavailable: FRED_API_KEY not set" {
		t.Errorf("warning = %q", warnings[0])
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(events), events)
	}
	// Sorted by time: CPI 12:30 UTC, AAPL 20:30 UTC, FOMC on the 16th.
	wantTitles := []string{"Consumer Price Index (CPI)", "AAPL earnings", "FOMC rate decision"}
	for i, want := range wantTitles {
		if events[i].Title != want {
			t.Errorf("events[%d].Title = %q, want %q", i, events[i].Title, want)
		}
	}
	if len(earnings.gotSyms) != 1 || earnings.gotSyms[0] != "AAPL" {
		t.Errorf("earnings provider got symbols %v, want [AAPL]", earnings.gotSyms)
	}
}

func TestCollectDedupsAcrossSources(t *testing.T) {
	// Same release, two sources, different published times on the same day.
	morning := Event{Kind: KindEconomic, Title: "Consumer Price Index (CPI)", At: at(t, "2026-09-11", 8, 30), Source: "fred.stlouisfed.org"}
	noon := Event{Kind: KindEconomic, Title: "Consumer Price Index (CPI)", At: at(t, "2026-09-11", 12, 0), Source: "elsewhere", AllDay: true}
	other := Event{Kind: KindEconomic, Title: "Retail Sales (advance)", At: at(t, "2026-09-11", 8, 30)}

	sources := Sources{Economic: []EconomicProvider{
		fakeEconomic{name: "FRED", events: []Event{morning, other}},
		fakeEconomic{name: "Backup", events: []Event{noon}},
	}}

	events, warnings := sources.Collect(context.Background(), nil, at(t, "2026-09-01", 0, 0), at(t, "2026-09-30", 0, 0))
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 after dedup: %+v", len(events), events)
	}
	for _, e := range events {
		if e.Title == "Consumer Price Index (CPI)" && e.Source != "fred.stlouisfed.org" {
			t.Errorf("kept %q, want the first source to win", e.Source)
		}
	}
}

func TestCollectNeverFailsWhenEverySourceIsDown(t *testing.T) {
	sources := Sources{
		Economic: []EconomicProvider{anonymous{}},
		Earnings: []EarningsProvider{&fakeEarnings{name: "Finnhub", err: errors.New("timeout")}},
	}

	events, warnings := sources.Collect(context.Background(), []string{"AAPL"}, time.Now(), time.Now())
	if len(events) != 0 {
		t.Errorf("got %d events, want none", len(events))
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want two", warnings)
	}
	if warnings[0] != "calendar.anonymous calendar unavailable: boom" {
		t.Errorf("unnamed provider warning = %q", warnings[0])
	}
	if warnings[1] != "Finnhub calendar unavailable: timeout" {
		t.Errorf("named provider warning = %q", warnings[1])
	}
}

func TestNotConfiguredIsTheSentinel(t *testing.T) {
	err := NotConfigured("FRED_API_KEY not set")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatal("NotConfigured does not match ErrNotConfigured")
	}
	if err.Error() != "FRED_API_KEY not set" {
		t.Errorf("Error() = %q, want just the reason", err.Error())
	}
}

func TestCollectSkipsNilProviders(t *testing.T) {
	sources := Sources{
		Economic: []EconomicProvider{nil},
		Earnings: []EarningsProvider{nil},
	}
	events, warnings := sources.Collect(context.Background(), nil, time.Now(), time.Now())
	if len(events) != 0 || len(warnings) != 0 {
		t.Errorf("got %d events and %v warnings, want none", len(events), warnings)
	}
}
