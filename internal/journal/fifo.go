package journal

import (
	"math"
	"sort"
	"time"

	"github.com/trapp01/tape/internal/broker"
)

// lot is one parcel of shares still open. Qty is negative while short.
type lot struct {
	qty          int
	price        float64
	costPerShare float64
	openedAt     time.Time
	// orderID is the order whose fill opened the lot, so a closed trade can name
	// the entry it came from.
	orderID int64
}

// matchFIFO turns a fill history into completed round trips and whatever is left
// open, matching each closing fill against the oldest lot of that symbol. One
// closing fill produces at most one Trade, covering every lot it consumed.
//
// Commission and fees ride on the fill, so a partial exit takes only the share
// of the entry and exit costs that belongs to the quantity it closed.
func matchFIFO(fills []Fill) ([]Trade, []OpenPosition) {
	ordered := make([]Fill, len(fills))
	copy(ordered, fills)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].FilledAt.Equal(ordered[j].FilledAt) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].FilledAt.Before(ordered[j].FilledAt)
	})

	open := make(map[string][]lot)
	var trades []Trade

	for _, f := range ordered {
		if f.Qty <= 0 {
			continue
		}
		dir := 1
		if f.Side == string(broker.Sell) {
			dir = -1
		}
		costPerShare := (f.Commission + f.Fees) / float64(f.Qty)
		lots := open[f.Symbol]
		remaining := f.Qty

		var (
			closedQty     int
			entryNotional float64
			exitNotional  float64
			costs         float64
			openedAt      time.Time
			entryOrderID  int64
		)
		for remaining > 0 && len(lots) > 0 && sign(lots[0].qty) != dir {
			l := &lots[0]
			take := remaining
			if avail := abs(l.qty); take > avail {
				take = avail
			}
			if closedQty == 0 || l.openedAt.Before(openedAt) {
				openedAt = l.openedAt
				entryOrderID = l.orderID
			}
			closedQty += take
			entryNotional += l.price * float64(take)
			exitNotional += f.ModeledPrice * float64(take)
			costs += (l.costPerShare + costPerShare) * float64(take)

			l.qty += dir * take
			remaining -= take
			if l.qty == 0 {
				lots = lots[1:]
			}
		}

		if closedQty > 0 {
			// A sell closes longs and earns exit minus entry; a buy covering a short
			// earns the reverse.
			gross := (exitNotional - entryNotional) * float64(-dir)
			trades = append(trades, Trade{
				Symbol:        f.Symbol,
				Qty:           closedQty,
				EntryAvgPrice: entryNotional / float64(closedQty),
				ExitAvgPrice:  exitNotional / float64(closedQty),
				OpenedAt:      openedAt,
				ClosedAt:      f.FilledAt,
				GrossPL:       gross,
				Costs:         costs,
				NetPL:         gross - costs,
				EntryOrderID:  entryOrderID,
				ExitOrderID:   f.OrderID,
			})
		}
		if remaining > 0 {
			lots = append(lots, lot{
				qty:          dir * remaining,
				price:        f.ModeledPrice,
				costPerShare: costPerShare,
				openedAt:     f.FilledAt,
				orderID:      f.OrderID,
			})
		}
		if len(lots) == 0 {
			delete(open, f.Symbol)
		} else {
			open[f.Symbol] = lots
		}
	}

	return trades, openPositions(open)
}

func openPositions(open map[string][]lot) []OpenPosition {
	positions := make([]OpenPosition, 0, len(open))
	for symbol, lots := range open {
		var (
			qty      int
			notional float64
			openedAt time.Time
		)
		for _, l := range lots {
			qty += l.qty
			notional += math.Abs(float64(l.qty)) * l.price
			if openedAt.IsZero() || l.openedAt.Before(openedAt) {
				openedAt = l.openedAt
			}
		}
		if qty == 0 {
			continue
		}
		avg := notional / math.Abs(float64(qty))
		positions = append(positions, OpenPosition{
			Symbol:        symbol,
			Qty:           qty,
			AvgEntryPrice: avg,
			CostBasis:     float64(qty) * avg,
			OpenedAt:      openedAt,
		})
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].Symbol < positions[j].Symbol })
	return positions
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
