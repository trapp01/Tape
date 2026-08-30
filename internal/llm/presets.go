package llm

import (
	"fmt"
	"maps"
	"os"
	"strings"
)

// Provider names that get special handling in the factory.
const (
	ProviderAnthropic        = "anthropic"
	ProviderClaudeCode       = "claude-code"
	ProviderOpenAICompatible = "openai-compatible"
)

// Preset is a known endpoint: everything needed to reach a provider except the key.
// ExtraHeaders are sent on every OpenAI-compatible request.
type Preset struct {
	Name         string
	BaseURL      string
	KeyEnv       string
	DefaultModel string
	Docs         string
	ExtraHeaders map[string]string
}

// presets is the stable listing order for `tape llm providers`: native first, then
// the OpenAI-compatible endpoints, then the bring-your-own-URL escape hatch.
var presets = []Preset{
	{
		Name:         ProviderAnthropic,
		BaseURL:      "https://api.anthropic.com",
		KeyEnv:       "ANTHROPIC_API_KEY",
		DefaultModel: "claude-opus-5",
		Docs:         "https://platform.claude.com/docs/en/api/messages",
	},
	{
		// No BaseURL and no key: this shells out to the locally installed CLI and
		// bills the machine owner's own subscription.
		Name:         ProviderClaudeCode,
		DefaultModel: "opus",
		Docs:         "https://code.claude.com/docs/en/headless.md (your own Claude Code login, personal use only)",
	},
	{
		Name:    "openrouter",
		BaseURL: "https://openrouter.ai/api/v1",
		KeyEnv:  "OPENROUTER_API_KEY",
		Docs:    "https://openrouter.ai/docs/quickstart",
		ExtraHeaders: map[string]string{
			"HTTP-Referer": "https://github.com/trapp01/tape",
			"X-Title":      "tape",
		},
	},
	{
		Name:    "zai",
		BaseURL: "https://api.z.ai/api/paas/v4",
		KeyEnv:  "ZAI_API_KEY",
		Docs:    "https://docs.z.ai/guides/overview/quick-start",
	},
	{
		Name:         "deepseek",
		BaseURL:      "https://api.deepseek.com",
		KeyEnv:       "DEEPSEEK_API_KEY",
		DefaultModel: "deepseek-chat",
		Docs:         "https://api-docs.deepseek.com/",
	},
	{
		Name:    "openai",
		BaseURL: "https://api.openai.com/v1",
		KeyEnv:  "OPENAI_API_KEY",
		Docs:    "https://platform.openai.com/docs/api-reference/chat",
	},
	{
		Name:    "groq",
		BaseURL: "https://api.groq.com/openai/v1",
		KeyEnv:  "GROQ_API_KEY",
		Docs:    "https://console.groq.com/docs/openai",
	},
	{
		// Local inference needs no credential; KeyEnv stays empty so none is demanded.
		Name:    "ollama",
		BaseURL: "http://localhost:11434/v1",
		Docs:    "https://docs.ollama.com/api/openai-compatibility",
	},
	{
		// BaseURL comes from llm.base_url in config.
		Name:   ProviderOpenAICompatible,
		KeyEnv: "TAPE_LLM_API_KEY",
		Docs:   "https://platform.openai.com/docs/api-reference/chat",
	},
}

// Presets returns the known providers in a stable order. The copy is deep enough
// that a caller cannot mutate the package's header maps.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	for i, p := range presets {
		if p.ExtraHeaders != nil {
			p.ExtraHeaders = maps.Clone(p.ExtraHeaders)
		}
		out[i] = p
	}
	return out
}

// PresetNames lists the accepted values for llm.provider, in listing order.
func PresetNames() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// FindPreset looks a provider up by name, case-insensitively.
func FindPreset(name string) (Preset, bool) {
	want := strings.ToLower(strings.TrimSpace(name))
	for _, p := range presets {
		if p.Name == want {
			if p.ExtraHeaders != nil {
				p.ExtraHeaders = maps.Clone(p.ExtraHeaders)
			}
			return p, true
		}
	}
	return Preset{}, false
}

// resolveKey prefers the configured key, then the preset's env var. A preset with
// no KeyEnv (ollama) needs no credential.
func resolveKey(p Preset, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	if p.KeyEnv == "" {
		return "", nil
	}
	if v := os.Getenv(p.KeyEnv); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%w for provider %q: set $%s or llm.api_key in config", ErrMissingAPIKey, p.Name, p.KeyEnv)
}
