package fake

import (
	"context"
	"fmt"
	"time"
)

// hours is one session's bells, as the venue calendar reports them.
type hours struct{ open, close time.Time }

// SetSessionHours puts one day on the venue calendar, which is how a half day is
// told apart from a session that stopped short.
func (b *Broker) SetSessionHours(day string, open, close time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[day] = hours{open: open, close: close}
}

// SessionHours answers from the calendar SetSessionHours built. A day nobody set
// is a day the venue does not trade.
func (b *Broker) SessionHours(_ context.Context, day string) (time.Time, time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.sessions[day]
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("fake: session hours on %s: the venue does not trade that day", day)
	}
	return h.open, h.close, nil
}
