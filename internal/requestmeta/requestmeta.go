// Package requestmeta projects a few request metadata fields from one decoded
// body. Callers that need several fields share the result.
package requestmeta

import (
	"strings"

	moegojson "github.com/MoeForge/moego/json"
)

// Metadata holds the decoded top-level members of a JSON object request.
//
// Decoding the whole body is deliberate: a member that the legacy map decode
// rejected (an out-of-range number, a malformed body) must leave every field
// at its zero value, exactly as before.
type Metadata struct {
	fields map[string]any
}

// Read decodes the top-level members of body. A body that is not a single JSON
// object yields a Metadata that reports zero values for every field.
func Read(body []byte) Metadata {
	if len(body) == 0 {
		return Metadata{}
	}
	var fields map[string]any
	if moegojson.UnmarshalNoCopy(body, &fields) != nil || fields == nil {
		return Metadata{}
	}
	return Metadata{fields: fields}
}

// String returns the first non-empty string member among keys, trimmed.
// Non-string values are skipped, matching a per-key type assertion.
func (m Metadata) String(keys ...string) string {
	for _, key := range keys {
		text, ok := m.fields[key].(string)
		if !ok {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			// The decoded string may view the body; keep the body from being
			// retained by a long-lived metrics value.
			return strings.Clone(text)
		}
	}
	return ""
}

// Float returns the number member named key. Numbers decode as float64, so a
// non-number member reports false. Callers apply their own sign rule before
// converting, because a positive fraction keeps its truncated value.
func (m Metadata) Float(key string) (float64, bool) {
	number, ok := m.fields[key].(float64)
	return number, ok
}

// Object returns the object member named key. A missing member or one that is
// not a JSON object yields a Metadata that reports zero values.
func (m Metadata) Object(key string) Metadata {
	fields, _ := m.fields[key].(map[string]any)
	return Metadata{fields: fields}
}

// NestedString returns the first path that resolves to a non-empty string,
// trimmed. Every path component must be an object member.
func (m Metadata) NestedString(paths ...[]string) string {
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		current := m
		for _, key := range path[:len(path)-1] {
			current = current.Object(key)
		}
		if text := current.String(path[len(path)-1]); text != "" {
			return text
		}
	}
	return ""
}
