package llm

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStripUnsupportedKeywords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "nested object properties",
			in:   `{"type":"object","required":["call"],"additionalProperties":false,"properties":{"call":{"type":"object","additionalProperties":false,"properties":{"note":{"type":"string","minLength":1,"maxLength":80,"description":"why"}}}}}`,
			want: `{"type":"object","required":["call"],"additionalProperties":false,"properties":{"call":{"type":"object","additionalProperties":false,"properties":{"note":{"type":"string","description":"why"}}}}}`,
		},
		{
			name: "array items and bounds",
			in:   `{"type":"array","maxItems":12,"minItems":1,"uniqueItems":true,"items":{"type":"string","minLength":1,"pattern":"^[A-Z]+$"}}`,
			want: `{"type":"array","items":{"type":"string"}}`,
		},
		{
			name: "anyOf branches",
			in:   `{"anyOf":[{"type":"number","minimum":0,"maximum":5},{"type":"null"}]}`,
			want: `{"anyOf":[{"type":"number"},{"type":"null"}]}`,
		},
		{
			name: "keeps enum required additionalProperties and type arrays",
			in:   `{"type":"object","required":["bias","pct"],"additionalProperties":false,"properties":{"bias":{"type":"string","enum":["bullish","bearish"]},"pct":{"type":["number","null"],"minimum":0}}}`,
			want: `{"type":"object","required":["bias","pct"],"additionalProperties":false,"properties":{"bias":{"type":"string","enum":["bullish","bearish"]},"pct":{"type":["number","null"]}}}`,
		},
		{
			name: "numeric and default keywords at the root",
			in:   `{"type":"integer","exclusiveMinimum":0,"exclusiveMaximum":10,"multipleOf":2,"default":4,"minProperties":1,"maxProperties":3,"format":"int64"}`,
			want: `{"type":"integer"}`,
		},
		{
			name: "a property named minimum is a name not a keyword",
			in:   `{"type":"object","properties":{"minimum":{"type":"number","minimum":0},"pattern":{"type":"string","minLength":2}}}`,
			want: `{"type":"object","properties":{"minimum":{"type":"number"},"pattern":{"type":"string"}}}`,
		},
		{
			name: "defs and refs survive",
			in:   `{"$defs":{"leg":{"type":"object","properties":{"n":{"type":"number","minimum":1}}}},"$ref":"#/$defs/leg"}`,
			want: `{"$defs":{"leg":{"type":"object","properties":{"n":{"type":"number"}}}},"$ref":"#/$defs/leg"}`,
		},
		{
			name: "enum values are literal data",
			in:   `{"type":"object","enum":[{"minimum":3}],"minProperties":1}`,
			want: `{"type":"object","enum":[{"minimum":3}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StripUnsupportedKeywords(json.RawMessage(tc.in))
			if err != nil {
				t.Fatalf("StripUnsupportedKeywords: %v", err)
			}
			assertSameJSON(t, got, tc.want)
		})
	}
}

func TestStripUnsupportedKeywordsRejectsBadJSON(t *testing.T) {
	if _, err := StripUnsupportedKeywords(json.RawMessage(`{"type":`)); err == nil {
		t.Fatal("want an error on malformed json")
	}
}

func TestNullableToAnyOf(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "nullable number becomes anyOf",
			in:   `{"type":"object","properties":{"pct":{"type":["number","null"],"description":"percent"}}}`,
			want: `{"type":"object","properties":{"pct":{"description":"percent","anyOf":[{"type":"number"},{"type":"null"}]}}}`,
		},
		{
			name: "single type is untouched",
			in:   `{"type":"string","description":"d"}`,
			want: `{"type":"string","description":"d"}`,
		},
		{
			name: "two non-null types are left alone",
			in:   `{"type":["number","string"]}`,
			want: `{"type":["number","string"]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteSchema(json.RawMessage(tc.in), nullableToAnyOf)
			if err != nil {
				t.Fatalf("rewriteSchema: %v", err)
			}
			assertSameJSON(t, got, tc.want)
		})
	}
}

// assertSameJSON compares two schema documents by value, since the walker does
// not preserve key order.
func assertSameJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotDoc, wantDoc any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		t.Fatalf("result is not json: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantDoc); err != nil {
		t.Fatalf("want is not json: %v", err)
	}
	if !reflect.DeepEqual(gotDoc, wantDoc) {
		t.Errorf("schema =\n%s\nwant\n%s", got, want)
	}
}
