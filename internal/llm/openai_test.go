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

const okChatBody = `{
  "model": "glm-4.6",
  "choices": [{"index": 0, "message": {"role": "assistant", "content": "pong"}, "finish_reason": "stop"}],
  "usage": {"prompt_tokens": 11, "completion_tokens": 2}
}`

func newTestOpenAI(t *testing.T, p Preset, handler http.HandlerFunc) *openaiProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prov := newOpenAICompatible(p, "glm-4.6", "secret-key", srv.URL+"/v1")
	prov.retryDelay = 0
	return prov
}

func TestOpenAICompatibleCompleteSendsExpectedRequest(t *testing.T) {
	preset, ok := FindPreset("openrouter")
	if !ok {
		t.Fatal("openrouter preset missing")
	}

	var gotPath, gotAuth, gotReferer, gotTitle string
	var gotBody map[string]any
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request body is not json: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, okChatBody)
	})

	resp, err := prov.Complete(context.Background(), Request{
		System:    "be terse",
		Messages:  []Message{{Role: RoleUser, Content: "ping"}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
	if gotReferer != "https://github.com/trapp01/tape" {
		t.Errorf("HTTP-Referer = %q", gotReferer)
	}
	if gotTitle != "tape" {
		t.Errorf("X-Title = %q", gotTitle)
	}
	if gotBody["model"] != "glm-4.6" {
		t.Errorf("model = %v, want glm-4.6", gotBody["model"])
	}
	if gotBody["max_tokens"] != float64(32) {
		t.Errorf("max_tokens = %v, want 32", gotBody["max_tokens"])
	}
	if _, ok := gotBody["response_format"]; ok {
		t.Error("response_format sent without a schema")
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v, want 2 entries", msgs)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be terse" {
		t.Errorf("first message = %v, want the system prompt", first)
	}

	if resp.Text != "pong" {
		t.Errorf("Text = %q, want pong", resp.Text)
	}
	if resp.Model != "glm-4.6" {
		t.Errorf("Model = %q", resp.Model)
	}
	if resp.InputTokens != 11 || resp.OutputTokens != 2 {
		t.Errorf("usage = %d/%d, want 11/2", resp.InputTokens, resp.OutputTokens)
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", resp.StopReason)
	}
}

func TestOpenAICompatibleSchemaSetsResponseFormatAndPrompt(t *testing.T) {
	preset, _ := FindPreset("deepseek")
	schema := json.RawMessage(`{"type":"object","properties":{"side":{"type":"string"}}}`)

	var gotBody map[string]any
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		io.WriteString(w, okChatBody)
	})

	if _, err := prov.Complete(context.Background(), Request{
		System:     "be terse",
		Messages:   []Message{{Role: RoleUser, Content: "call it"}},
		JSONSchema: schema,
		SchemaName: "trade_call",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rf, ok := gotBody["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format missing from %v", gotBody)
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v", rf["type"])
	}
	js, _ := rf["json_schema"].(map[string]any)
	if js["name"] != "trade_call" {
		t.Errorf("json_schema.name = %v, want trade_call", js["name"])
	}
	if js["strict"] != true {
		t.Errorf("json_schema.strict = %v, want true", js["strict"])
	}
	if _, ok := js["schema"].(map[string]any); !ok {
		t.Errorf("json_schema.schema = %v, want the schema object", js["schema"])
	}

	msgs, _ := gotBody["messages"].([]any)
	system, _ := msgs[0].(map[string]any)
	content, _ := system["content"].(string)
	if !strings.Contains(content, "be terse") || !strings.Contains(content, `"side"`) {
		t.Errorf("system prompt does not restate the schema: %q", content)
	}
}

// constrainedSchema carries every keyword strict structured outputs rejects.
const constrainedSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["market_read", "call", "risks"],
  "properties": {
    "market_read": {"type": "string", "minLength": 1, "description": "the tape"},
    "call": {
      "type": "object",
      "additionalProperties": false,
      "required": ["direction", "threshold_pct"],
      "properties": {
        "direction": {"type": "string", "enum": ["up", "down", "flat"]},
        "threshold_pct": {"type": ["number", "null"], "minimum": 0, "maximum": 5}
      }
    },
    "risks": {"type": "array", "maxItems": 5, "items": {"type": "string", "minLength": 1}}
  }
}`

func TestOpenAICompatibleStripsUnsupportedSchemaKeywords(t *testing.T) {
	preset, _ := FindPreset("openai")

	var gotBody map[string]any
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		io.WriteString(w, okChatBody)
	})

	if _, err := prov.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "brief me"}},
		JSONSchema: json.RawMessage(constrainedSchema),
		SchemaName: "brief",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rf, _ := gotBody["response_format"].(map[string]any)
	js, _ := rf["json_schema"].(map[string]any)
	schema, ok := js["schema"].(map[string]any)
	if !ok {
		t.Fatalf("json_schema.schema = %v, want an object", js["schema"])
	}

	sent, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("re-encoding the sent schema: %v", err)
	}
	for _, kw := range []string{"minLength", "maximum", "minimum", "maxItems"} {
		if strings.Contains(string(sent), `"`+kw+`"`) {
			t.Errorf("%s survived into response_format: %s", kw, sent)
		}
	}

	if schema["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", schema["additionalProperties"])
	}
	required, _ := schema["required"].([]any)
	if len(required) != 3 {
		t.Errorf("required = %v, want three entries", schema["required"])
	}
	props, _ := schema["properties"].(map[string]any)
	call, _ := props["call"].(map[string]any)
	callProps, _ := call["properties"].(map[string]any)
	pct, _ := callProps["threshold_pct"].(map[string]any)
	types, _ := pct["type"].([]any)
	if len(types) != 2 || types[0] != "number" || types[1] != "null" {
		t.Errorf("threshold_pct.type = %v, want [number null]", pct["type"])
	}
	direction, _ := callProps["direction"].(map[string]any)
	if enum, _ := direction["enum"].([]any); len(enum) != 3 {
		t.Errorf("direction.enum = %v, want three values", direction["enum"])
	}

	// The prompt still restates the original, ranges and all.
	msgs, _ := gotBody["messages"].([]any)
	system, _ := msgs[0].(map[string]any)
	content, _ := system["content"].(string)
	if !strings.Contains(content, "minLength") || !strings.Contains(content, "maxItems") {
		t.Errorf("system prompt should restate the original schema: %q", content)
	}
}

func TestOpenAICompatibleRetriesServerError(t *testing.T) {
	preset, _ := FindPreset("openai")
	calls := 0
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"upstream boom"}}`)
			return
		}
		io.WriteString(w, okChatBody)
	})

	resp, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if resp.Text != "pong" {
		t.Errorf("Text = %q, want pong", resp.Text)
	}
}

func TestOpenAICompatibleDoesNotRetryClientError(t *testing.T) {
	preset, _ := FindPreset("openai")
	calls := 0
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"bad key"}}`)
	})

	_, err := prov.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	if err == nil {
		t.Fatal("want an error on 401")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("error should name the status and body excerpt: %v", err)
	}
}

func TestPingReportsReply(t *testing.T) {
	preset, _ := FindPreset("groq")
	prov := newTestOpenAI(t, preset, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, okChatBody)
	})

	res, err := Ping(context.Background(), prov)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if res.Reply != "pong" {
		t.Errorf("Reply = %q, want pong", res.Reply)
	}
	if res.Model != "glm-4.6" {
		t.Errorf("Model = %q", res.Model)
	}
	if res.Latency <= 0 {
		t.Error("Latency should be measured")
	}
}
