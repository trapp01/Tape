package brief

import "strings"

// Confidence is the model's own read of a proposal; it never changes the size.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// confidences is every value a proposal may carry, weakest first.
var confidences = []Confidence{ConfidenceLow, ConfidenceMedium, ConfidenceHigh}

func confidenceList() string {
	names := make([]string, len(confidences))
	for i, c := range confidences {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}

// SideLong is the only side v1 trades.
const SideLong = "long"

// Proposal is one trade idea from the model. Every price is required — an idea
// without its exit is a note, not a proposal — and the size is never the
// model's: Go computes it from the risk limits and the stop distance.
type Proposal struct {
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	// SetupID is the playbook rule the proposal rests on, e.g. "M2".
	SetupID      string     `json:"setup_id"`
	Entry        float64    `json:"entry"`
	Stop         float64    `json:"stop"`
	Target       float64    `json:"target"`
	Thesis       string     `json:"thesis"`
	Invalidation string     `json:"invalidation"`
	Confidence   Confidence `json:"confidence"`
}

// MaxProposals bounds how many ideas a briefing may carry.
const MaxProposals = 3
