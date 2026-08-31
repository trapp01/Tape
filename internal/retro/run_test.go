package retro

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trapp01/tape/internal/llm"
)

// fakeProvider replies with a canned string and counts how often it was asked.
type fakeProvider struct {
	reply string
	calls int
}

func (p *fakeProvider) Name() string  { return "fake" }
func (p *fakeProvider) Model() string { return "fake-model-1" }

func (p *fakeProvider) Complete(context.Context, llm.Request) (llm.Response, error) {
	p.calls++
	return llm.Response{Text: p.reply, Model: p.Model(), InputTokens: 9200, OutputTokens: 600}, nil
}

// reply wraps findings and diffs in the rest of the schema.
func reply(diffs string) string {
	return `{
	  "summary": "Two trades is not a sample; nothing here supports a rule change.",
	  "findings": [
	    {"title": "M1 is the only rule that traded", "evidence": "1 trade, +$39.00 net", "confidence": "low"}
	  ],
	  "diffs": [` + diffs + `]
	}`
}

func runRetro(t *testing.T, st *fakeStore, replyText string) (Result, error) {
	t.Helper()
	return Run(context.Background(), testDeps(t, st, ""), &fakeProvider{reply: replyText})
}

// An empty diffs list is a valid answer, and on a week this short it is the
// correct one.
func TestRunArchivesAReviewWithNoDiffs(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	res, err := runRetro(t, st, reply(""))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Retro.ID == 0 || res.Retro.Model != "fake-model-1" || res.Retro.FromDay != "2026-08-22" {
		t.Fatalf("archived row = %+v", res.Retro)
	}
	if len(res.Output.Findings) != 1 || len(res.Diffs) != 0 {
		t.Fatalf("output = %+v", res.Output)
	}
	var archived Output
	if err := json.Unmarshal(st.retros[0].InputJSON, &Input{}); err != nil {
		t.Fatalf("the archived input is not readable: %v", err)
	}
	if err := json.Unmarshal(st.retros[0].OutputJSON, &archived); err != nil {
		t.Fatalf("the archived reply is not readable: %v", err)
	}
}

func TestRunArchivesAValidDiff(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	res, err := runRetro(t, st, reply(`
	  {"section": "### M1 gap-and-go continuation", "change": "edit",
	   "rationale": "The one M1 trade held its gap.",
	   "before": "Enter on the first pullback that holds the gap.",
	   "after": "Enter on the first pullback that holds the gap and the prior day's high."}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Diffs) != 1 || res.Diffs[0].Index != 1 || res.Diffs[0].Change != ChangeEdit {
		t.Fatalf("diffs = %+v", res.Diffs)
	}
	if res.Diffs[0].AppliedAt != nil {
		t.Fatalf("a proposed diff is not an applied one: %+v", res.Diffs[0])
	}
}

// The risk rules are enforced in Go. A review that reaches for them is archived
// and refused, never quietly dropped.
func TestRunRefusesADiffOnTheRiskRules(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	_, err := runRetro(t, st, reply(`
	  {"section": "## Risk rules", "change": "edit", "rationale": "Bigger size would have paid.",
	   "before": "Per trade 0.5% of equity, lost at the stop.",
	   "after": "Per trade 2% of equity, lost at the stop."}`))
	if err == nil {
		t.Fatal("a diff on the risk rules must be refused")
	}
	if !strings.Contains(err.Error(), "not the model's to edit") {
		t.Fatalf("refusal = %v", err)
	}
	if len(st.retros) != 1 {
		t.Fatalf("the refused reply must still be archived, %d rows", len(st.retros))
	}
	if len(st.diffs[st.retros[0].ID]) != 0 {
		t.Fatalf("a refused diff must not be filed as proposable: %+v", st.diffs)
	}
}

// A before that is not in the playbook cannot identify a place to edit, so the
// review is archived and refused rather than applied to something else.
func TestRunRefusesABeforeItCannotFind(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	_, err := runRetro(t, st, reply(`
	  {"section": "### M1 gap-and-go continuation", "change": "edit", "rationale": "Tighter entry.",
	   "before": "Enter on the second pullback.", "after": "Enter on the third pullback."}`))
	if err == nil {
		t.Fatal("a before the playbook does not carry must be refused")
	}
	if !strings.Contains(err.Error(), "does not appear under") {
		t.Fatalf("refusal = %v", err)
	}
	if len(st.retros) != 1 {
		t.Fatalf("the refused reply must still be archived, %d rows", len(st.retros))
	}
}

// A new setup has to be citable. A heading the convention cannot resolve would
// leave a rule no proposal is allowed to name.
func TestRunRefusesASetupWithNoID(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	_, err := runRetro(t, st, reply(`
	  {"section": "## Setups", "change": "add", "rationale": "A second continuation.",
	   "before": "", "after": "### Afternoon fade\n\nShort the second push."}`))
	if err == nil {
		t.Fatal("a setup heading with no id must be refused")
	}
	if !strings.Contains(err.Error(), "### <ID> title") {
		t.Fatalf("refusal = %v", err)
	}
}

// A reply that is not JSON is kept verbatim on the row, so the failure is
// readable later instead of guessed at.
func TestRunArchivesAnUnparseableReply(t *testing.T) {
	st := newFakeStore()
	seed(t, st)

	_, err := runRetro(t, st, "the model wrote an essay")
	if err == nil {
		t.Fatal("a reply that is not JSON must be reported as failed")
	}
	if len(st.retros) != 1 || string(st.retros[0].OutputJSON) != "the model wrote an essay" {
		t.Fatalf("the raw reply was not archived: %q", st.retros[0].OutputJSON)
	}
}
