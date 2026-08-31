package retro

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/trapp01/tape/internal/config"
	"github.com/trapp01/tape/internal/journal"
)

// hashConfig is what changes the meaning of a trade: the walls Go enforces, the
// costs every fill pays, the instrument and bar the call is graded on, and the
// analyst that wrote it. Never a key, a watchlist, or a lookback.
type hashConfig struct {
	Risk             config.RiskConfig  `toml:"risk"`
	Costs            config.CostsConfig `toml:"costs"`
	RegimeSymbol     string             `toml:"regime_symbol"`
	CallThresholdPct float64            `toml:"call_threshold_pct"`
	LLMProvider      string             `toml:"llm_provider"`
	LLMModel         string             `toml:"llm_model"`
}

// ConfigHash fingerprints the rules in force. A record traded under different
// numbers is a different record, so the gate starts again when this moves.
func ConfigHash(cfg config.Config) (string, error) {
	raw, err := toml.Marshal(hashConfig{
		Risk:             cfg.Risk,
		Costs:            cfg.Costs,
		RegimeSymbol:     cfg.Brief.RegimeSymbol,
		CallThresholdPct: cfg.Brief.CallThresholdPct,
		LLMProvider:      cfg.LLM.Provider,
		LLMModel:         cfg.LLM.Model,
	})
	if err != nil {
		return "", fmt.Errorf("retro: fingerprinting the config: %w", err)
	}
	return sha256Hex(raw), nil
}

// EnsureVersion snapshots the playbook whenever it or the config has moved since
// the last snapshot. Every command that reads or writes the record calls it, so a
// rule edited by hand resets the gate window exactly like one a review applied.
// The returned version is nil when nothing had changed.
func EnsureVersion(ctx context.Context, jnl VersionStore, cfg config.Config, playbookPath string) (*journal.PlaybookVersion, error) {
	if jnl == nil {
		return nil, errors.New("retro: snapshotting the playbook: no journal configured")
	}
	sum, err := fileSHA(playbookPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("retro: no strategy file at %s; run `tape playbook --write` to write one", playbookPath)
	}
	if err != nil {
		return nil, err
	}
	configHash, err := ConfigHash(cfg)
	if err != nil {
		return nil, err
	}

	latest, err := jnl.LatestPlaybookVersion(ctx)
	if errors.Is(err, journal.ErrNotFound) {
		return insertVersion(ctx, jnl, playbookPath, sum, configHash, "first snapshot")
	}
	if err != nil {
		return nil, err
	}
	note := changeNote(latest, sum, configHash)
	if note == "" {
		return nil, nil
	}
	return insertVersion(ctx, jnl, playbookPath, sum, configHash, note)
}

// changeNote names what moved since the last snapshot, and is empty when nothing did.
func changeNote(latest journal.PlaybookVersion, sum, configHash string) string {
	playbookMoved := latest.SHA256 != sum
	configMoved := latest.ConfigHash != configHash
	switch {
	case playbookMoved && configMoved:
		return "playbook and config changed outside a retro"
	case playbookMoved:
		return "playbook changed outside a retro"
	case configMoved:
		return "config changed"
	default:
		return ""
	}
}

func insertVersion(ctx context.Context, jnl VersionStore, path, sum, configHash, note string) (*journal.PlaybookVersion, error) {
	v := journal.PlaybookVersion{SHA256: sum, Path: path, Note: note, ConfigHash: configHash}
	if err := jnl.InsertPlaybookVersion(ctx, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// fileSHA fingerprints a file's contents. A missing file wraps os.ErrNotExist so
// the caller can tell it apart from an unreadable one.
func fileSHA(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("retro: reading %s: %w", path, err)
	}
	return sha256Hex(raw), nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
