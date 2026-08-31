package calendar

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"
)

// Sources is the set of calendars a briefing pulls from. Any of them may be
// unkeyed or down, so Collect degrades instead of failing.
type Sources struct {
	Economic []EconomicProvider
	Earnings []EarningsProvider
}

// named lets a provider label its own warnings. Anything else is labelled by type.
type named interface{ Name() string }

// Collect queries every source, sorts what came back by time, and turns each
// failure into a warning. It never returns an error; no events plus warnings is
// a valid answer.
func (s Sources) Collect(ctx context.Context, symbols []string, from, to time.Time) ([]Event, []string) {
	var events []Event
	var warnings []string

	for _, p := range s.Economic {
		if p == nil {
			continue
		}
		evs, err := p.Economic(ctx, from, to)
		if err != nil {
			warnings = append(warnings, Warning(sourceName(p), err))
			continue
		}
		events = append(events, evs...)
	}
	for _, p := range s.Earnings {
		if p == nil {
			continue
		}
		evs, err := p.Earnings(ctx, symbols, from, to)
		if err != nil {
			warnings = append(warnings, Warning(sourceName(p), err))
			continue
		}
		events = append(events, evs...)
	}

	events = dedup(events)
	slices.SortStableFunc(events, byTime)
	return events, warnings
}

// Warning renders a source failure the way Collect does, for callers that could
// not even construct the provider.
func Warning(name string, err error) string {
	return fmt.Sprintf("%s calendar unavailable: %v", name, err)
}

// NotConfigured reports a missing key as ErrNotConfigured while keeping the
// message short enough to read as a warning line.
func NotConfigured(reason string) error {
	return notConfigured{reason: reason}
}

type notConfigured struct{ reason string }

func (e notConfigured) Error() string { return e.reason }
func (e notConfigured) Unwrap() error { return ErrNotConfigured }

func sourceName(p any) string {
	if n, ok := p.(named); ok {
		return n.Name()
	}
	return fmt.Sprintf("%T", p)
}

// dedup keeps the first of each repeated event, since two sources can carry the
// same release. Every provider dates events so the UTC and Eastern days agree.
func dedup(events []Event) []Event {
	type key struct {
		kind                Kind
		title, symbol, date string
	}
	seen := make(map[key]bool, len(events))
	out := make([]Event, 0, len(events))
	for _, e := range events {
		k := key{e.Kind, e.Title, e.Symbol, e.At.UTC().Format("2006-01-02")}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

func byTime(a, b Event) int {
	if c := a.At.Compare(b.At); c != 0 {
		return c
	}
	if c := cmp.Compare(string(a.Kind), string(b.Kind)); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Symbol, b.Symbol); c != 0 {
		return c
	}
	return cmp.Compare(a.Title, b.Title)
}
