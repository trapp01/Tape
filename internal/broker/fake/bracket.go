package fake

import (
	"fmt"

	"github.com/trapp01/tape/internal/broker"
)

// attachBracket registers the stop and target children of an entry order so each
// leg can be fetched and filled by its own id, the way a venue exposes them.
// Callers hold the lock.
func (b *Broker) attachBracket(parent *broker.Order, req broker.OrderRequest) {
	exit := broker.Sell
	if req.Side == broker.Sell {
		exit = broker.Buy
	}
	if req.TakeProfit != nil {
		b.addLeg(parent, exit, broker.Limit, req.TakeProfit)
	}
	if req.StopLoss != nil {
		b.addLeg(parent, exit, broker.OrderType("stop"), req.StopLoss)
	}
}

// addLeg creates one protective child. Callers hold the lock.
func (b *Broker) addLeg(parent *broker.Order, side broker.Side, kind broker.OrderType, price *float64) {
	b.seq++
	leg := &broker.Order{
		ID:          fmt.Sprintf("fake-%d", b.seq),
		Symbol:      parent.Symbol,
		Side:        side,
		Qty:         parent.Qty,
		Type:        kind,
		Status:      broker.StatusAccepted,
		RawStatus:   "held",
		SubmittedAt: parent.SubmittedAt,
	}
	if kind == broker.Limit {
		leg.LimitPrice = price
	}
	b.orders[leg.ID] = leg
	b.legs[parent.ID] = append(b.legs[parent.ID], leg.ID)
	b.parentOf[leg.ID] = parent.ID
}

// snapshot copies o with its bracket children as they stand now, so a caller
// polling the parent sees a leg that has since filled. Callers hold the lock.
func (b *Broker) snapshot(o *broker.Order) broker.Order {
	out := *o
	out.Legs = nil
	for _, id := range b.legs[o.ID] {
		if leg, ok := b.orders[id]; ok {
			out.Legs = append(out.Legs, *leg)
		}
	}
	return out
}

// cancelSiblingLegs closes the other half of a bracket once one leg trades, the
// way a venue's one-cancels-other pair behaves. Callers hold the lock.
func (b *Broker) cancelSiblingLegs(filled *broker.Order) {
	parent, ok := b.parentOf[filled.ID]
	if !ok {
		return
	}
	for _, id := range b.legs[parent] {
		sibling, ok := b.orders[id]
		if !ok || sibling.ID == filled.ID || sibling.Status.Terminal() {
			continue
		}
		sibling.Status, sibling.RawStatus = broker.StatusCanceled, "canceled"
	}
}
