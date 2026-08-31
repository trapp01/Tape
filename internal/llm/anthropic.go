package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicProvider talks to the Claude Messages API through the official SDK.
// It uses the beta namespace because server-side refusal fallbacks live there.
type anthropicProvider struct {
	client anthropic.Client
	name   string
	model  string
}

func newAnthropic(p Preset, model, key, baseURL string) *anthropicProvider {
	opts := []option.RequestOption{option.WithBaseURL(baseURL)}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	return &anthropicProvider{client: anthropic.NewClient(opts...), name: p.Name, model: model}
}

func (a *anthropicProvider) Name() string  { return a.name }
func (a *anthropicProvider) Model() string { return a.model }

func (a *anthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	params, err := a.params(req)
	if err != nil {
		return Response{}, err
	}

	msg, err := a.client.Beta.Messages.New(ctx, params)
	if err != nil && rejectsFallbacks(err) {
		// Not every Claude model accepts the fallbacks beta; retry without it.
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{}
		params.Betas = nil
		msg, err = a.client.Beta.Messages.New(ctx, params)
	}
	if err != nil {
		return Response{}, fmt.Errorf("%s: completing with %s: %w", a.name, a.model, err)
	}

	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	// msg.Model names the model that produced the reply, which a fallback can change.
	out := Response{
		Text:         text.String(),
		Model:        string(msg.Model),
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
		StopReason:   string(msg.StopReason),
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal && out.Text == "" {
		return out, fmt.Errorf("%s: %s refused the request (category %q): %s",
			a.name, out.Model, msg.StopDetails.Category, msg.StopDetails.Explanation)
	}
	if out.Text == "" {
		return out, fmt.Errorf("%s: %s returned no text (stop reason %q)", a.name, out.Model, out.StopReason)
	}
	return out, nil
}

func (a *anthropicProvider) params(req Request) (anthropic.BetaMessageNewParams, error) {
	if len(req.Messages) == 0 {
		return anthropic.BetaMessageNewParams{}, fmt.Errorf("%s: request has no messages", a.name)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	messages := make([]anthropic.BetaMessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		block := anthropic.NewBetaTextBlock(m.Content)
		role := anthropic.BetaMessageParamRoleUser
		if m.Role == RoleAssistant {
			role = anthropic.BetaMessageParamRoleAssistant
		}
		messages = append(messages, anthropic.BetaMessageParam{
			Role:    role,
			Content: []anthropic.BetaContentBlockParamUnion{block},
		})
	}

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
		// "default" lets the server pick the substitute model by refusal category.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}
	if req.System != "" {
		params.System = []anthropic.BetaTextBlockParam{{Text: req.System}}
	}
	// Temperature is rejected on current models, so it is never sent.
	if len(req.JSONSchema) > 0 {
		schema, err := outputSchema(req.JSONSchema)
		if err != nil {
			return anthropic.BetaMessageNewParams{}, fmt.Errorf("%s: %w", a.name, err)
		}
		params.OutputConfig = anthropic.BetaOutputConfigParam{
			Format: anthropic.BetaJSONSchemaOutputFormat(schema),
		}
	}
	return params, nil
}

// outputSchema puts a schema in the shape structured outputs accepts: no range
// or length keywords, and nullable fields as anyOf rather than a type array.
// The SDK's own transform drops a schema entirely when it meets a type array.
func outputSchema(raw json.RawMessage) (map[string]any, error) {
	stripped, err := StripUnsupportedKeywords(raw)
	if err != nil {
		return nil, err
	}
	expanded, err := rewriteSchema(stripped, nullableToAnyOf)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(expanded, &m); err != nil {
		return nil, fmt.Errorf("decoding json schema: %w", err)
	}
	return m, nil
}

// rejectsFallbacks reports whether err is the API refusing the fallbacks parameter
// rather than a real failure of the request.
func rejectsFallbacks(err error) bool {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.RawJSON()), "fallback")
}
