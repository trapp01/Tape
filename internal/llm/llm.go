// Package llm is the model-agnostic completion contract. Anthropic has a native
// client; everything else (OpenRouter, Z.ai/GLM, DeepSeek, OpenAI, Groq, Ollama)
// goes through one OpenAI-compatible client with a preset base URL.
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrUnknownProvider = errors.New("unknown llm provider")
	ErrMissingAPIKey   = errors.New("missing llm api key")
)

// defaultMaxTokens bounds a reply when Request.MaxTokens is zero.
const defaultMaxTokens = 8192

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	System   string
	Messages []Message
	// MaxTokens bounds the reply. Zero means the provider default.
	MaxTokens int
	// JSONSchema, when set, asks the provider for a reply that validates against it.
	// Providers without native structured output fall back to instructing the model
	// and validating the parsed JSON.
	JSONSchema json.RawMessage
	SchemaName string
}

type Response struct {
	Text         string
	Model        string
	InputTokens  int
	OutputTokens int
	StopReason   string
	// CostUSD is the provider's own estimate of what the call cost, when it reports
	// one. Nil everywhere else.
	CostUSD *float64
}

// Provider is one configured model behind one API.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// Config is what the user writes under [llm] in config.toml, resolved against
// environment variables for the key.
type Config struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

// New builds a Provider from cfg. Implemented in factory.go.
func New(cfg Config) (Provider, error) {
	return newProvider(cfg)
}
