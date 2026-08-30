package costs

import (
	"math"
	"testing"

	"github.com/trapp01/tape/internal/broker"
)

const eps = 1e-9

func TestApply(t *testing.T) {
	zero := Model{}

	tests := []struct {
		name      string
		model     Model
		side      broker.Side
		qty       int
		rawPrice  float64
		wantPrice float64
		wantComm  float64
		wantFees  float64
	}{
		{
			name:      "normal buy pays slippage up and the commission minimum",
			model:     Default(),
			side:      broker.Buy,
			qty:       100,
			rawPrice:  50,
			wantPrice: 50.025,
			wantComm:  1.00,
			wantFees:  0,
		},
		{
			name:      "normal sell pays slippage down plus SEC and TAF",
			model:     Default(),
			side:      broker.Sell,
			qty:       100,
			rawPrice:  50,
			wantPrice: 49.975,
			wantComm:  1.00,
			wantFees:  0.16,
		},
		{
			name:      "per-share commission beats the minimum on size",
			model:     Default(),
			side:      broker.Buy,
			qty:       1000,
			rawPrice:  50,
			wantPrice: 50.025,
			wantComm:  5.00,
			wantFees:  0,
		},
		{
			name:      "tiny trade is capped at one percent of value",
			model:     Default(),
			side:      broker.Buy,
			qty:       1,
			rawPrice:  5,
			wantPrice: 5.0025,
			wantComm:  0.05,
			wantFees:  0,
		},
		{
			name:      "large sell hits the TAF cap",
			model:     Default(),
			side:      broker.Sell,
			qty:       100_000,
			rawPrice:  10,
			wantPrice: 9.995,
			wantComm:  500.00,
			wantFees:  8.30 + 27.79,
		},
		{
			name:      "zero slippage leaves the raw price alone",
			model:     Model{CommissionPerShare: 0.005, CommissionMin: 1.00, CommissionMaxPct: 1.0},
			side:      broker.Buy,
			qty:       100,
			rawPrice:  50,
			wantPrice: 50,
			wantComm:  1.00,
			wantFees:  0,
		},
		{
			name:      "zero-cost model returns the raw price and no costs",
			model:     zero,
			side:      broker.Sell,
			qty:       250,
			rawPrice:  42.17,
			wantPrice: 42.17,
			wantComm:  0,
			wantFees:  0,
		},
		{
			name:      "non-positive quantity costs nothing",
			model:     Default(),
			side:      broker.Buy,
			qty:       0,
			rawPrice:  50,
			wantPrice: 50,
			wantComm:  0,
			wantFees:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.model.Apply(tc.side, tc.qty, tc.rawPrice)
			if math.Abs(got.ModeledPrice-tc.wantPrice) > eps {
				t.Errorf("ModeledPrice = %v, want %v", got.ModeledPrice, tc.wantPrice)
			}
			if math.Abs(got.Commission-tc.wantComm) > eps {
				t.Errorf("Commission = %v, want %v", got.Commission, tc.wantComm)
			}
			if math.Abs(got.Fees-tc.wantFees) > eps {
				t.Errorf("Fees = %v, want %v", got.Fees, tc.wantFees)
			}
			if want := tc.wantComm + tc.wantFees; math.Abs(got.Total()-want) > eps {
				t.Errorf("Total() = %v, want %v", got.Total(), want)
			}
		})
	}
}

// The headline number for a small account: 100 shares of a $50 stock costs the
// $1.00 IBKR minimum, and selling adds $0.14 SEC plus $0.02 TAF.
func TestDefaultHundredSharesAtFifty(t *testing.T) {
	m := Default()

	buy := m.Apply(broker.Buy, 100, 50)
	if buy.Commission != 1.00 {
		t.Errorf("buy commission = %v, want 1.00", buy.Commission)
	}
	if buy.Fees != 0 {
		t.Errorf("buy fees = %v, want 0", buy.Fees)
	}

	sell := m.Apply(broker.Sell, 100, 50)
	if sell.Commission != 1.00 {
		t.Errorf("sell commission = %v, want 1.00", sell.Commission)
	}
	if want := 0.14 + 0.02; math.Abs(sell.Fees-want) > eps {
		t.Errorf("sell fees = %v, want %v", sell.Fees, want)
	}
}

func TestApplySlippageDirection(t *testing.T) {
	m := Model{SlippageBps: 100}

	buy := m.Apply(broker.Buy, 10, 100)
	if math.Abs(buy.ModeledPrice-101) > eps {
		t.Errorf("buy at 100 with 100bps = %v, want 101", buy.ModeledPrice)
	}

	sell := m.Apply(broker.Sell, 10, 100)
	if math.Abs(sell.ModeledPrice-99) > eps {
		t.Errorf("sell at 100 with 100bps = %v, want 99", sell.ModeledPrice)
	}
}

// F6: a negative notional ran through the percent cap and produced a negative
// commission, so an impossible fill paid the ledger instead of charging it.
func TestApplyRefusesNonPositivePrice(t *testing.T) {
	m := Default()

	for _, raw := range []float64{0, -8990, math.NaN(), math.Inf(-1)} {
		got := m.Apply(broker.Buy, 1000, raw)
		if got != (Result{}) {
			t.Errorf("Apply at raw price %v = %+v, want a zero Result", raw, got)
		}
	}

	// The cap itself must never hand back a credit, whatever the model holds.
	inverted := Model{CommissionPerShare: 0.005, CommissionMin: 1.00, CommissionMaxPct: 1.0}
	if got := inverted.Apply(broker.Buy, 1, -100); got.Commission < 0 {
		t.Errorf("Commission = %v, want no credit on an invalid price", got.Commission)
	}
}

func TestApplyRoundsCommissionHalfUp(t *testing.T) {
	// 3 shares at $0.005 is $0.015, which must round up to $0.02, and the 1% cap
	// on $300 of value is far above it.
	m := Model{CommissionPerShare: 0.005, CommissionMaxPct: 1.0}
	got := m.Apply(broker.Buy, 3, 100)
	if got.Commission != 0.02 {
		t.Errorf("Commission = %v, want 0.02", got.Commission)
	}
}
