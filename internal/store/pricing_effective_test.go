package store

import (
	"context"
	"strings"
	"testing"

	"github.com/yuluo688/credit-manager/internal/money"
)

// Revision stability is a hard contract: any upsert, enable, disable, or
// delete must change the revision while a no-op keeps it stable, because the
// keeper cost engine keys pricing snapshots and immutable event costs on it.
// (ref: DL-005, DL-006)
func TestEffectiveRevisionChangesOnAnyWrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	policy := UnknownPricingPolicyInput{Policy: "deny"}

	base := PricingRule{
		ID: "rule-a", MatchKind: MatchExact, Pattern: "gpt-5", Priority: 10, Enabled: true,
		Price: money.PricePerMTok{Input: 100, Output: 200},
	}
	if err := s.PutPricingRule(ctx, base); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	rev1, err := s.EffectiveRevision(ctx, policy)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	if !strings.HasPrefix(rev1, "sha256:") {
		t.Fatalf("revision must carry sha256: prefix, got %q", rev1)
	}
	// identical rewrite is a no-op for the fingerprint
	if err := s.PutPricingRule(ctx, base); err != nil {
		t.Fatalf("rewrite rule: %v", err)
	}
	rev2, _ := s.EffectiveRevision(ctx, policy)
	if rev1 != rev2 {
		t.Fatalf("no-op write changed revision: %s -> %s", rev1, rev2)
	}

	mutations := []struct {
		name   string
		mutate func()
	}{
		{"upsert new rule", func() {
			_ = s.PutPricingRule(ctx, PricingRule{ID: "rule-b", MatchKind: MatchExact, Pattern: "other", Price: money.PricePerMTok{Input: 1}})
		}},
		{"disable rule", func() { _ = s.SetPricingRuleEnabled(ctx, "rule-a", false) }},
		{"enable rule", func() { _ = s.SetPricingRuleEnabled(ctx, "rule-a", true) }},
		{"delete rule", func() { _ = s.DeletePricingRule(ctx, "rule-b") }},
	}
	previous := rev2
	for _, mutation := range mutations {
		name := mutation.name
		mutation.mutate()
		next, err := s.EffectiveRevision(ctx, policy)
		if err != nil {
			t.Fatalf("%s: revision: %v", name, err)
		}
		if next == previous {
			t.Fatalf("%s did not change revision", name)
		}
		previous = next
	}
}

func TestEffectivePricingStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enabled := PricingRule{ID: "on", MatchKind: MatchExact, Pattern: "model-on", Enabled: true, Price: money.PricePerMTok{Input: 100}}
	disabled := PricingRule{ID: "off", MatchKind: MatchExact, Pattern: "model-off", Enabled: false, Price: money.PricePerMTok{Input: 100}}
	for _, rule := range []PricingRule{enabled, disabled} {
		if err := s.PutPricingRule(ctx, rule); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	defaultCard := &money.PricePerMTok{Input: 5, Output: 10}

	cases := []struct {
		name        string
		policy      UnknownPricingPolicyInput
		model       string
		wantStatus  string
		wantAllowed bool
		wantRule    bool
	}{
		{"active winner priced", UnknownPricingPolicyInput{Policy: "deny"}, "model-on", EffectiveStatusPriced, true, true},
		{"disabled winner cost unknown", UnknownPricingPolicyInput{Policy: "deny"}, "model-off", EffectiveStatusDisabled, false, true},
		{"unmatched deny", UnknownPricingPolicyInput{Policy: "deny"}, "unknown", EffectiveStatusDenied, false, false},
		{"unmatched allow zero", UnknownPricingPolicyInput{Policy: "allow"}, "unknown", EffectiveStatusAllowedZero, true, false},
		{"unmatched default card", UnknownPricingPolicyInput{Policy: "default", Default: defaultCard}, "unknown", EffectiveStatusDefaultPriced, true, false},
		{"unmatched default missing card denied", UnknownPricingPolicyInput{Policy: "default"}, "unknown", EffectiveStatusDenied, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := s.EffectivePricingSnapshotFor(ctx, []EffectivePair{{Model: tc.model}}, tc.policy)
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if len(snapshot.Items) != 1 {
				t.Fatalf("want exactly one item, got %d", len(snapshot.Items))
			}
			item := snapshot.Items[0]
			if item.Status != tc.wantStatus || item.Allowed != tc.wantAllowed {
				t.Fatalf("got status=%s allowed=%v, want %s/%v", item.Status, item.Allowed, tc.wantStatus, tc.wantAllowed)
			}
			if (item.RuleID != nil) != tc.wantRule {
				t.Fatalf("rule_id presence = %v, want %v", item.RuleID != nil, tc.wantRule)
			}
			if item.Status == EffectiveStatusDefaultPriced && item.RuleID != nil {
				t.Fatalf("default_priced must carry rule_id null")
			}
			if item.Status == EffectiveStatusDisabled && item.Price != nil {
				t.Fatalf("disabled winner must not expose a price: cost unknown, never zero")
			}
		})
	}
}

func TestEffectivePricingEmptyInputRevisionOnly(t *testing.T) {
	s := newTestStore(t)
	snapshot, err := s.EffectivePricingSnapshotFor(context.Background(), nil, UnknownPricingPolicyInput{Policy: "deny"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshot.Items) != 0 {
		t.Fatalf("empty input must return revision only")
	}
	if !strings.HasPrefix(snapshot.Revision, "sha256:") {
		t.Fatalf("revision missing: %q", snapshot.Revision)
	}
}
