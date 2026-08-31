package cli

import (
	"strings"
	"testing"
	"time"
)

// The cutoff is the venue's clock, not the reader's: 16:30 Eastern is 14:30 in
// Mountain time, and the free feed's 15-minute delay is why it is not 16:00.
func TestDefaultThroughDayGatesOnTheVenueClock(t *testing.T) {
	mountain, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}

	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"before the bell", time.Date(2026, 8, 28, 12, 52, 0, 0, time.UTC), "2026-08-27"},
		{"one minute before the cutoff", time.Date(2026, 8, 28, 20, 29, 0, 0, time.UTC), "2026-08-27"},
		{"on the cutoff", time.Date(2026, 8, 28, 20, 30, 0, 0, time.UTC), "2026-08-28"},
		{"the evening", time.Date(2026, 8, 28, 23, 0, 0, 0, time.UTC), "2026-08-28"},
		{"14:29 Mountain is still too early", time.Date(2026, 8, 28, 14, 29, 0, 0, mountain), "2026-08-27"},
		{"14:30 Mountain is the cutoff", time.Date(2026, 8, 28, 14, 30, 0, 0, mountain), "2026-08-28"},
		// 23:00 Mountain is the small hours of the 29th in New York.
		{"after midnight Eastern", time.Date(2026, 8, 28, 23, 0, 0, 0, mountain), "2026-08-28"},
		{"standard time", time.Date(2026, 1, 5, 21, 30, 0, 0, time.UTC), "2026-01-05"},
		{"standard time, an hour earlier", time.Date(2026, 1, 5, 20, 30, 0, 0, time.UTC), "2026-01-04"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultThroughDay(tc.now); got != tc.want {
				t.Fatalf("defaultThroughDay(%s) = %q, want %q", tc.now, got, tc.want)
			}
		})
	}
}

// Grading before the cutoff would read a half-built session, and a call is
// graded once.
func TestScoreLeavesTodayAloneBeforeTheCutoff(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	out, err := run(t, "score")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !strings.Contains(out, "Calls through 2026-08-27") {
		t.Fatalf("score before the cutoff must stop at the last finished session:\n%s", out)
	}
	if !strings.Contains(out, "nothing left to grade") {
		t.Fatalf("today's call must stay open:\n%s", out)
	}
}

// A session the feed has not finished serving leaves the call open with a reason.
func TestScoreSkipsASessionThatIsNotFinal(t *testing.T) {
	_, feed, _ := newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}
	feed.sessionDone = false
	atClock(t, briefEvening)

	out, err := run(t, "score")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if !strings.Contains(out, "not graded: SPY on "+briefSession+": session not final yet") {
		t.Fatalf("score must name the unfinished session:\n%s", out)
	}
}

// --through picks the last session to grade; it does not move the cutoff. Naming
// today before 16:30 ET would grade a half-built session, permanently.
func TestScoreThroughCannotReachIntoARunningSession(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	for _, day := range []string{briefSession, "2026-08-29"} {
		_, err := run(t, "score", "--through", day)
		if err == nil {
			t.Fatalf("--through %s before the cutoff must be refused", day)
		}
		if !strings.Contains(err.Error(), "16:30 ET") {
			t.Fatalf("refusal for --through %s = %v, want it to name the cutoff", day, err)
		}
	}

	// Yesterday is finished whatever time it is now.
	if _, err := run(t, "score", "--through", "2026-08-27"); err != nil {
		t.Fatalf("--through a closed session: %v", err)
	}

	// After the cutoff today is fair game.
	atClock(t, briefEvening)
	out, err := run(t, "score", "--through", briefSession)
	if err != nil {
		t.Fatalf("--through after the cutoff: %v", err)
	}
	if !strings.Contains(out, "actual +0.50%") {
		t.Fatalf("the call should grade after the cutoff:\n%s", out)
	}
}

func TestEODSaysWhenGradesArrive(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	out, err := run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	if !strings.Contains(out, gradeLater) {
		t.Fatalf("eod before the cutoff must say when grades arrive:\n%s", out)
	}
	if strings.Contains(out, "actual ") {
		t.Fatalf("eod must not grade before the cutoff:\n%s", out)
	}
}

func TestScoreGradesTheCallAfterTheClose(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}

	// The bell has rung and the feed's delay is behind us.
	atClock(t, briefEvening)

	out, err := run(t, "score")
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	for _, want := range []string{"[paper] score", "SPY", "up ≥0.3%", "actual +0.50%", "✓", "last 30 days: 1/1 (100%)", "3+ months"} {
		if !strings.Contains(out, want) {
			t.Fatalf("score output missing %q:\n%s", want, out)
		}
	}

	// A call is graded once, so a second pass has nothing to do.
	again, err := run(t, "score")
	if err != nil {
		t.Fatalf("second score: %v", err)
	}
	if !strings.Contains(again, "nothing left to grade") {
		t.Fatalf("second score:\n%s", again)
	}
}

func TestEODPrintsTheCallOutcome(t *testing.T) {
	newBriefHome(t)
	if _, err := run(t, "brief"); err != nil {
		t.Fatalf("brief: %v", err)
	}
	atClock(t, briefEvening)

	out, err := run(t, "eod")
	if err != nil {
		t.Fatalf("eod: %v", err)
	}
	if !strings.Contains(out, "Calls through") || !strings.Contains(out, "actual +0.50%") {
		t.Fatalf("eod must grade the morning's call:\n%s", out)
	}
}
