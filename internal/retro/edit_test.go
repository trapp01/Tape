package retro

import (
	"strings"
	"testing"
)

// An add lands at the end of its own section, not at the end of the file, so a
// new setup does not fall under the risk rules.
func TestAddLandsInsideItsSection(t *testing.T) {
	got, err := applyAll(testPlaybook, []Diff{{
		Section: "## Setups", Change: ChangeAdd,
		After: "### M2 afternoon continuation\n\nEnter the second push over the noon high.",
	}})
	if err != nil {
		t.Fatalf("applyAll: %v", err)
	}
	if strings.Index(got, "### M2") < strings.Index(got, "### N1") {
		t.Fatalf("the addition jumped ahead of the section's own rules:\n%s", got)
	}
	if strings.Index(got, "### M2") > strings.Index(got, "## Risk rules") {
		t.Fatalf("the addition landed outside its section:\n%s", got)
	}
	if !strings.Contains(got, "## Risk rules\n\nPer trade 0.5%") {
		t.Fatalf("the section after the addition was disturbed:\n%s", got)
	}
}

// A remove takes the text and the gap it leaves, not the blank line structure of
// everything around it.
func TestRemoveCollapsesTheGapItLeaves(t *testing.T) {
	got, err := applyAll(testPlaybook, []Diff{{
		Section: "## Setups", Change: ChangeRemove,
		Before: "### N1 no-trade conditions\n\nStand down on an FOMC afternoon.\n",
	}})
	if err != nil {
		t.Fatalf("applyAll: %v", err)
	}
	if strings.Contains(got, "N1") {
		t.Fatalf("the rule is still there:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("the removal left a hole:\n%s", got)
	}
}

// Later diffs see what earlier ones wrote, so two edits to one section cannot
// resolve against a file neither of them produced.
func TestDiffsApplyInOrder(t *testing.T) {
	got, err := applyAll(testPlaybook, []Diff{
		{Section: "### M1 gap-and-go continuation", Change: ChangeEdit,
			Before: "Enter on the first pullback", After: "Enter on the second pullback"},
		{Section: "### M1 gap-and-go continuation", Change: ChangeEdit,
			Before: "Enter on the second pullback that holds the gap.", After: "Skip it."},
	})
	if err != nil {
		t.Fatalf("applyAll: %v", err)
	}
	if !strings.Contains(got, "Skip it.") || strings.Contains(got, "pullback") {
		t.Fatalf("the second edit did not see the first:\n%s", got)
	}
}

// A section is found whether the model copies the heading or types the title, but
// text that appears twice under it names no single place to edit.
func TestFindSectionAndAmbiguousBefore(t *testing.T) {
	for _, name := range []string{"### M1 gap-and-go continuation", "M1 gap-and-go continuation"} {
		if _, err := findSection(testPlaybook, name); err != nil {
			t.Fatalf("findSection(%q): %v", name, err)
		}
	}
	if _, err := findSection(testPlaybook, "## Setups (revised)"); err == nil {
		t.Fatal("a heading that is not in the playbook must be refused")
	}

	twice := testPlaybook + "\n## Notes\n\nHold the gap.\n\nHold the gap.\n"
	_, err := applyAll(twice, []Diff{{Section: "## Notes", Change: ChangeEdit, Before: "Hold the gap.", After: "Hold it."}})
	if err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Fatalf("an ambiguous before must be refused, got: %v", err)
	}
}

func TestApplyRefusesAnUnknownChange(t *testing.T) {
	_, err := applyAll(testPlaybook, []Diff{{Section: "## Setups", Change: "rewrite", After: "anything"}})
	if err == nil || !strings.Contains(err.Error(), "change must be") {
		t.Fatalf("refusal = %v", err)
	}
}

// New text may not smuggle in a risk section under a heading the model is
// allowed to name.
func TestApplyRefusesARiskSectionInNewText(t *testing.T) {
	_, err := applyAll(testPlaybook, []Diff{{
		Section: "## Setups", Change: ChangeAdd,
		After: "## Risk rules\n\nPer trade 4% of equity.",
	}})
	if err == nil || !strings.Contains(err.Error(), "not the model's to edit") {
		t.Fatalf("refusal = %v", err)
	}
}

// A diff edits text inside a section. New text that opens a section of its own
// restructures the playbook, which is the trader's job, not the model's.
func TestApplyRefusesANewTopLevelSection(t *testing.T) {
	for name, after := range map[string]string{
		"a level-2 heading": "## Extra rules\n\nSize up after two winners.",
		"a level-1 heading": "# Playbook v2\n\nStart again.",
		"a setext h1":       "Extra rules\n===========\n\nSize up after two winners.",
		"a setext h2":       "Extra rules\n-----------\n\nSize up after two winners.",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := applyAll(testPlaybook, []Diff{{Section: "## Setups", Change: ChangeAdd, After: after}})
			if err == nil || !strings.Contains(err.Error(), "section of its own") {
				t.Fatalf("refusal = %v", err)
			}
		})
	}
}

// An id names one rule. A second "### M1" would make every stat cut by setup a
// blend of two rules nobody can tell apart.
func TestApplyRefusesADuplicateSetupID(t *testing.T) {
	_, err := applyAll(testPlaybook, []Diff{{
		Section: "## Setups", Change: ChangeAdd,
		After: "### M1 second gap rule\n\nEnter the second push.",
	}})
	if err == nil || !strings.Contains(err.Error(), "M1") {
		t.Fatalf("refusal = %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "already") {
		t.Fatalf("refusal should say the id is taken, got: %v", err)
	}
}

// The risk rules carry their children. A subsection nested under them is still
// a number Go enforces.
func TestApplyRefusesASectionNestedUnderTheRiskRules(t *testing.T) {
	nested := testPlaybook + "\n### Position sizing\n\nRound down to whole shares.\n"
	_, err := applyAll(nested, []Diff{{
		Section: "### Position sizing", Change: ChangeEdit,
		Before: "Round down to whole shares.", After: "Round up to whole shares.",
	}})
	if err == nil || !strings.Contains(err.Error(), "not the model's to edit") {
		t.Fatalf("refusal = %v", err)
	}
}

func TestValidateChecksTheReplyShape(t *testing.T) {
	cases := map[string]Output{
		"the summary is empty": {Summary: "  "},
		"cites no numbers":     {Summary: "ok", Findings: []Finding{{Title: "t", Confidence: "low"}}},
		"is not one of":        {Summary: "ok", Findings: []Finding{{Title: "t", Evidence: "e", Confidence: "certain"}}},
		"has no rationale":     {Summary: "ok", Diffs: []Diff{{Section: "## Setups", Change: ChangeAdd, After: "x"}}},
	}
	for want, out := range cases {
		_, err := Validate(testPlaybook, out)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate(%+v) = %v, want an error naming %q", out, err, want)
		}
	}

	if _, err := Validate(testPlaybook, Output{Summary: "Nothing here supports a change."}); err != nil {
		t.Fatalf("an empty diffs list is a valid answer: %v", err)
	}
}
