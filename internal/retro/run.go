package retro

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
const maxOutputTokens = 8192

// Run assembles the record, asks the model what it shows, and archives the reply
// with the diffs it proposed. A reply that fails validation is still archived,
// verbatim, and reported as failed — never reused as if it were valid.
func Run(ctx context.Context, d Deps, p llm.Provider) (Result, error) {
	if d.Journal == nil {
		return Result{}, errors.New("retro: no journal configured")
	}
	in, err := Assemble(ctx, d)
	if err != nil {
		return Result{}, err
	}
	inputJSON, err := json.Marshal(in)
	if err != nil {
		return Result{}, fmt.Errorf("retro: encoding the archived input: %w", err)
	}

	system, user := BuildPrompt(in)
	start := time.Now()
	resp, err := p.Complete(ctx, llm.Request{
		System:     system,
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: user}},
		MaxTokens:  maxOutputTokens,
		JSONSchema: Schema(),
		SchemaName: "retro",
	})
	if err != nil {
		return Result{}, fmt.Errorf("retro: asking %s (%s): %w", p.Name(), p.Model(), err)
	}

	r := journal.Retro{
		Mode:         d.Mode,
		GeneratedAt:  d.now().UTC(),
		FromDay:      in.FromDay,
		ToDay:        in.ToDay,
		Provider:     p.Name(),
		Model:        pick(resp.Model, p.Model()),
		InputJSON:    inputJSON,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CostUSD:      resp.CostUSD,
		LatencyMs:    time.Since(start).Milliseconds(),
	}

	out, outputJSON, parseErr := decodeReply(resp.Text, in.Playbook)
	r.OutputJSON = outputJSON
	rows := diffRows(out)
	if err := d.Journal.InsertRetro(ctx, &r, rows); err != nil {
		return Result{}, err
	}
	if parseErr != nil {
		return Result{Input: in, Retro: r}, fmt.Errorf("retro: review #%d archived but its reply failed validation: %w", r.ID, parseErr)
	}

	res := Result{Input: in, Output: out, Retro: r}
	for _, row := range rows {
		res.Diffs = append(res.Diffs, *row)
	}
	return res, nil
}

// decodeReply returns the parsed reply and the bytes to archive. A reply that
// cannot be parsed or validated is archived anyway, with the error.
func decodeReply(text, playbookText string) (Output, []byte, error) {
	raw, err := llm.ExtractJSON(text)
	if err != nil {
		return Output{}, rawArchive(text), err
	}
	var out Output
	if err := json.Unmarshal(raw, &out); err != nil {
		return Output{}, raw, fmt.Errorf("retro: decoding the reply: %w", err)
	}
	if _, err := Validate(playbookText, out); err != nil {
		return Output{}, raw, err
	}
	return out, raw, nil
}

// rawArchive keeps an unparseable reply readable in the journal, which requires a
// non-empty output column.
func rawArchive(text string) []byte {
	if text == "" {
		return []byte("(the model returned an empty reply)")
	}
	return []byte(text)
}

func pick(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}
