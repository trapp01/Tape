package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestPresetsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets() {
		if p.Name == "" {
			t.Fatal("preset with no name")
		}
		if seen[p.Name] {
			t.Errorf("duplicate preset %q", p.Name)
		}
		seen[p.Name] = true
		if p.Docs == "" {
			t.Errorf("preset %q has no docs link", p.Name)
		}
		// ollama is local and claude-code runs a local binary, so neither needs a key.
		if p.KeyEnv == "" && p.Name != "ollama" && p.Name != ProviderClaudeCode {
			t.Errorf("preset %q has no key env var", p.Name)
		}
		if p.BaseURL == "" && p.Name != ProviderOpenAICompatible && p.Name != ProviderClaudeCode {
			t.Errorf("preset %q has no base url", p.Name)
		}
	}
	for _, want := range []string{ProviderAnthropic, ProviderClaudeCode, "openrouter", "zai", "deepseek", "openai", "groq", "ollama", ProviderOpenAICompatible} {
		if !seen[want] {
			t.Errorf("preset %q is missing", want)
		}
	}
}

func TestPresetOrderPutsAnthropicThenClaudeCodeFirst(t *testing.T) {
	names := PresetNames()
	if names[0] != ProviderAnthropic || names[1] != ProviderClaudeCode {
		t.Errorf("order starts %v, want anthropic then claude-code", names[:2])
	}
}

func TestNewClaudeCodeNeedsNoKeyOrBaseURL(t *testing.T) {
	prov, err := New(Config{Provider: ProviderClaudeCode})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cc, ok := prov.(*claudeCodeProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *claudeCodeProvider", prov)
	}
	if cc.Model() != "opus" {
		t.Errorf("Model() = %q, want the opus default", cc.Model())
	}
}

func TestPresetsCopyIsIndependent(t *testing.T) {
	first := Presets()
	for i := range first {
		if first[i].ExtraHeaders != nil {
			first[i].ExtraHeaders["X-Title"] = "mutated"
		}
	}
	for _, p := range Presets() {
		if p.ExtraHeaders["X-Title"] == "mutated" {
			t.Fatalf("preset %q leaked its header map", p.Name)
		}
	}
}

func TestNewResolvesEveryPreset(t *testing.T) {
	for _, p := range Presets() {
		cfg := Config{Provider: p.Name, Model: "test-model", APIKey: "test-key"}
		if p.Name == ProviderOpenAICompatible {
			cfg.BaseURL = "https://example.invalid/v1"
		}
		prov, err := New(cfg)
		if err != nil {
			t.Errorf("New(%q): %v", p.Name, err)
			continue
		}
		if prov.Name() != p.Name {
			t.Errorf("Name() = %q, want %q", prov.Name(), p.Name)
		}
		if prov.Model() != "test-model" {
			t.Errorf("Model() = %q, want test-model", prov.Model())
		}
	}
}

func TestNewDefaultsToAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	prov, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if prov.Name() != ProviderAnthropic {
		t.Errorf("Name() = %q, want anthropic", prov.Name())
	}
	if prov.Model() != "claude-opus-5" {
		t.Errorf("Model() = %q, want claude-opus-5", prov.Model())
	}
}

func TestNewUnknownProviderListsValidNames(t *testing.T) {
	_, err := New(Config{Provider: "gpt-whatever"})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
	for _, name := range PresetNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not list %q: %v", name, err)
		}
	}
}

func TestNewMissingKeyNamesEnvVar(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	_, err := New(Config{Provider: "groq", Model: "test-model"})
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
	if !strings.Contains(err.Error(), "GROQ_API_KEY") {
		t.Errorf("error should name the env var: %v", err)
	}
}

func TestNewReadsKeyFromEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	prov, err := New(Config{Provider: "DeepSeek"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	oai, ok := prov.(*openaiProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *openaiProvider", prov)
	}
	if oai.apiKey != "env-key" {
		t.Errorf("apiKey = %q, want env-key", oai.apiKey)
	}
	if oai.model != "deepseek-chat" {
		t.Errorf("model = %q, want the preset default", oai.model)
	}
}

func TestNewOllamaNeedsNoKey(t *testing.T) {
	prov, err := New(Config{Provider: "ollama", Model: "llama3.2"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	oai, ok := prov.(*openaiProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *openaiProvider", prov)
	}
	if oai.apiKey != "" {
		t.Errorf("apiKey = %q, want empty", oai.apiKey)
	}
	if oai.baseURL != "http://localhost:11434/v1" {
		t.Errorf("baseURL = %q", oai.baseURL)
	}
}

func TestNewOpenAICompatibleNeedsBaseURL(t *testing.T) {
	_, err := New(Config{Provider: ProviderOpenAICompatible, Model: "m", APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("err = %v, want a base_url complaint", err)
	}
}

func TestNewNeedsAModelWithoutAPresetDefault(t *testing.T) {
	_, err := New(Config{Provider: "openai", APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "llm.model") {
		t.Fatalf("err = %v, want a model complaint", err)
	}
}

func TestNewConfigKeyBeatsEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	prov, err := New(Config{Provider: "openai", Model: "m", APIKey: "config-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if prov.(*openaiProvider).apiKey != "config-key" {
		t.Error("config key should win over the env var")
	}
}

func TestNewBaseURLOverridesPreset(t *testing.T) {
	prov, err := New(Config{Provider: "ollama", Model: "m", BaseURL: "http://box.local:11434/v1/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := prov.(*openaiProvider).baseURL; got != "http://box.local:11434/v1" {
		t.Errorf("baseURL = %q, want the override without a trailing slash", got)
	}
}
