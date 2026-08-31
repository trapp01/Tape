package market

import "time"

// DayLayout is the fixed-width layout every session date in tape is written in.
const DayLayout = "2006-01-02"

// eastern is the venue's zone. US equity sessions are stamped in it, so a
// session date is never the reader's calendar day.
var eastern = loadEastern()

func loadEastern() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Without zoneinfo the offset is standard time year round, which misdates
		// a midnight-Eastern bar stamped 04:00Z between March and November.
		return time.FixedZone("EST", -5*60*60)
	}
	return loc
}

// Eastern is the zone the venue stamps sessions in.
func Eastern() *time.Location { return eastern }

// SessionDate is the trading day t belongs to. Alpaca dates a daily bar at
// midnight Eastern, which reads as the previous day in every zone west of it.
func SessionDate(t time.Time) string {
	return t.In(eastern).Format(DayLayout)
}
