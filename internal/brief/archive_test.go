package brief

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/trapp01/tape/internal/journal"
)

// The archive is what `tape why` recovers a proposal's sizing basis from. Field
// names came from Go's defaults, so renaming a field silently turned an old
// briefing's equity and limits into zeroes and the math printed as nonsense.
func TestArchivedInputRoundTripsUnderStableNames(t *testing.T) {
	in := briefingData()
	in.GeneratedAt = time.Date(2026, 8, 28, 12, 52, 0, 0, time.UTC)
	in.Timezone, in.Mode = "America/Edmonton", journal.ModePaper
	in.Equity, in.LedgerCash, in.FreeCash = 5000, 4800, 4750
	in.Limits = deskLimits()

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshalling the input: %v", err)
	}
	for _, want := range []string{
		`"generated_at"`, `"ledger_cash"`, `"free_cash"`, `"equity"`, `"limits"`,
		`"per_trade_pct"`, `"prev_close"`, `"change_pct"`, `"market_open"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("archived input has no %s field:\n%s", want, raw)
		}
	}

	got, err := ArchivedInput(journal.Briefing{ID: 1, InputJSON: raw})
	if err != nil {
		t.Fatalf("ArchivedInput: %v", err)
	}
	if got.Equity != in.Equity || got.FreeCash != in.FreeCash || got.LedgerCash != in.LedgerCash {
		t.Fatalf("cash figures = %v/%v/%v, want %v/%v/%v",
			got.Equity, got.FreeCash, got.LedgerCash, in.Equity, in.FreeCash, in.LedgerCash)
	}
	if got.Limits != in.Limits {
		t.Fatalf("limits = %+v, want %+v", got.Limits, in.Limits)
	}
	if len(got.Indexes) != len(in.Indexes) || got.Indexes[0].Symbol != in.Indexes[0].Symbol {
		t.Fatalf("indexes = %+v, want %+v", got.Indexes, in.Indexes)
	}
}
