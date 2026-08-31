package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openaiTimeout    = 120 * time.Second
	openaiAttempts   = 3
	openaiBaseDelay  = 500 * time.Millisecond
	maxResponseBytes = 8 << 20
)

// openaiProvider speaks the OpenAI chat-completions wire format against any base
// URL. Written on net/http so a new preset never drags in another SDK.
type openaiProvider struct {
	name       string
	model      string
	baseURL    string
	apiKey     string
	headers    map[string]string
	http       *http.Client
	retryDelay time.Duration
}

func newOpenAICompatible(p Preset, model, key, baseURL string) *openaiProvider {
	return &openaiProvider{
		name:       p.Name,
		model:      model,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     key,
		headers:    p.ExtraHeaders,
		http:       &http.Client{Timeout: openaiTimeout},
		retryDelay: openaiBaseDelay,
	}
}

func (o *openaiProvider) Name() string  { return o.name }
func (o *openaiProvider) Model() string { return o.model }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type chatResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema chatJSONSchema `json:"json_schema"`
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	MaxTokens      int                 `json:"max_tokens"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (o *openaiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	chat, err := o.buildRequest(req)
	if err != nil {
		return Response{}, err
	}
	body, err := json.Marshal(chat)
	if err != nil {
		return Response{}, fmt.Errorf("%s: encoding request: %w", o.name, err)
	}
	url := o.baseURL + "/chat/completions"

	var lastErr error
	for attempt := 1; attempt <= openaiAttempts; attempt++ {
		if attempt > 1 {
			if err := sleepCtx(ctx, o.retryDelay<<(attempt-2)); err != nil {
				return Response{}, fmt.Errorf("%s: %w (last error: %v)", o.name, err, lastErr)
			}
		}
		resp, retryable, err := o.attempt(ctx, url, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !retryable {
			return Response{}, err
		}
	}
	return Response{}, fmt.Errorf("%s: giving up after %d attempts: %w", o.name, openaiAttempts, lastErr)
}

func (o *openaiProvider) buildRequest(req Request) (chatRequest, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	out := chatRequest{Model: o.model, MaxTokens: maxTokens}

	system := req.System
	if len(req.JSONSchema) > 0 {
		name := req.SchemaName
		if name == "" {
			name = "response"
		}
		// strict:true rejects range and length keywords, so only the stripped
		// schema goes on the wire.
		wire, err := StripUnsupportedKeywords(req.JSONSchema)
		if err != nil {
			return chatRequest{}, fmt.Errorf("%s: %w", o.name, err)
		}
		out.ResponseFormat = &chatResponseFormat{
			Type:       "json_schema",
			JSONSchema: chatJSONSchema{Name: name, Schema: wire, Strict: true},
		}
		// Several providers accept response_format and ignore it, so the full
		// schema is restated in the prompt where the ranges still read.
		system = withSchemaPrompt(system, req.JSONSchema)
	}
	if system != "" {
		out.Messages = append(out.Messages, chatMessage{Role: "system", Content: system})
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == RoleAssistant {
			role = "assistant"
		}
		out.Messages = append(out.Messages, chatMessage{Role: role, Content: m.Content})
	}
	return out, nil
}

// attempt performs one HTTP round trip. The bool reports whether a retry is worth it.
func (o *openaiProvider) attempt(ctx context.Context, url string, body []byte) (Response, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, false, fmt.Errorf("%s: building request for %s: %w", o.name, url, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	for k, v := range o.headers {
		httpReq.Header.Set(k, v)
	}

	res, err := o.http.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, false, fmt.Errorf("%s: calling %s: %w", o.name, url, err)
		}
		return Response{}, true, fmt.Errorf("%s: calling %s: %w", o.name, url, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return Response{}, true, fmt.Errorf("%s: reading response from %s: %w", o.name, url, err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		retryable := res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500
		return Response{}, retryable, fmt.Errorf("%s: %s returned %s: %s", o.name, url, res.Status, excerpt(raw))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, false, fmt.Errorf("%s: decoding response from %s: %w (body: %s)", o.name, url, err, excerpt(raw))
	}
	if parsed.Error != nil {
		return Response{}, false, fmt.Errorf("%s: %s reported %s: %s", o.name, url, parsed.Error.Type, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, false, fmt.Errorf("%s: %s returned no choices (body: %s)", o.name, url, excerpt(raw))
	}

	model := parsed.Model
	if model == "" {
		model = o.model
	}
	choice := parsed.Choices[0]
	out := Response{
		Text:         choice.Message.Content,
		Model:        model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		StopReason:   choice.FinishReason,
	}
	if out.Text == "" {
		return out, false, fmt.Errorf("%s: %s returned no text (finish reason %q)", o.name, model, out.StopReason)
	}
	return out, false, nil
}

// withSchemaPrompt appends the schema to the system prompt for providers that
// silently drop response_format.
func withSchemaPrompt(system string, schema json.RawMessage) string {
	instruction := "Reply with a single JSON object that validates against this JSON Schema. " +
		"Output JSON only, with no prose and no markdown fences.\n" + string(schema)
	if system == "" {
		return instruction
	}
	return system + "\n\n" + instruction
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
