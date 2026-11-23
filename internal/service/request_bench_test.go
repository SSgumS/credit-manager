package service

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo688/credit-manager/internal/config"
	"github.com/yuluo688/credit-manager/internal/money"
	"github.com/yuluo688/credit-manager/internal/store"
)

const benchPepperEnv = "CREDIT_MANAGER_BENCH_PEPPERS"

func openServiceForTest(tb testing.TB) *Service {
	tb.Helper()
	svc := openServiceWithConfig(tb, func(cfg *config.Config) {})
	if err := svc.Store().PutPricingRule(context.Background(), store.PricingRule{
		ID: "all", MatchKind: store.MatchGlob, Pattern: "*", Priority: 1, Enabled: true,
		Price: money.PricePerMTok{Input: 1_000_000, Output: 3_000_000},
	}); err != nil {
		tb.Fatalf("put pricing rule: %v", err)
	}
	return svc
}

func openServiceWithConfig(tb testing.TB, mutate func(*config.Config)) *Service {
	tb.Helper()
	tb.Setenv(benchPepperEnv, "active:0123456789abcdef0123456789abcdef")
	cfg := config.Default()
	cfg.DataDir = tb.TempDir()
	cfg.Keys.PepperEnv = benchPepperEnv
	cfg.Keys.ActivePepperID = "active"
	mutate(&cfg)
	svc, err := Open(context.Background(), cfg)
	if err != nil {
		tb.Fatalf("open service: %v", err)
	}
	tb.Cleanup(func() { _ = svc.Close() })
	return svc
}

var (
	benchSmallBody = []byte(`{"model":"gpt-4o-mini","max_tokens":512,"service_tier":"priority","messages":[{"role":"system","content":"You are concise."},{"role":"user","content":"Summarize the release notes."}]}`)

	benchLargeImageBody = []byte(`{"model":"gpt-4o-mini","max_tokens":1024,"service_tier":"priority","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"What is in this screenshot?"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + strings.Repeat("iVBORw0KGgo", 384*1024) + `"}}]}]}`)

	benchLargeToolBody = []byte(`{"model":"gpt-4o-mini","max_output_tokens":256,"messages":[{"role":"user","content":"` +
		strings.Repeat("context line with tool output ", 128*1024) + `"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)
)

func BenchmarkBuildReservePlanSmall(b *testing.B) {
	svc := openServiceForTest(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.BuildReservePlan(ctx, "", benchSmallBody); err != nil {
			b.Fatalf("build plan: %v", err)
		}
	}
}

func BenchmarkBuildReservePlanLargeImage(b *testing.B) {
	svc := openServiceForTest(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.BuildReservePlan(ctx, "", benchLargeImageBody); err != nil {
			b.Fatalf("build plan: %v", err)
		}
	}
}

func BenchmarkBuildReservePlanLargeTool(b *testing.B) {
	svc := openServiceForTest(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.BuildReservePlan(ctx, "", benchLargeToolBody); err != nil {
			b.Fatalf("build plan: %v", err)
		}
	}
}

func BenchmarkBuildReservePlanUnpricedProvidedModel(b *testing.B) {
	svc := openServiceWithConfig(b, func(cfg *config.Config) {
		cfg.Pricing.UnknownPolicy = config.UnknownPricingAllow
	})
	if err := svc.Store().DeletePricingRule(context.Background(), BootstrapPricingRuleID); err != nil {
		b.Fatalf("delete bootstrap rule: %v", err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan, err := svc.BuildReservePlan(ctx, "unlisted-model", benchLargeImageBody)
		if err != nil || !plan.AllowUnpriced || plan.Amount != 0 {
			b.Fatalf("plan = %#v err = %v", plan, err)
		}
	}
}

func BenchmarkRequestMetadataSmall(b *testing.B) {
	benchmarkRequestMetadata(b, benchSmallBody)
}

func BenchmarkRequestMetadataLargeImage(b *testing.B) {
	benchmarkRequestMetadata(b, benchLargeImageBody)
}

func BenchmarkRequestMetadataLargeTool(b *testing.B) {
	benchmarkRequestMetadata(b, benchLargeToolBody)
}

// benchmarkRequestMetadata exercises the shared decode plus every projected
// field, the unit the reserve plan reads in one pass.
func benchmarkRequestMetadata(b *testing.B, body []byte) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		meta := readRequestMetadata(body)
		_ = meta.model()
		_ = meta.imageCount()
		_ = meta.outputLimit()
		_ = meta.serviceTier()
	}
}
