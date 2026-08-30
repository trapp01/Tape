package alpaca

import (
	"testing"

	sdk "github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"

	"github.com/trapp01/tape/internal/broker"
)

// Statuses normalise for the contract while RawStatus keeps Alpaca's own word, so
// vocabulary the contract has no constant for still reaches the journal.
func TestStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want broker.OrderStatus
	}{
		{"new", broker.StatusNew},
		{"pending_new", broker.StatusNew},
		{"accepted_for_bidding", broker.StatusNew},
		{"accepted", broker.StatusAccepted},
		{"partially_filled", broker.StatusPartiallyFilled},
		{"filled", broker.StatusFilled},
		{"canceled", broker.StatusCanceled},
		{"replaced", broker.StatusCanceled},
		{"rejected", broker.StatusRejected},
		{"expired", broker.StatusExpired},
		{"done_for_day", broker.StatusExpired},
		{"held", broker.StatusAccepted},
		{"pending_cancel", broker.StatusAccepted},
		{"stopped", broker.StatusAccepted},
		{"suspended", broker.StatusAccepted},
		{"", broker.StatusAccepted},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := mapStatus(tc.raw); got != tc.want {
				t.Fatalf("mapStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			got := toOrder(sdk.Order{Status: tc.raw})
			if got.Status != tc.want {
				t.Fatalf("toOrder(%q).Status = %q, want %q", tc.raw, got.Status, tc.want)
			}
			if got.RawStatus != tc.raw {
				t.Fatalf("toOrder(%q).RawStatus = %q, want it round-tripped", tc.raw, got.RawStatus)
			}
		})
	}
}

// F9: a day order done for the day, and one the venue replaced, can take no
// further fills. Mapping them as live left sync polling a dead order forever.
func TestTerminalStatusesForDoneForDayAndReplaced(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"done_for_day", true},
		{"replaced", true},
		{"stopped", false},
		{"held", false},
		{"pending_cancel", false},
	} {
		if got := mapStatus(tc.raw).Terminal(); got != tc.want {
			t.Errorf("mapStatus(%q).Terminal() = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestRawStatusOnLegs(t *testing.T) {
	order := toOrder(sdk.Order{
		Status: "done_for_day",
		Legs:   []sdk.Order{{Status: "held"}, {Status: "canceled"}},
	})

	if order.Status != broker.StatusExpired || order.RawStatus != "done_for_day" {
		t.Fatalf("parent = %q / %q, want expired / done_for_day", order.Status, order.RawStatus)
	}
	if len(order.Legs) != 2 {
		t.Fatalf("legs = %+v, want 2", order.Legs)
	}
	if order.Legs[0].Status != broker.StatusAccepted || order.Legs[0].RawStatus != "held" {
		t.Fatalf("leg 0 = %q / %q, want accepted / held", order.Legs[0].Status, order.Legs[0].RawStatus)
	}
	if order.Legs[1].Status != broker.StatusCanceled || order.Legs[1].RawStatus != "canceled" {
		t.Fatalf("leg 1 = %q / %q, want canceled / canceled", order.Legs[1].Status, order.Legs[1].RawStatus)
	}
}
