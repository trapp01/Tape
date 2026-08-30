package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "bare object", in: `{"side":"long"}`, want: `{"side":"long"}`},
		{name: "surrounding space", in: "  \n{\"side\":\"long\"}\n ", want: `{"side":"long"}`},
		{name: "bare array", in: `[1,2,3]`, want: `[1,2,3]`},
		{
			name: "json fence",
			in:   "Here you go:\n```json\n{\"side\":\"long\"}\n```\nHope that helps.",
			want: "{\"side\":\"long\"}",
		},
		{
			name: "unlabelled fence",
			in:   "```\n{\"side\":\"short\"}\n```",
			want: "{\"side\":\"short\"}",
		},
		{
			name: "unclosed fence",
			in:   "```json\n{\"side\":\"short\"}",
			want: "{\"side\":\"short\"}",
		},
		{
			name: "leading prose",
			in:   `Sure thing. {"side":"long","size":3} — let me know.`,
			want: `{"side":"long","size":3}`,
		},
		{
			name: "nested braces",
			in:   `prose {"a":{"b":[1,{"c":2}]}} tail`,
			want: `{"a":{"b":[1,{"c":2}]}}`,
		},
		{
			name: "brace inside a string",
			in:   `text {"note":"} not the end","ok":true} done`,
			want: `{"note":"} not the end","ok":true}`,
		},
		{
			name: "escaped quote inside a string",
			in:   `x {"note":"say \"hi\" }","ok":true} y`,
			want: `{"note":"say \"hi\" }","ok":true}`,
		},
		{name: "scalar reply", in: `"pong"`, want: `"pong"`},
		{name: "empty", in: "   ", wantErr: true},
		{name: "no json", in: "I cannot help with that.", wantErr: true},
		{name: "truncated object", in: `{"side":"lo`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractJSON(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractJSON: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractJSONObject(t *testing.T) {
	if _, err := ExtractJSONObject(`{"side":"long"}`); err != nil {
		t.Errorf("object should pass: %v", err)
	}
	if _, err := ExtractJSONObject("```json\n{\"side\":\"long\"}\n```"); err != nil {
		t.Errorf("fenced object should pass: %v", err)
	}
	if _, err := ExtractJSONObject(`[1,2,3]`); err == nil {
		t.Error("an array is not an object")
	}
	if _, err := ExtractJSONObject(`"pong"`); err == nil {
		t.Error("a string is not an object")
	}
}
