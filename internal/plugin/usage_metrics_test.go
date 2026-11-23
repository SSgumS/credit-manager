package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/yuluo688/credit-manager/internal/store"
)

func TestUsageMetricsMatchesLegacyDecode(t *testing.T) {
	bodies := []string{
		``,
		`{}`,
		`null`,
		`[]`,
		`{"service_tier":"priority"}`,
		`{"service_tier":" priority "}`,
		`{"service_tier":"","tier":"flex"}`,
		`{"service_tier":5,"tier":"flex"}`,
		`{"tier":"flex","service_tier":"priority"}`,
		`{"reasoning":{"effort":"low"}}`,
		`{"reasoning":{"effort":""},"thinking":{"effort":"high"}}`,
		`{"reasoning":"low"}`,
		`{"reasoning":null,"reasoning_effort":" medium "}`,
		`{"thinking":{"effort":3},"reasoning_effort":" medium "}`,
		`{"reasoning":{"effort":["x"]}}`,
		`{"service_tier":"priority","reasoning":{"effort":"low"}}`,
		`{"service_tier":"priority","reasoning":{"effort":"low"},"junk":1e400}`,
		`{"thinking":{"effort":"high"},"junk":[1e400]}`,
		`{"service_tier":"priority"`,
	}
	started := time.Unix(100, 0)
	first := started.Add(250 * time.Millisecond)
	completed := started.Add(2 * time.Second)
	for _, body := range bodies {
		for _, tc := range []struct {
			first  time.Time
			done   time.Time
			result string
		}{
			{first, completed, "success"},
			{time.Time{}, completed, "failed"},
			{started, started.Add(-time.Second), "cancelled"},
		} {
			want := legacyUsageMetrics([]byte(body), started, tc.first, tc.done, tc.result)
			got := usageMetricsFromStream([]byte(body), started, tc.first, tc.done, tc.result)
			if !sameMetrics(got, want) {
				t.Errorf("body %q first %v done %v: got %#v want %#v", body, tc.first, tc.done, got, want)
			}
		}
	}
}

func sameMetrics(got, want store.UsageMetrics) bool {
	if !sameOptional(got.Tier, want.Tier) || !sameOptional(got.Result, want.Result) ||
		!sameOptional(got.ThinkingIntensity, want.ThinkingIntensity) ||
		!sameOptionalDuration(got.GenerationDuration, want.GenerationDuration) ||
		!sameOptionalDuration(got.FirstTokenLatency, want.FirstTokenLatency) {
		return false
	}
	return true
}

func sameOptional(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func sameOptionalDuration(got, want *time.Duration) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func TestUsageMetricsKeepsLargeImageBodyUntouched(t *testing.T) {
	body := []byte(`{"service_tier":"priority","messages":[{"role":"user","content":"` + strings.Repeat("x", 1<<20) + `"}]}`)
	original := string(body)
	metrics := usageMetricsFromRequest(body, time.Unix(0, 0), time.Unix(1, 0), "success")
	if metrics.Tier == nil || *metrics.Tier != "priority" {
		t.Fatalf("tier = %v, want priority", metrics.Tier)
	}
	if string(body) != original {
		t.Fatal("metrics extraction modified the request body")
	}
}
