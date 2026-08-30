package llm

import (
	"fmt"
	"strings"
)

// newProvider resolves cfg into a live client. An empty provider means anthropic.
func newProvider(cfg Config) (Provider, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if name == "" {
		name = ProviderAnthropic
	}
	preset, ok := FindPreset(name)
	if !ok {
		return nil, fmt.Errorf("%w %q (valid: %s)", ErrUnknownProvider, cfg.Provider, strings.Join(PresetNames(), ", "))
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = preset.DefaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("provider %q has no default model: set llm.model in config (see %s)", preset.Name, preset.Docs)
	}

	// Claude Code is a local binary, so it needs neither a base URL nor a key.
	if preset.Name == ProviderClaudeCode {
		return newClaudeCode(preset, model), nil
	}

	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = preset.BaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("provider %q needs llm.base_url set in config", preset.Name)
	}

	key, err := resolveKey(preset, cfg.APIKey)
	if err != nil {
		return nil, err
	}

	if preset.Name == ProviderAnthropic {
		return newAnthropic(preset, model, key, baseURL), nil
	}
	return newOpenAICompatible(preset, model, key, baseURL), nil
}
