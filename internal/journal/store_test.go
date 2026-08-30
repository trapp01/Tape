package journal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const startingEquity = 5000.0

func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tape.db")
	s, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// insertFill records an order and its single fill, returning the fill.
func insertFill(t *testing.T, s *Store, mode, symbol, side string, qty int, price float64, at time.Time, commission, fees float64) Fill {
	t.Helper()
	ctx := context.Background()
	o := &Order{
		Symbol:      symbol,
		Side:        side,
		Qty:         qty,
		Type:        "market",
		Status:      "filled",
		Source:      SourceHuman,
		Mode:        mode,
		SubmittedAt: at,
	}
	if err := s.InsertOrder(ctx, o); err != nil {
		t.Fatalf("InsertOrder: %v", err)
	}
	f := &Fill{
		OrderID:      o.ID,
		Symbol:       symbol,
		Side:         side,
		Qty:          qty,
		RawPrice:     price,
		ModeledPrice: price,
		Commission:   commission,
		Fees:         fees,
		FilledAt:     at,
	}
	if err := s.InsertFill(ctx, f); err != nil {
		t.Fatalf("InsertFill: %v", err)
	}
	return *f
}

func TestOpenTwiceSameEquity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")

	first, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if second.StartingEquity() != startingEquity {
		t.Errorf("StartingEquity() = %v, want %v", second.StartingEquity(), startingEquity)
	}
}

func TestOpenTwiceDifferentEquityErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")

	first, err := Open(path, startingEquity)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	first.Close()

	second, err := Open(path, 10_000)
	if err == nil {
		second.Close()
		t.Fatal("Open with a different starting equity succeeded, want an error")
	}
	msg := err.Error()
	for _, want := range []string{"5000", "10000", path} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestOpenRejectsNonPositiveEquity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")
	if _, err := Open(path, 0); err == nil {
		t.Fatal("Open with zero starting equity succeeded, want an error")
	}
}

func TestOpenIsIdempotentAcrossMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tape.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path, startingEquity)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		var version int
		if err := s.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
			t.Fatalf("reading schema version: %v", err)
		}
		if version != len(migrations) {
			t.Fatalf("schema version = %d, want %d", version, len(migrations))
		}
		s.Close()
	}
}
