package journal

import (
	"context"
	"testing"
	"time"
)

func TestClosedTradesRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 10, 110, day.Add(time.Hour), 1.00, 0.16)

	trades, err := s.ClosedTrades(ctx, time.Time{}, time.Time{}, ModePaper)
	if err != nil {
		t.Fatalf("ClosedTrades: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("got %d trades, want 1", len(trades))
	}
	closeTo(t, "GrossPL", trades[0].GrossPL, 100)
	closeTo(t, "Costs", trades[0].Costs, 2.16)
	closeTo(t, "NetPL", trades[0].NetPL, 100-2.16)

	positions, err := s.OpenPositions(ctx, ModePaper)
	if err != nil {
		t.Fatalf("OpenPositions: %v", err)
	}
	if len(positions) != 0 {
		t.Errorf("got %d open positions, want 0", len(positions))
	}
}

func TestClosedTradesTwoPartialExits(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 4, 110, day.Add(time.Hour), 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 6, 120, day.Add(2*time.Hour), 1.00, 0)

	trades, err := s.ClosedTrades(ctx, time.Time{}, time.Time{}, ModePaper)
	if err != nil {
		t.Fatalf("ClosedTrades: %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("got %d trades, want 2", len(trades))
	}
	if trades[0].Qty != 4 || trades[1].Qty != 6 {
		t.Errorf("quantities = %d, %d, want 4, 6", trades[0].Qty, trades[1].Qty)
	}

	positions, err := s.OpenPositions(ctx, ModePaper)
	if err != nil {
		t.Fatalf("OpenPositions: %v", err)
	}
	if len(positions) != 0 {
		t.Errorf("got %d open positions, want 0", len(positions))
	}
}

func TestOpenPositionAfterPartialExit(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 4, 110, day.Add(time.Hour), 1.00, 0)

	positions, err := s.OpenPositions(ctx, ModePaper)
	if err != nil {
		t.Fatalf("OpenPositions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("got %d open positions, want 1", len(positions))
	}
	pos := positions[0]
	if pos.Symbol != "AAPL" || pos.Qty != 6 {
		t.Fatalf("position = %+v, want AAPL qty 6", pos)
	}
	closeTo(t, "AvgEntryPrice", pos.AvgEntryPrice, 100)
	closeTo(t, "CostBasis", pos.CostBasis, 600)
}

func TestLedgerCashMath(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 4, 110, day.Add(time.Hour), 1.00, 0.02)

	led, err := s.Ledger(ctx, ModePaper)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	closeTo(t, "StartingEquity", led.StartingEquity, startingEquity)
	// 5000 - 1000 (buy) + 440 (sell) - 2.00 commission - 0.02 fees.
	closeTo(t, "Cash", led.Cash, 5000-1000+440-2.00-0.02)
	closeTo(t, "Commissions", led.Commissions, 2.00)
	closeTo(t, "Fees", led.Fees, 0.02)
	// 40 gross, less 4/10 of the entry commission and all of the exit costs.
	closeTo(t, "RealizedPL", led.RealizedPL, 40-(0.4+1.02))
	if len(led.OpenPositions) != 1 || led.OpenPositions[0].Qty != 6 {
		t.Errorf("open positions = %+v, want one of 6", led.OpenPositions)
	}
}

func TestLedgerEmptyJournal(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	led, err := s.Ledger(ctx, ModePaper)
	if err != nil {
		t.Fatalf("Ledger: %v", err)
	}
	closeTo(t, "Cash", led.Cash, startingEquity)
	closeTo(t, "RealizedPL", led.RealizedPL, 0)
	if len(led.OpenPositions) != 0 {
		t.Errorf("open positions = %+v, want none", led.OpenPositions)
	}
}

func TestModeSeparation(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 10, 110, day.Add(time.Hour), 1.00, 0)
	insertFill(t, s, ModeLive, "MSFT", "buy", 5, 400, day, 1.00, 0)

	live, err := s.Ledger(ctx, ModeLive)
	if err != nil {
		t.Fatalf("Ledger live: %v", err)
	}
	closeTo(t, "live RealizedPL", live.RealizedPL, 0)
	closeTo(t, "live Cash", live.Cash, 5000-2000-1.00)
	if len(live.OpenPositions) != 1 || live.OpenPositions[0].Symbol != "MSFT" {
		t.Fatalf("live positions = %+v, want only MSFT", live.OpenPositions)
	}

	paper, err := s.Ledger(ctx, ModePaper)
	if err != nil {
		t.Fatalf("Ledger paper: %v", err)
	}
	closeTo(t, "paper RealizedPL", paper.RealizedPL, 100-2.00)
	if len(paper.OpenPositions) != 0 {
		t.Errorf("paper positions = %+v, want none", paper.OpenPositions)
	}

	paperTrades, err := s.ClosedTrades(ctx, time.Time{}, time.Time{}, ModePaper)
	if err != nil {
		t.Fatalf("ClosedTrades paper: %v", err)
	}
	if len(paperTrades) != 1 || paperTrades[0].Symbol != "AAPL" {
		t.Errorf("paper trades = %+v, want one AAPL trade", paperTrades)
	}
	liveTrades, err := s.ClosedTrades(ctx, time.Time{}, time.Time{}, ModeLive)
	if err != nil {
		t.Fatalf("ClosedTrades live: %v", err)
	}
	if len(liveTrades) != 0 {
		t.Errorf("live trades = %+v, want none", liveTrades)
	}
}

func TestDayRecapRespectsLocation(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// 2026-08-24 15:30 UTC is 09:30 in Edmonton on the same day; 05:30 UTC is
	// 23:30 the evening before.
	sameDay := time.Date(2026, 8, 24, 15, 30, 0, 0, time.UTC)
	previousLocalDay := time.Date(2026, 8, 24, 5, 30, 0, 0, time.UTC)

	for _, tc := range []struct {
		name    string
		exitAt  time.Time
		wantDay string
	}{
		{"15:30 UTC lands on the same local day", sameDay, "2026-08-24"},
		{"05:30 UTC lands on the previous local day", previousLocalDay, "2026-08-23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			entry := tc.exitAt.Add(-2 * time.Hour)
			insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, entry, 1.00, 0)
			insertFill(t, s, ModePaper, "AAPL", "sell", 10, 110, tc.exitAt, 1.00, 0)

			onExitDay, err := s.DayRecap(ctx, tc.exitAt, loc, ModePaper)
			if err != nil {
				t.Fatalf("DayRecap: %v", err)
			}
			if got := onExitDay.Day.Format("2006-01-02"); got != tc.wantDay {
				t.Fatalf("recap day = %s, want %s", got, tc.wantDay)
			}
			if len(onExitDay.Trades) != 1 {
				t.Fatalf("got %d trades on %s, want 1", len(onExitDay.Trades), tc.wantDay)
			}
			closeTo(t, "NetPL", onExitDay.NetPL, 100-2.00)
			closeTo(t, "GrossPL", onExitDay.GrossPL, 100)
			closeTo(t, "Costs", onExitDay.Costs, 2.00)
			if onExitDay.Wins != 1 || onExitDay.Losses != 0 {
				t.Errorf("wins/losses = %d/%d, want 1/0", onExitDay.Wins, onExitDay.Losses)
			}

			// The UTC calendar day of the exit holds no trades when the local day differs.
			if tc.wantDay != "2026-08-24" {
				utcDay, err := s.DayRecap(ctx, tc.exitAt, time.UTC, ModePaper)
				if err != nil {
					t.Fatalf("DayRecap UTC: %v", err)
				}
				if len(utcDay.Trades) != 1 {
					t.Errorf("UTC recap held %d trades, want 1", len(utcDay.Trades))
				}
				nextLocal, err := s.DayRecap(ctx, tc.exitAt.Add(24*time.Hour), loc, ModePaper)
				if err != nil {
					t.Fatalf("DayRecap next day: %v", err)
				}
				if len(nextLocal.Trades) != 0 {
					t.Errorf("2026-08-24 in Edmonton held %d trades, want 0", len(nextLocal.Trades))
				}
			}
		})
	}
}

// F12: the recap summed costs over closed trades only, so a day of three entries
// held overnight reported costs of $0.00 against $3.00 of real commission.
func TestDayRecapFillCostsCoverPositionsStillOpen(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "MSFT", "buy", 5, 400, day.Add(time.Hour), 1.00, 0)
	insertFill(t, s, ModePaper, "NVDA", "buy", 3, 120, day.Add(2*time.Hour), 1.00, 0)

	rec, err := s.DayRecap(ctx, day, time.UTC, ModePaper)
	if err != nil {
		t.Fatalf("DayRecap: %v", err)
	}
	if len(rec.Trades) != 0 {
		t.Fatalf("got %d closed trades, want 0", len(rec.Trades))
	}
	closeTo(t, "Costs", rec.Costs, 0)
	closeTo(t, "FillCosts", rec.FillCosts, 3.00)

	// Closing one of them adds the exit's own costs to the day's fills.
	insertFill(t, s, ModePaper, "AAPL", "sell", 10, 110, day.Add(3*time.Hour), 1.00, 0.16)
	rec, err = s.DayRecap(ctx, day, time.UTC, ModePaper)
	if err != nil {
		t.Fatalf("DayRecap after the exit: %v", err)
	}
	closeTo(t, "Costs", rec.Costs, 2.16)
	closeTo(t, "FillCosts", rec.FillCosts, 4.16)
}

func TestDayRecapCountsOrdersAndLosses(t *testing.T) {
	ctx := context.Background()
	loc, err := time.LoadLocation("America/Edmonton")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	s := newStore(t)

	day := time.Date(2026, 8, 24, 15, 30, 0, 0, time.UTC)
	insertFill(t, s, ModePaper, "AAPL", "buy", 10, 100, day, 1.00, 0)
	insertFill(t, s, ModePaper, "AAPL", "sell", 10, 95, day.Add(time.Hour), 1.00, 0)
	// A live order on the same day must not reach the paper recap.
	insertFill(t, s, ModeLive, "MSFT", "buy", 1, 400, day, 1.00, 0)

	rec, err := s.DayRecap(ctx, day, loc, ModePaper)
	if err != nil {
		t.Fatalf("DayRecap: %v", err)
	}
	if rec.OrdersCount != 2 {
		t.Errorf("OrdersCount = %d, want 2", rec.OrdersCount)
	}
	closeTo(t, "GrossPL", rec.GrossPL, -50)
	closeTo(t, "NetPL", rec.NetPL, -52)
	if rec.Wins != 0 || rec.Losses != 1 {
		t.Errorf("wins/losses = %d/%d, want 0/1", rec.Wins, rec.Losses)
	}
}
