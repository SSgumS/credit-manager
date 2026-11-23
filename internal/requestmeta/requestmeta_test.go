package requestmeta

import (
	"encoding/json"
	"strings"
	"testing"
)

// legacyStringRead is the pre-change helper: whole-body decode into any.
func legacyStringRead(body []byte, keys ...string) string {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := root[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func TestStringMatchesDecodedMap(t *testing.T) {
	bodies := []string{
		``,
		`{}`,
		`null`,
		`[]`,
		`"text"`,
		`{"model":"gpt-4o"}`,
		`{"model":"  gpt-4o  "}`,
		`{"model":""}`,
		`{"model":null}`,
		`{"model":42}`,
		`{"model":true}`,
		`{"model":["gpt-4o"]}`,
		`{"model":"first","model":"second"}`,
		`{"Model":"case"}`,
		`{"service_tier":"","tier":"priority"}`,
		`{"service_tier":7,"tier":"priority"}`,
		`{"model":"x"`,
		`{"model":"x"} trailing`,
		// A member the legacy map decode rejects must zero every field.
		`{"model":"gpt-4o","junk":1e400}`,
		`{"tier":"priority","nested":{"junk":1e400}}`,
	}
	for _, body := range bodies {
		for _, keys := range [][]string{{"model"}, {"service_tier", "tier"}} {
			want := legacyStringRead([]byte(body), keys...)
			got := Read([]byte(body)).String(keys...)
			if got != want {
				t.Errorf("body %q keys %v: got %q want %q", body, keys, got, want)
			}
		}
	}
}

func TestFloatKeepsFractionsAndSkipsNonNumbers(t *testing.T) {
	cases := []struct {
		body   string
		want   float64
		wantOK bool
	}{
		{`{"n":2}`, 2, true},
		{`{"n":2.9}`, 2.9, true},
		{`{"n":0.1}`, 0.1, true},
		{`{"n":1e3}`, 1000, true},
		{`{"n":-4}`, -4, true},
		{`{"n":"5"}`, 0, false},
		{`{"n":null}`, 0, false},
		{`{"n":true}`, 0, false},
		{`{"n":1,"n":4}`, 4, true},
		{`{"n":64,"junk":1e400}`, 0, false},
		{`{"n":64,"nested":{"junk":1e400}}`, 0, false},
	}
	for _, c := range cases {
		got, ok := Read([]byte(c.body)).Float("n")
		if got != c.want || ok != c.wantOK {
			t.Errorf("body %q: got (%v,%v) want (%v,%v)", c.body, got, ok, c.want, c.wantOK)
		}
	}
}

func TestObjectAndNestedString(t *testing.T) {
	body := Read([]byte(`{"reasoning":{"effort":"low"},"thinking":{"effort":7},"reasoning_effort":" medium ","nested":{"deep":{"value":"v"}}}`))
	if got := body.NestedString([]string{"reasoning", "effort"}, []string{"thinking", "effort"}, []string{"reasoning_effort"}); got != "low" {
		t.Errorf("nested string = %q, want low", got)
	}
	if got := body.NestedString([]string{"thinking", "effort"}, []string{"reasoning_effort"}); got != "medium" {
		t.Errorf("nested string = %q, want medium", got)
	}
	if got := body.NestedString([]string{"nested", "deep", "value"}); got != "v" {
		t.Errorf("deep nested string = %q, want v", got)
	}
	if got := body.NestedString([]string{"missing", "effort"}, []string{"reasoning", "missing"}); got != "" {
		t.Errorf("missing nested string = %q, want empty", got)
	}
	if got := Read([]byte(`{"reasoning":"low"}`)).NestedString([]string{"reasoning", "effort"}); got != "" {
		t.Errorf("non-object parent string = %q, want empty", got)
	}
	if got := Read([]byte(`{"a":{"b":"c","junk":1e400}}`)).NestedString([]string{"a", "b"}); got != "" {
		t.Errorf("rejected nested decode leaked a value: %q", got)
	}
}

func TestStringDoesNotRetainBody(t *testing.T) {
	var body []byte
	for i := 0; i < 8; i++ {
		body = []byte(`{"service_tier":"priority","messages":"` + strings.Repeat("x", 4096) + `"}`)
	}
	value := Read(body).String("service_tier")
	if value != "priority" {
		t.Fatalf("value = %q", value)
	}
	// Rewriting the body must not change the returned string.
	for i := range body {
		body[i] = 'A'
	}
	if value != "priority" {
		t.Fatal("returned string views the request body")
	}
}
