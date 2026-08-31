package playbook

import (
	"reflect"
	"testing"
)

func TestSetupIDsOfTheDefaultTemplate(t *testing.T) {
	got := SetupIDs(DefaultTemplate)
	want := []string{"M1", "M2", "R1", "N1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SetupIDs = %v, want %v in document order", got, want)
	}
}

// N1 is a no-trade condition. It is a citable heading and a real rule, but a
// proposal that cites it is arguing for the trade the rule forbids.
func TestEntrySetupIDsDropTheNoTradeRules(t *testing.T) {
	got := EntrySetupIDs(DefaultTemplate)
	want := []string{"M1", "M2", "R1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EntrySetupIDs = %v, want %v", got, want)
	}
	if len(EntrySetupIDs("### N1 no-trade\n### N2 also no\n")) != 0 {
		t.Error("a playbook of nothing but no-trade rules offers no entry setups")
	}
	if got := EntrySetupIDs("### N1 no-trade\n### M1 gap\n"); !reflect.DeepEqual(got, []string{"M1"}) {
		t.Errorf("EntrySetupIDs = %v, want [M1]", got)
	}
}

func TestSetupTitles(t *testing.T) {
	got := SetupTitles(DefaultTemplate)
	if got["M2"] != "M2 momentum continuation above prior high" {
		t.Errorf("SetupTitles[M2] = %q", got["M2"])
	}
	if len(got) != 4 {
		t.Errorf("SetupTitles has %d entries, want one per setup", len(got))
	}

	// A heading's spacing is normalised, the first of a repeated id wins, and a
	// playbook that defines nothing maps nothing.
	repeated := SetupTitles("###   M1   gap and go\n### M1 something else\n")
	if repeated["M1"] != "M1 gap and go" {
		t.Errorf("SetupTitles[M1] = %q", repeated["M1"])
	}
	if len(SetupTitles("# Playbook\n")) != 0 {
		t.Error("a playbook with no setups maps nothing")
	}
}

func TestSetupIDs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"an empty playbook has no setups", "", nil},
		{"prose alone has no setups", "# Playbook\n\nTrade well.\n", nil},
		{"a heading with only an id", "### M1\n", []string{"M1"}},
		{"a two-digit id", "### M12 the long one\n", []string{"M12"}},
		{"document order is kept", "### R1 edge\n### M1 gap\n", []string{"R1", "M1"}},
		{"a repeated id appears once", "### M1 gap\n### M1 gap again\n", []string{"M1"}},
		{"an indented heading still counts", "  ### M1 gap\n", []string{"M1"}},

		// Malformed headings: the id has to be the first word of a level-three
		// heading, or the model has nothing stable to cite.
		{"no space after the hashes", "###M1 gap\n", nil},
		{"a level-two heading", "## M1 gap\n", nil},
		{"a level-four heading", "#### M1 gap\n", nil},
		{"the id is not the first word", "### Setup M1 gap\n", nil},
		{"lowercase", "### m1 gap\n", nil},
		{"no digits", "### MOMENTUM gap\n", nil},
		{"digits first", "### 1M gap\n", nil},
		{"two letters", "### MM1 gap\n", nil},
		{"an empty heading", "### \n", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SetupIDs(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SetupIDs(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
