package plugin

import (
	"strings"
	"testing"
	"time"
)

var (
	benchPluginSmallBody = []byte(`{"model":"gpt-4o-mini","max_tokens":512,"service_tier":"priority","reasoning":{"effort":"low"},"messages":[{"role":"user","content":"Summarize the release notes."}]}`)

	benchPluginLargeImageBody = []byte(`{"model":"gpt-4o-mini","max_tokens":1024,"service_tier":"priority","thinking":{"effort":"high"},"messages":[{"role":"user","content":[` +
		`{"type":"text","text":"Describe this image."},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + strings.Repeat("iVBORw0KGgo", 384*1024) + `"}}]}]}`)
)

func BenchmarkUsageMetricsSmall(b *testing.B) {
	started := time.Unix(0, 0)
	completed := started.Add(2 * time.Second)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = usageMetricsFromStream(benchPluginSmallBody, started, started, completed, "success")
	}
}

func BenchmarkUsageMetricsLargeImage(b *testing.B) {
	started := time.Unix(0, 0)
	completed := started.Add(2 * time.Second)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = usageMetricsFromStream(benchPluginLargeImageBody, started, started, completed, "success")
	}
}
