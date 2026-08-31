package playbook

import (
	"regexp"
	"slices"
	"strings"
)

// setupID matches the id a setup heading opens with: one uppercase letter and a
// number, like M1 or R2. Anything else is a plain heading, not a setup.
var setupID = regexp.MustCompile(`^[A-Z][0-9]+$`)

// SetupIDs lists the ids a proposal may cite, in the order the playbook defines
// them. An id is the first word of a "### " heading; a repeat is ignored.
func SetupIDs(text string) []string {
	var out []string
	for _, words := range setupHeadings(text) {
		if !slices.Contains(out, words[0]) {
			out = append(out, words[0])
		}
	}
	return out
}

// EntrySetupIDs is SetupIDs without the no-trade rules. An id opening with N is
// a condition that ends the discussion for a symbol, so a proposal citing one is
// arguing for the trade its own rule forbids.
func EntrySetupIDs(text string) []string {
	var out []string
	for _, id := range SetupIDs(text) {
		if !strings.HasPrefix(id, "N") {
			out = append(out, id)
		}
	}
	return out
}

// SetupTitles maps each setup id to the whole heading it opens, so a proposal
// citing M2 can be shown next to what M2 is.
func SetupTitles(text string) map[string]string {
	out := map[string]string{}
	for _, words := range setupHeadings(text) {
		if _, seen := out[words[0]]; !seen {
			out[words[0]] = strings.Join(words, " ")
		}
	}
	return out
}

// setupHeadings is every "### " heading that opens with a setup id, in file
// order, split into words.
func setupHeadings(text string) [][]string {
	var out [][]string
	for _, line := range strings.Split(text, "\n") {
		heading, ok := strings.CutPrefix(strings.TrimSpace(line), "### ")
		if !ok {
			continue
		}
		words := strings.Fields(heading)
		if len(words) == 0 || !setupID.MatchString(words[0]) {
			continue
		}
		out = append(out, words)
	}
	return out
}
