package brief

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/trapp01/tape/internal/journal"
	"github.com/trapp01/tape/internal/llm"
)

// maxOutputTokens bounds the reply. The schema is small; a model that needs more
// than this is repeating itself.
const maxOutputTokens = 4096

// Result is one morning's work: what the model saw, what it said, and the rows
// that now carry it.
type Result struct {
	Input    Input
	Output   Output
	Briefing journal.Briefing
	// Call is nil when the session's call was left standing.
	Call   *journal.Call
	Reused bool
	// CallKept is true when a forced re-run archived a new briefing and left the
	// session's call standing, which is what happens after the open.
	CallKept bool
	// CallReplaced is true when a forced re-run swapped the session's call before
	// the bell, while nothing had been predicted about a session in progress.
	CallReplaced bool
}

// Run archives the session's briefing and files its call. An existing briefing
// for that session is returned as-is unless Deps.Force is set.
func Run(ctx context.Context, d Deps, p llm.Provider) (Result, error) {
	if d.Journal == nil {
		return Result{}, errors.New("brief: no journal configured")
	}
	v := d.session(ctx)

	if !d.Force {
		res, found, err := reuse(ctx, d, v.day)
		if err != nil || found {
			return res, err
		}
	}

	in, err := Assemble(ctx, d)
	if err != nil {
		return Result{}, err
	}
	inputJSON, err := json.Marshal(in)
	if err != nil {
		return Result{}, fmt.Errorf("brief: encoding the archived input: %w", err)
	}

	system, user := BuildPrompt(in)
	start := time.Now()
	resp, err := p.Complete(ctx, llm.Request{
		System:     system,
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: user}},
		MaxTokens:  maxOutputTokens,
		JSONSchema: Schema(),
		SchemaName: "briefing",
	})
	if err != nil {
		return Result{}, fmt.Errorf("brief: asking %s (%s): %w", p.Name(), p.Model(), err)
	}

	b := journal.Briefing{
		Mode:         d.Mode,
		GeneratedAt:  d.now().UTC(),
		Day:          v.day,
		Provider:     p.Name(),
		Model:        pick(resp.Model, p.Model()),
		InputJSON:    inputJSON,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      resp.CostUSD,
		LatencyMs:    time.Since(start).Milliseconds(),
	}

	out, outputJSON, parseErr := decodeReply(resp.Text, in)
	b.OutputJSON = outputJSON
	if err := d.Journal.InsertBriefing(ctx, &b); err != nil {
		return Result{}, err
	}
	if parseErr != nil {
		return Result{Input: in, Briefing: b}, fmt.Errorf("brief: briefing #%d archived but its reply failed validation: %w", b.ID, parseErr)
	}

	res := Result{Input: in, Output: out, Briefing: b}
	filed, err := fileCall(ctx, d, b, out, v)
	if err != nil {
		return res, err
	}
	res.Call, res.CallKept, res.CallReplaced = filed.call, filed.kept, filed.replaced
	return res, nil
}

// reuse returns the session's archived briefing so the ritual is idempotent:
// reading it again must not cost another call or overwrite what was predicted.
func reuse(ctx context.Context, d Deps, day string) (Result, bool, error) {
	b, err := d.Journal.LatestBriefing(ctx, d.Mode, day)
	if errors.Is(err, journal.ErrNotFound) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	res, err := FromArchive(ctx, d.Journal, b)
	res.Reused = true
	if err != nil {
		return res, true, fmt.Errorf("%w; re-run with --force to ask again", err)
	}
	return res, true, nil
}

// FromArchive rebuilds a Result from an archived row, so an old briefing reads
// back exactly as it was written. A reply that failed validation when it arrived
// fails it again here: the row keeps the raw text, not a usable briefing.
func FromArchive(ctx context.Context, jnl *journal.Store, b journal.Briefing) (Result, error) {
	res := Result{Briefing: b}
	if err := json.Unmarshal(b.InputJSON, &res.Input); err != nil {
		return res, fmt.Errorf("brief: briefing #%d has an unreadable archived input: %w", b.ID, err)
	}
	if err := json.Unmarshal(b.OutputJSON, &res.Output); err != nil {
		res.Output = Output{}
		return res, fmt.Errorf("brief: briefing #%d was archived without a usable reply (the raw text is on the row): %w", b.ID, err)
	}
	if err := ValidateAgainst(res.Output, res.Input); err != nil {
		res.Output = Output{}
		return res, fmt.Errorf("brief: briefing #%d archived but its reply failed validation: %w", b.ID, err)
	}
	call, err := jnl.CallByBriefing(ctx, b.ID)
	if err == nil {
		res.Call = &call
	} else if !errors.Is(err, journal.ErrNotFound) {
		return res, err
	}
	return res, nil
}

// decodeReply returns the parsed output and the bytes to archive. A reply that
// cannot be parsed or validated is still archived, verbatim, with the error.
func decodeReply(text string, in Input) (Output, []byte, error) {
	raw, err := llm.ExtractJSON(text)
	if err != nil {
		return Output{}, rawArchive(text), err
	}
	var out Output
	if err := json.Unmarshal(raw, &out); err != nil {
		return Output{}, raw, fmt.Errorf("brief: decoding the reply: %w", err)
	}
	if err := ValidateAgainst(out, in); err != nil {
		return Output{}, raw, err
	}
	return out, raw, nil
}

// rawArchive keeps an unparseable reply readable in the journal, which requires
// a non-empty output column.
func rawArchive(text string) []byte {
	if text == "" {
		return []byte("(the model returned an empty reply)")
	}
	return []byte(text)
}

// filing is what a run did to the session's call: filed a new one, kept the one
// that was there, or swapped it.
type filing struct {
	call           *journal.Call
	kept, replaced bool
}

// fileCall records the prediction under the session it is about. The call of the
// day locks at the open: before the bell a forced re-run replaces it, after the
// bell the first one stands and the new briefing is only a second read.
func fileCall(ctx context.Context, d Deps, b journal.Briefing, out Output, v venue) (filing, error) {
	// The row carries the threshold the call is graded against, resolved now: a
	// later default must not change what an old call meant.
	threshold := d.Cfg.CallThresholdPct
	if out.Call.ThresholdPct != nil {
		threshold = *out.Call.ThresholdPct
	}
	call := journal.Call{
		BriefingID:   b.ID,
		Mode:         d.Mode,
		Day:          v.day,
		Instrument:   out.Call.Instrument,
		Direction:    string(out.Call.Direction),
		ThresholdPct: threshold,
		Rationale:    out.Call.Rationale,
	}

	existing, err := d.Journal.CallByDay(ctx, d.Mode, v.day)
	if errors.Is(err, journal.ErrNotFound) {
		if err := d.Journal.InsertCall(ctx, &call); err != nil {
			return filing{}, err
		}
		return filing{call: &call}, nil
	}
	if err != nil {
		return filing{}, err
	}
	if v.opened || existing.ScoredAt != nil {
		return filing{kept: true}, nil
	}
	if err := d.Journal.ReplaceCall(ctx, existing.ID, &call); err != nil {
		return filing{}, err
	}
	return filing{call: &call, replaced: true}, nil
}

func pick(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}
