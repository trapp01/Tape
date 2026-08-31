package market

import (
	"testing"
	"time"
)

func TestSessionDateReadsTheVenueZone(t *testing.T) {
	mountain, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}

	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"a daily bar in daylight time", time.Date(2026, 8, 28, 4, 0, 0, 0, time.UTC), "2026-08-28"},
		{"a daily bar in standard time", time.Date(2026, 1, 5, 5, 0, 0, 0, time.UTC), "2026-01-05"},
		{"the same instant read from Mountain", time.Date(2026, 8, 28, 4, 0, 0, 0, time.UTC).In(mountain), "2026-08-28"},
		{"the close", time.Date(2026, 8, 28, 20, 0, 0, 0, time.UTC), "2026-08-28"},
		{"an evening in Mountain is still the same session", time.Date(2026, 8, 28, 20, 0, 0, 0, mountain), "2026-08-28"},
		{"after midnight Eastern is the next day", time.Date(2026, 8, 28, 23, 0, 0, 0, mountain), "2026-08-29"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SessionDate(tc.at); got != tc.want {
				t.Fatalf("SessionDate(%s) = %q, want %q", tc.at, got, tc.want)
			}
		})
	}
}

func TestEasternIsTheVenueZone(t *testing.T) {
	at := time.Date(2026, 8, 28, 13, 30, 0, 0, time.UTC).In(Eastern())
	if h, m := at.Hour(), at.Minute(); h != 9 || m != 30 {
		t.Fatalf("13:30 UTC in the venue zone = %02d:%02d, want the 09:30 bell", h, m)
	}
}
