// Package playbook reads the user's strategy file. The model applies these rules
// and cites them; nothing else steers it.
package playbook

import (
	"errors"
	"fmt"
	"os"
)

// ErrMissing means no playbook exists yet; `tape init` writes the default.
var ErrMissing = errors.New("playbook: file not found")

// Load returns the playbook text at path.
func Load(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w at %s", ErrMissing, path)
	}
	if err != nil {
		return "", fmt.Errorf("playbook: reading %s: %w", path, err)
	}
	return string(raw), nil
}

// WriteDefault creates the seed playbook at path. It refuses to overwrite.
func WriteDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("playbook: %s already exists", path)
	}
	if err := os.WriteFile(path, []byte(DefaultTemplate), 0o600); err != nil {
		return fmt.Errorf("playbook: writing %s: %w", path, err)
	}
	return nil
}
