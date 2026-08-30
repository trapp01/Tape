package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const okMessageBody = `{
  "id": "msg_01",
  "type": "message",
  "role": "assistant",
  "model": "claude-opus-5",
  "content": [{"type": "text", "text": "pong"}],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {"input_tokens": 12, "output_tokens": 3}
}`

func newTestAnthropic(t *testing.T, handler http.HandlerFunc) *anthropicProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	preset, ok := FindPreset(ProviderAnthropic)
	if !ok {
		t.Fatal("anthropic preset missing")
	}
	return newAnthropic(preset, "claude-opus-5", "test-key", srv.URL)
}

func TestAnthropicCompleteSendsExpectedRequest(t *testing.T) {
	var gotPath, gotBeta, gotKey string
	var gotBody map[string]any
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBeta = r.Header.Get("anthropic-beta")
		gotKey = r.Header.Get("x-api-key")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not json: %v", err)
		}
		io.WriteString(w, okMessageBody)
	})

	resp, err := prov.Complete(context.Background(), Request{
		System:   "be terse",
		Messages: []Message{{Role: RoleUser, Content: "ping"}, {Role: RoleAssistant, Content: "sure"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "test-key" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	if !strings.Contains(gotBeta, "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta = %q, want the server-side fallback beta", gotBeta)
	}
	if gotBody["model"] != "claude-opus-5" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["max_tokens"] != float64(defaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", gotBody["max_tokens"], defaultMaxTokens)
	}
	if gotBody["fallbacks"] != "default" {
		t.Errorf("fallbacks = %v, want \"default\"", gotBody["fallbacks"])
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Error("temperature must not be sent")
	}
	system, _ := gotBody["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v, want one block", gotBody["system"])
	}
	if block, _ := system[0].(map[string]any); block["text"] != "be terse" {
		t.Errorf("system block = %v", system[0])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v, want 2 entries", msgs)
	}
	if first, _ := msgs[0].(map[string]any); first["role"] != "user" {
		t.Errorf("messages[0].role = %v, want user", first["role"])
	}
	if second, _ := msgs[1].(map[string]any); second["role"] != "assistant" {
		t.Errorf("messages[1].role = %v, want assistant", second["role"])
	}

	if resp.Text != "pong" {
		t.Errorf("Text = %q, want pong", resp.Text)
	}
	if resp.Model != "claude-opus-5" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 3 {
		t.Errorf("usage = %d/%d, want 12/3", resp.InputTokens, resp.OutputTokens)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
}

func TestAnthropicSchemaSetsOutputConfig(t *testing.T) {
	var gotBody map[string]any
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		io.WriteString(w, okMessageBody)
	})

	_, err := prov.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "call it"}},
		JSONSchema: json.RawMessage(`{"type":"object","properties":{"side":{"type":"string"}},"additionalProperties":false}`),
		SchemaName: "trade_call",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	cfg, ok := gotBody["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("output_config missing from %v", gotBody)
	}
	format, _ := cfg["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Errorf("output_config.format.type = %v", format["type"])
	}
	schema, _ := format["schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("output_config.format.schema = %v", format["schema"])
	}
}

func TestAnthropicRefusalWithoutContentErrors(t *testing.T) {
	const refusal = `{
	  "id": "msg_02", "type": "message", "role": "assistant", "model": "claude-opus-5",
	  "content": [],
	  "stop_reason": "refusal",
	  "stop_details": {"type": "refusal", "category": "cyber", "explanation": "declined"},
	  "usage": {"input_tokens": 9, "output_tokens": 0}
	}`
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, refusal)
	})

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error when a refusal has no content")
	}
	if resp.StopReason != "refusal" {
		t.Errorf("StopReason = %q, want refusal", resp.StopReason)
	}
	if !strings.Contains(err.Error(), "cyber") || !strings.Contains(err.Error(), "declined") {
		t.Errorf("error should carry the refusal details: %v", err)
	}
}

func TestAnthropicRetriesWithoutFallbacksOn400(t *testing.T) {
	var bodies []map[string]any
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"fallbacks is not supported for this model"}}`)
			return
		}
		io.WriteString(w, okMessageBody)
	})

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	if bodies[0]["fallbacks"] != "default" {
		t.Errorf("first request fallbacks = %v, want \"default\"", bodies[0]["fallbacks"])
	}
	if _, ok := bodies[1]["fallbacks"]; ok {
		t.Errorf("retry still sent fallbacks: %v", bodies[1]["fallbacks"])
	}
	if resp.Text != "pong" {
		t.Errorf("Text = %q, want pong", resp.Text)
	}
}

func TestAnthropicOtherErrorsAreNotRetried(t *testing.T) {
	calls := 0
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`)
	})

	if _, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}}); err == nil {
		t.Fatal("want an error on 400")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestAnthropicRejectsEmptyMessages(t *testing.T) {
	prov := newTestAnthropic(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be sent")
	})
	if _, err := prov.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("want an error when there are no messages")
	}
}
