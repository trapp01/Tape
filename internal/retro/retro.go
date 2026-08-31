// Package retro is the weekly mirror. It reads the scored record, asks the model
// what the numbers say, and turns the answer into exact playbook edits the trader
// applies one at a time. Nothing here changes a rule on its own.
package retro

import (
	"context"
	"encoding/json"
	"time"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/risk"
	"github.com/trapp01/tape/internal/stats"
)

// dayLayout matches the journal's day columns, so day strings compare as text.
const dayLayout = "2006-01-02"

// Store is the slice of the journal a retro reads and writes. *journal.Store
// satisfies it.
type Store interface {
	stats.Source
	VersionStore
	ListRetros(ctx context.Context, mode string, limit int) ([]journal.Retro, error)
	InsertRetro(ctx context.Context, r *journal.Retro, diffs []*journal.RetroDiff) error
	RetroByID(ctx context.Context, id int64) (journal.Retro, error)
	DiffsByRetro(ctx context.Context, retroID int64) ([]journal.RetroDiff, error)
	// ApplyRetroDiffs files the snapshot and marks every diff in one transaction.
	ApplyRetroDiffs(ctx context.Context, v *journal.PlaybookVersion, diffIDs []int64, at time.Time) error
}

// VersionStore is the slice of the journal playbook bookkeeping needs.
type VersionStore interface {
	LatestPlaybookVersion(ctx context.Context) (journal.PlaybookVersion, error)
	InsertPlaybookVersion(ctx context.Context, v *journal.PlaybookVersion) error
}

// Deps is everything a review reads. Playbook and PlaybookPath are the same file
// read two ways: the text goes in the prompt, the path is what Apply rewrites.
type Deps struct {
	Journal Store
	Mode    string
	Loc     *time.Location
	// Now is the clock the window ends at. Nil means time.Now.
	Now func() time.Time
	// Weeks is how far back the window reaches; zero takes the config default.
	Weeks        int
	Gate         stats.Gate
	Limits       risk.Limits
	Playbook     string
	PlaybookPath string
	// Cfg fingerprints the rules a version is taken under.
	Cfg config.Config
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func (d Deps) loc() *time.Location {
	if d.Loc == nil {
		return time.UTC
	}
	return d.Loc
}

// Input is everything the model is shown, archived verbatim so a review can be
// re-read next to the record that produced it.
type Input struct {
	GeneratedAt time.Time `json:"generated_at"`
	Timezone    string    `json:"timezone"`
	Mode        string    `json:"mode"`
	FromDay     string    `json:"from_day"`
	ToDay       string    `json:"to_day"`
	Weeks       int       `json:"weeks"`
	// Report covers the review's own window. Gate covers the whole record, and
	// its gate section reads only the sessions since the rules last moved.
	Report stats.Report `json:"report"`
	Gate   stats.Report `json:"gate"`
	// Best and Worst are the three biggest winners and losers of the window.
	Best  []TradeLine `json:"best"`
	Worst []TradeLine `json:"worst"`
	// Passes are the ideas the trader vetoed, next to what the replay says they
	// would have done.
	Passes   []PassLine    `json:"passes"`
	Refusals []RefusalLine `json:"refusals"`
	// PreviousSummary is the last review's own summary, so the model can see what
	// it already said. It is model text, fenced as data in the prompt.
	PreviousSummary string `json:"previous_summary"`
	// Playbook is the strategy file verbatim; SetupIDs are the ids it defines.
	Playbook string      `json:"playbook"`
	SetupIDs []string    `json:"setup_ids"`
	Limits   risk.Limits `json:"limits"`
}

// TradeLine is one closed trade: what it was, and what it did.
type TradeLine struct {
	Day       string  `json:"day"`
	Symbol    string  `json:"symbol"`
	SetupID   string  `json:"setup_id"`
	NetUSD    float64 `json:"net_usd"`
	RMultiple float64 `json:"r_multiple"`
}

// PassLine is one vetoed idea beside its replay. Replayed is false while the
// counterfactual has not been run for it yet.
type PassLine struct {
	Day      string `json:"day"`
	Symbol   string `json:"symbol"`
	SetupID  string `json:"setup_id"`
	Reason   string `json:"reason"`
	Replayed bool   `json:"replayed"`
	Filled   bool   `json:"filled"`
	ExitKind string `json:"exit_kind"`
	// Ambiguous marks a replay the stop-first convention decided rather than the bar.
	Ambiguous bool    `json:"ambiguous"`
	NetUSD    float64 `json:"net_usd"`
	RMultiple float64 `json:"r_multiple"`
}

// RefusalLine counts one guardrail's refusals over the window.
type RefusalLine struct {
	Rule  string `json:"rule"`
	Count int    `json:"count"`
}

// Output is the model's reply, validated against the current playbook in Go
// before a single character of it is trusted.
type Output struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	Diffs    []Diff    `json:"diffs"`
}

// Finding is one thing the numbers say, with the numbers that say it.
type Finding struct {
	Title    string `json:"title"`
	Evidence string `json:"evidence"`
	// Confidence is "low", "medium", or "high".
	Confidence string `json:"confidence"`
}

// Diff is one exact text edit to the playbook. Before must appear verbatim in
// Section for an edit or a remove; After is what replaces or joins it.
type Diff struct {
	Section string `json:"section"`
	// Change is "add", "edit", or "remove".
	Change    string `json:"change"`
	Rationale string `json:"rationale"`
	Before    string `json:"before"`
	After     string `json:"after"`
}

// Result is one review: what the model saw, what it said, and the rows that now
// carry it.
type Result struct {
	Input  Input
	Output Output
	Retro  journal.Retro
	Diffs  []journal.RetroDiff
}

// Schema is the JSON schema Output is validated against. Implemented in schema.go.
func Schema() json.RawMessage { return schema() }
