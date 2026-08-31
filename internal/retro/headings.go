package retro

import (
	"fmt"
	"slices"
	"strings"

	"github.com/trapp01/tape/internal/playbook"
)

// heading is one markdown heading and where its line sits in the text.
type heading struct {
	// text is the whole heading line; title is the same line without its hashes.
	text  string
	title string
	level int
	start int
	next  int
}

// checkAfter refuses new text that restructures the playbook rather than filling
// in a section of it: a heading of its own, a setup the convention cannot cite,
// an id the playbook already defines, or a risk section.
func checkAfter(text, after string) error {
	for _, h := range headings(after) {
		if strings.EqualFold(h.title, riskSection) {
			return ErrRiskRules
		}
		if h.level <= 2 {
			return fmt.Errorf("%q opens a section of its own; a diff fills in the section it names", h.text)
		}
		if h.level != 3 {
			continue
		}
		words := strings.Fields(h.title)
		if len(words) < 2 || !playbook.IsSetupID(words[0]) {
			return fmt.Errorf("setup heading %q has to read \"### <ID> title\", with an id like M3 or R2", h.text)
		}
		if slices.Contains(playbook.SetupIDs(text), words[0]) {
			return fmt.Errorf("the playbook already defines %s; an id names one rule", words[0])
		}
	}
	if line, ok := setextLine(after); ok {
		return fmt.Errorf("%q underlines the line above it into a section of its own; a diff fills in the section it names", line)
	}
	return nil
}

// headings lists every hash heading in text with its byte offsets.
func headings(text string) []heading {
	var out []heading
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if level := hashLevel(trimmed); level > 0 {
			out = append(out, heading{
				text:  trimmed,
				title: strings.TrimSpace(trimmed[level:]),
				level: level,
				start: offset,
				next:  offset + len(line),
			})
		}
		offset += len(line)
	}
	return out
}

// hashLevel is a line's heading depth, or zero when the line is not a heading.
func hashLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// setextLine finds an underline that turns the line above it into a heading: a
// run of "=" or "-" alone under a line of prose. Renderers read those as level 1
// and level 2, which is a section the model may not open.
func setextLine(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		line, above := strings.TrimSpace(lines[i]), strings.TrimSpace(lines[i-1])
		if !isUnderline(line) || above == "" || hashLevel(above) > 0 {
			continue
		}
		return line, true
	}
	return "", false
}

// isUnderline reports whether the line is nothing but "=" or nothing but "-".
func isUnderline(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	if c != '=' && c != '-' {
		return false
	}
	return strings.Count(line, string(c)) == len(line)
}
