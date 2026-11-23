package service

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo688/credit-manager/internal/config"
)

func TestRequestMetadataMatchesLegacyDecode(t *testing.T) {
	bodies := []string{
		``,
		`{}`,
		`null`,
		`[]`,
		`"model"`,
		`{"model":"gpt-4o-mini"}`,
		`{"model":"  gpt-4o-mini  "}`,
		`{"model":""}`,
		`{"model":null}`,
		`{"model":42}`,
		`{"model":{"name":"x"}}`,
		`{"model":"first","model":"second"}`,
		`{"Model":"case-folded"}`,
		`{"service_tier":"priority"}`,
		`{"service_tier":"","tier":"flex"}`,
		`{"service_tier":3,"tier":"flex"}`,
		`{"tier":"  flex  ","service_tier":"priority"}`,
		`{"max_tokens":1024}`,
		`{"max_tokens":0,"max_output_tokens":256}`,
		`{"max_tokens":"1024","max_output_tokens":256}`,
		`{"max_tokens":-5,"max_output_tokens":256}`,
		`{"max_completion_tokens":512.9}`,
		`{"generationConfig":{"maxOutputTokens":2048}}`,
		`{"generationConfig":{"maxOutputTokens":"2048"}}`,
		`{"generationConfig":{"maxOutputTokens":-1}}`,
		`{"generationConfig":null}`,
		`{"generationConfig":"2048"}`,
		`{"max_tokens":128,"generationConfig":{"maxOutputTokens":2048}}`,
		`{"n":4}`,
		`{"n":0,"n_images":2}`,
		`{"n":"3","n_images":2}`,
		`{"n":-1,"num_images":64}`,
		`{"n":2.9}`,
		`{"image_count":7}`,
		`{"n":true,"image_count":7}`,
		`{"n":1,"n":9}`,
		`{"model":"gpt-4o-mini","max_tokens":512,"service_tier":"priority","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","max_tokens":64,"junk":1e400}`,
		`{"model":"m","service_tier":"priority","junk":1e400}`,
		`{"model":"m","n":4,"junk":1e400}`,
		`{"model":"m","generationConfig":{"maxOutputTokens":8},"junk":1e400}`,
		// Truncation precedence the legacy scan applied.
		`{"max_tokens":0.1,"generationConfig":{"maxOutputTokens":8}}`,
		`{"max_tokens":0.9}`,
		`{"n":0.1,"n_images":2}`,
		`{"n":0.9}`,
		`{"n":0,"n_images":0,"num_images":3}`,
		`{"max_tokens":1e300}`,
		`{"n":1e300}`,
		`{"model":"gpt-4o-mini"`,
		`{"model":"gpt-4o-mini"} trailing`,
		`{"model":1e400}`,
	}
	for _, body := range bodies {
		wantModel, wantTier, wantLimit, wantImages := legacyRequestMetadata([]byte(body))
		meta := readRequestMetadata([]byte(body))
		if got := meta.model(); got != wantModel {
			t.Errorf("body %q model: got %q want %q", body, got, wantModel)
		}
		if got := meta.serviceTier(); got != wantTier {
			t.Errorf("body %q tier: got %q want %q", body, got, wantTier)
		}
		if got := meta.outputLimit(); got != wantLimit {
			t.Errorf("body %q output limit: got %d want %d", body, got, wantLimit)
		}
		wantImage := wantImages
		if len(body) == 0 {
			wantImage = 1
		}
		if got := meta.imageCount(); got != wantImage {
			t.Errorf("body %q images: got %d want %d", body, got, wantImage)
		}
		wantIn, wantOut := legacyEstimateTokens([]byte(body), 4096, 1_000_000)
		in, out := estimateTokensWithLimit([]byte(body), meta.outputLimit(), 4096, 1_000_000)
		if in != wantIn || out != wantOut {
			t.Errorf("body %q estimate: got (%d,%d) want (%d,%d)", body, in, out, wantIn, wantOut)
		}
	}
}

func TestEstimateTokensBoundsPreserved(t *testing.T) {
	body := []byte(`{"model":"m","max_tokens":900000}`)
	for _, maxTotal := range []int64{1_000_000, 500_000, 1000} {
		wantIn, wantOut := legacyEstimateTokens(body, 4096, maxTotal)
		meta := readRequestMetadata(body)
		gotIn, gotOut := estimateTokensWithLimit(body, meta.outputLimit(), 4096, maxTotal)
		if gotIn != wantIn || gotOut != wantOut {
			t.Errorf("maxTotal %d: got (%d,%d) want (%d,%d)", maxTotal, gotIn, gotOut, wantIn, wantOut)
		}
	}
}

func TestBuildReservePlanUsesRequestOverrides(t *testing.T) {
	svc := openServiceForTest(t)
	ctx := context.Background()
	plan, err := svc.BuildReservePlan(ctx, "", []byte(`{"model":"gpt-4o-mini","max_output_tokens":300,"service_tier":"priority","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.Model != "gpt-4o-mini" || plan.OutputEstimate != 300 {
		t.Fatalf("plan = %#v, want model and explicit output limit", plan)
	}
	large := []byte(`{"model":"gpt-4o-mini","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("x", 200_000) + `"}]}`)
	plan, err = svc.BuildReservePlan(ctx, "gpt-4o-mini", large)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.OutputEstimate != 64 || plan.InputEstimate != int64(len(large)/2)+1 {
		t.Fatalf("plan = %#v, want body-derived input with explicit output", plan)
	}
}

// A supplied model that resolves to unknown_pricing=allow must not parse the
// body: the pre-change path returned before any extraction.
func TestReserveAllowUnpricedSkipsBodyParse(t *testing.T) {
	svc := openServiceWithConfig(t, func(cfg *config.Config) {
		cfg.Pricing.UnknownPolicy = config.UnknownPricingAllow
	})
	if err := svc.Store().DeletePricingRule(context.Background(), BootstrapPricingRuleID); err != nil {
		t.Fatalf("delete bootstrap rule: %v", err)
	}
	plan, err := svc.BuildReservePlan(context.Background(), "unlisted-model", []byte(`{"model":"other","max_tokens":64}`))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if !plan.AllowUnpriced || plan.Amount != 0 || plan.Model != "unlisted-model" {
		t.Fatalf("plan = %#v, want unpriced supplied model", plan)
	}
	// The body is still parsed when it is the only model source, so an
	// unpriced request without a supplied model reports that model.
	plan, err = svc.BuildReservePlan(context.Background(), "", []byte(`{"model":"other"}`))
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.Model != "other" || !plan.AllowUnpriced {
		t.Fatalf("plan = %#v, want body-derived unpriced model", plan)
	}
}

func TestImageCountClampedAndDefaults(t *testing.T) {
	meta := readRequestMetadata([]byte(`{"n":1000}`))
	if got := meta.imageCount(); got != 32 {
		t.Fatalf("image count = %d, want clamped 32", got)
	}
	if got := readRequestMetadata([]byte(`{"model":"m"}`)).imageCount(); got != 1 {
		t.Fatalf("default image count = %d, want 1", got)
	}
}
