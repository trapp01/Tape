package playbook

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDefaultThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.md")

	if _, err := Load(path); !errors.Is(err, ErrMissing) {
		t.Fatalf("Load before writing: %v, want ErrMissing", err)
	}
	if err := WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != DefaultTemplate {
		t.Error("Load returned something other than the template that was written")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestWriteDefaultRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playbook.md")
	const edited = "# My rules\n\nOnly R1, only on Fridays.\n"

	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("seeding the file: %v", err)
	}
	err := WriteDefault(path)
	if err == nil {
		t.Fatal("WriteDefault overwrote an existing playbook")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the file", err)
	}

	got, loadErr := Load(path)
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if got != edited {
		t.Error("the refused write changed the file")
	}
}

func TestDefaultTemplateCitesEverySetup(t *testing.T) {
	for _, id := range []string{"M1", "M2", "R1", "N1"} {
		if !strings.Contains(DefaultTemplate, id) {
			t.Errorf("the template never mentions setup %s", id)
		}
	}
	for _, section := range []string{
		"## Posture by regime",
		"## Setups",
		"## Risk rules",
		"## Call of the day",
	} {
		if !strings.Contains(DefaultTemplate, section) {
			t.Errorf("the template is missing the %q section", section)
		}
	}
	for _, line := range []string{"When:", "Entry:", "Invalidation:", "Target:", "Size:"} {
		if !strings.Contains(DefaultTemplate, line) {
			t.Errorf("the setups never state %q", line)
		}
	}
	for _, rule := range []string{"0.5%", "3 open positions", "averaging down", "Flat by the close", "two stopped-out losses"} {
		if !strings.Contains(DefaultTemplate, rule) {
			t.Errorf("the risk rules never state %q", rule)
		}
	}
	if !strings.HasSuffix(DefaultTemplate, "\n") {
		t.Error("the template does not end with a newline")
	}
	if n := strings.Count(DefaultTemplate, "\n"); n < 80 || n > 120 {
		t.Errorf("the template is %d lines; it should stay between 80 and 120", n)
	}
}
