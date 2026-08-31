package alpacadata

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// snapshotsBody covers the three shapes the venue sends: a full snapshot, one
// with no daily bars, and a symbol it does not carry.
const snapshotsBody = `{
	"AAPL": {
		"latestTrade": {"t":"2026-08-28T19:59:59Z","p":231.45,"s":100},
		"latestQuote": {"t":"2026-08-28T20:00:00Z","bp":231.40,"ap":231.50},
		"dailyBar": {"t":"2026-08-28T04:00:00Z","o":228.00,"h":232.10,"l":227.50,"c":231.45,"v":52000000},
		"prevDailyBar": {"t":"2026-08-27T04:00:00Z","o":225.00,"h":229.00,"l":224.50,"c":228.30,"v":48000000}
	},
	"NEWCO": {
		"latestTrade": {"t":"2026-08-28T19:45:00Z","p":10.50,"s":10},
		"latestQuote": {"t":"2026-08-28T19:45:01Z","bp":10.40,"ap":10.60}
	},
	"GONE": null
}`

func TestSnapshotsMapsEveryField(t *testing.T) {
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/stocks/snapshots" {
			t.Errorf("path = %q", r.URL.Path)
		}
		query = r.URL.Query()
		writeJSON(t, w, http.StatusOK, snapshotsBody)
	})

	snaps, err := c.Snapshots(context.Background(), []string{"AAPL", "NEWCO", "GONE"})
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if query.Get("feed") != "iex" {
		t.Errorf("feed = %q, want iex", query.Get("feed"))
	}
	if query.Get("symbols") != "AAPL,NEWCO,GONE" {
		t.Errorf("symbols = %q", query.Get("symbols"))
	}

	aapl := snaps["AAPL"]
	if aapl.Symbol != "AAPL" || aapl.Last != 231.45 || aapl.Bid != 231.40 || aapl.Ask != 231.50 {
		t.Fatalf("AAPL = %+v", aapl)
	}
	if !aapl.LastAt.Equal(time.Date(2026, 8, 28, 19, 59, 59, 0, time.UTC)) {
		t.Errorf("AAPL LastAt = %v", aapl.LastAt)
	}
	if aapl.PrevClose != 228.30 {
		t.Errorf("AAPL PrevClose = %v, want the previous daily close", aapl.PrevClose)
	}
	if aapl.TodayOpen != 228.00 {
		t.Errorf("AAPL TodayOpen = %v, want the current daily open", aapl.TodayOpen)
	}
	if aapl.Volume != 52000000 {
		t.Errorf("AAPL Volume = %v", aapl.Volume)
	}
}

func TestSnapshotsWithoutDailyBarsStayZero(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, snapshotsBody)
	})

	snaps, err := c.Snapshots(context.Background(), []string{"AAPL", "NEWCO", "GONE"})
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}

	newco := snaps["NEWCO"]
	if newco.Last != 10.50 || newco.Bid != 10.40 || newco.Ask != 10.60 {
		t.Fatalf("NEWCO = %+v", newco)
	}
	if newco.PrevClose != 0 || newco.TodayOpen != 0 || newco.Volume != 0 {
		t.Errorf("NEWCO carries bar data it was not sent: %+v", newco)
	}
	if newco.ChangePct() != 0 {
		t.Errorf("NEWCO ChangePct = %v, want 0 without a previous close", newco.ChangePct())
	}
}

func TestSnapshotsOmitUnknownSymbols(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, snapshotsBody)
	})

	snaps, err := c.Snapshots(context.Background(), []string{"AAPL", "NEWCO", "GONE"})
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if _, ok := snaps["GONE"]; ok {
		t.Errorf("GONE should be absent, got %+v", snaps["GONE"])
	}
	if len(snaps) != 2 {
		t.Fatalf("snapshots = %+v, want 2", snaps)
	}
}

func TestSnapshotsEmptySymbolsSkipsRequest(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be made for an empty symbol list")
	})

	snaps, err := c.Snapshots(context.Background(), nil)
	if err != nil {
		t.Fatalf("Snapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("snapshots = %+v, want empty", snaps)
	}
}

func TestSnapshotsErrorNamesTheSymbols(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, `{"message":"subscription does not permit"}`)
	})

	_, err := c.Snapshots(context.Background(), []string{"SPY", "QQQ"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "alpacadata: snapshots SPY,QQQ") {
		t.Fatalf("error = %v", err)
	}
}
