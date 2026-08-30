package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// excerptLimit caps how much of a body or reply is quoted back in an error.
const excerptLimit = 512

// ExtractJSON pulls the first valid JSON value out of model output, tolerating
// ```json fences and leading prose.
func ExtractJSON(text string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, errors.New("extracting json: reply is empty")
	}
	for _, candidate := range jsonCandidates(trimmed) {
		if json.Valid([]byte(candidate)) {
			return json.RawMessage(candidate), nil
		}
	}
	return nil, fmt.Errorf("extracting json: no valid json value in reply: %s", excerpt([]byte(trimmed)))
}

// ExtractJSONObject is ExtractJSON restricted to an object, the shape every schema
// in tape asks for. It checks the shape only; schema validation is the caller's job.
func ExtractJSONObject(text string) (json.RawMessage, error) {
	raw, err := ExtractJSON(text)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("extracting json object: reply is not a json object: %s", excerpt(raw))
	}
	return raw, nil
}

// jsonCandidates lists the substrings worth trying, most literal first.
func jsonCandidates(text string) []string {
	sources := append([]string{text}, fencedBlocks(text)...)
	out := make([]string, 0, len(sources)*2)
	for _, s := range sources {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
		if span, ok := balancedSpan(s); ok && span != s {
			out = append(out, span)
		}
	}
	return out
}

// fencedBlocks returns the bodies of ``` blocks, dropping the language tag line.
func fencedBlocks(text string) []string {
	var blocks []string
	rest := text
	for {
		open := strings.Index(rest, "```")
		if open < 0 {
			return blocks
		}
		rest = rest[open+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 && !strings.ContainsAny(rest[:nl], "{[\"") {
			rest = rest[nl+1:]
		}
		end := strings.Index(rest, "```")
		if end < 0 {
			return append(blocks, rest)
		}
		blocks = append(blocks, rest[:end])
		rest = rest[end+3:]
	}
}

// balancedSpan returns the first balanced {...} or [...] run in s, ignoring
// delimiters that sit inside a JSON string.
func balancedSpan(s string) (string, bool) {
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return "", false
	}
	open := s[start]
	closer := byte('}')
	if open == '[' {
		closer = ']'
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == open:
			depth++
		case c == closer:
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

func excerpt(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= excerptLimit {
		return s
	}
	return s[:excerptLimit] + "..."
}
