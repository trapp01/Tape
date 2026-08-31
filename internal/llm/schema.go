package llm

import (
	"encoding/json"
	"fmt"
)

// unsupportedKeywords are the validation keywords strict structured outputs
// reject with a 400. Go-side validation enforces these ranges instead.
var unsupportedKeywords = map[string]bool{
	"minLength": true, "maxLength": true, "pattern": true, "format": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"multipleOf": true, "minItems": true, "maxItems": true, "uniqueItems": true,
	"minProperties": true, "maxProperties": true, "default": true,
}

// namedChildren hold schemas keyed by author-chosen names, so a property called
// "minimum" is a name and not the keyword.
var namedChildren = map[string]bool{"properties": true, "$defs": true, "definitions": true}

// opaqueValues hold instance data rather than subschemas; walking into them
// would rewrite a user's literal values.
var opaqueValues = map[string]bool{"const": true, "enum": true, "examples": true}

// StripUnsupportedKeywords removes the validation keywords strict structured
// outputs reject, at every nesting level. type, enum, required, items, anyOf and
// additionalProperties come through untouched.
func StripUnsupportedKeywords(schema json.RawMessage) (json.RawMessage, error) {
	return rewriteSchema(schema, dropUnsupported)
}

func dropUnsupported(node map[string]any) map[string]any {
	for k := range node {
		if unsupportedKeywords[k] {
			delete(node, k)
		}
	}
	return node
}

// nullableToAnyOf rewrites {"type":["number","null"]} into an anyOf pair. It is
// the only nullable form Anthropic's structured outputs accept.
func nullableToAnyOf(node map[string]any) map[string]any {
	types, ok := node["type"].([]any)
	if !ok {
		return node
	}
	kept := make([]any, 0, len(types))
	nullable := false
	for _, t := range types {
		if t == "null" {
			nullable = true
			continue
		}
		kept = append(kept, t)
	}
	if !nullable || len(kept) != 1 {
		return node
	}

	inner := make(map[string]any, len(node))
	for k, v := range node {
		if k != "description" {
			inner[k] = v
		}
	}
	inner["type"] = kept[0]
	// The description is hoisted so the model still reads it on the wrapper.
	out := map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
	if d, ok := node["description"]; ok {
		out["description"] = d
	}
	return out
}

// rewriteSchema decodes schema, applies fn to every schema object in it, and
// re-encodes. Key order is not preserved; the wire does not depend on it.
func rewriteSchema(schema json.RawMessage, fn func(map[string]any) map[string]any) (json.RawMessage, error) {
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil, fmt.Errorf("decoding json schema: %w", err)
	}
	out, err := json.Marshal(walkSchema(doc, fn))
	if err != nil {
		return nil, fmt.Errorf("encoding json schema: %w", err)
	}
	return out, nil
}

func walkSchema(node any, fn func(map[string]any) map[string]any) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			switch {
			case opaqueValues[k]:
				out[k] = child
			case namedChildren[k]:
				out[k] = walkNamed(child, fn)
			default:
				out[k] = walkSchema(child, fn)
			}
		}
		return fn(out)
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = walkSchema(child, fn)
		}
		return out
	}
	return node
}

// walkNamed walks a map whose keys name properties or definitions, so the keys
// are carried through as names and only their values are treated as schemas.
func walkNamed(node any, fn func(map[string]any) map[string]any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return walkSchema(node, fn)
	}
	out := make(map[string]any, len(m))
	for name, child := range m {
		out[name] = walkSchema(child, fn)
	}
	return out
}
