package retro

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The three edits a diff can be. Anything else never reaches the file.
const (
	ChangeAdd    = "add"
	ChangeEdit   = "edit"
	ChangeRemove = "remove"
)

// ErrRiskRules refuses an edit to the section whose numbers Go enforces. The
// model plans inside those limits; it does not get to move them.
var ErrRiskRules = errors.New("the risk rules are enforced in code and are not the model's to edit")

// riskSection is that section's title, without its hashes.
const riskSection = "Risk rules"

// blankRun collapses the gap a removal leaves back to one blank line.
var blankRun = regexp.MustCompile(`\n{3,}`)

// applyAll applies the diffs to text in order, so a later edit sees what an
// earlier one wrote. The result is what the playbook would become.
func applyAll(text string, diffs []Diff) (string, error) {
	for i, d := range diffs {
		next, err := applyOne(text, d)
		if err != nil {
			return "", fmt.Errorf("retro: diff %d (%s, %s): %w", i+1, d.Change, oneLine(d.Section), err)
		}
		text = next
	}
	return text, nil
}

// applyOne resolves one diff against the current text and returns the text it
// produces. Every refusal names what it could not find.
func applyOne(text string, d Diff) (string, error) {
	sec, err := findSection(text, d.Section)
	if err != nil {
		return "", err
	}
	// The risk rules carry their children, so a subsection under them is refused
	// exactly like the section itself.
	if strings.EqualFold(enclosingSection(text, sec).title, riskSection) {
		return "", ErrRiskRules
	}
	if err := checkAfter(text, d.After); err != nil {
		return "", err
	}
	end := sectionEnd(text, sec)

	switch d.Change {
	case ChangeAdd:
		if strings.TrimSpace(d.After) == "" {
			return "", errors.New("an add with no text adds nothing")
		}
		return insertAt(text, end, d.After), nil
	case ChangeEdit, ChangeRemove:
		body := text[sec.next:end]
		if err := locate(body, d, sec); err != nil {
			return "", err
		}
		replacement := d.After
		if d.Change == ChangeRemove {
			replacement = ""
		}
		edited := blankRun.ReplaceAllString(strings.Replace(body, d.Before, replacement, 1), "\n\n")
		return text[:sec.next] + edited + text[end:], nil
	default:
		return "", fmt.Errorf("change must be %s, %s, or %s", ChangeAdd, ChangeEdit, ChangeRemove)
	}
}

// locate checks that the text a diff quotes identifies exactly one place under
// the section it named. Anything else would edit something nobody chose.
func locate(body string, d Diff, sec heading) error {
	if d.Before == "" {
		return fmt.Errorf("%s needs the text it replaces in before", d.Change)
	}
	switch n := strings.Count(body, d.Before); n {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("the before text does not appear under %q", sec.text)
	default:
		return fmt.Errorf("the before text appears %d times under %q; it has to name one place", n, sec.text)
	}
}

// enclosingSection is the "##" section a heading sits under, or the heading
// itself when nothing shallower precedes it.
func enclosingSection(text string, sec heading) heading {
	out := sec
	for _, h := range headings(text) {
		if h.start > sec.start {
			break
		}
		if h.level == 2 {
			out = h
		}
	}
	return out
}

// findSection locates the heading a diff names. The model may copy the heading
// with its hashes or type the title alone; both resolve to the same section, and
// a title matching two headings resolves to neither.
func findSection(text, want string) (heading, error) {
	want = strings.TrimSpace(want)
	if want == "" {
		return heading{}, errors.New("no section named")
	}
	var found []heading
	for _, h := range headings(text) {
		if h.text == want || h.title == want {
			found = append(found, h)
		}
	}
	switch len(found) {
	case 1:
	case 0:
		return heading{}, fmt.Errorf("no heading %q in the playbook", want)
	default:
		return heading{}, fmt.Errorf("%q names %d headings; copy the whole heading line", want, len(found))
	}
	if found[0].level < 2 {
		return heading{}, fmt.Errorf("%q is the document title, not a section", found[0].text)
	}
	return found[0], nil
}

// sectionEnd is where a section's body stops: at the next heading of the same
// depth or shallower, so a "##" section carries its "###" children.
func sectionEnd(text string, sec heading) int {
	for _, h := range headings(text) {
		if h.start > sec.start && h.level <= sec.level {
			return h.start
		}
	}
	return len(text)
}

// insertAt puts new text at the end of a section, keeping one blank line between
// what was already there and what arrives.
func insertAt(text string, at int, addition string) string {
	out := strings.TrimRight(text[:at], "\n") + "\n\n" + strings.TrimSpace(addition) + "\n"
	if tail := strings.TrimLeft(text[at:], "\n"); tail != "" {
		out += "\n" + tail
	}
	return out
}
