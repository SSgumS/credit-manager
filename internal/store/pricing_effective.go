package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/yuluo688/credit-manager/internal/money"
)

// Effective pricing statuses are the complete contract coworkers consume;
// Keeper-side raw rulebook interpretation is forbidden, so every resolution
// outcome must map to exactly one of these values.
const (
	EffectiveStatusPriced        = "priced"
	EffectiveStatusDisabled      = "disabled"
	EffectiveStatusDenied        = "denied"
	EffectiveStatusAllowedZero   = "allowed_zero"
	EffectiveStatusDefaultPriced = "default_priced"
)

type EffectivePair struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type EffectivePricingItem struct {
	Provider string              `json:"provider"`
	Model    string              `json:"model"`
	Status   string              `json:"status"`
	Allowed  bool                `json:"allowed"`
	RuleID   *string             `json:"rule_id"`
	Price    *money.PricePerMTok `json:"price,omitempty"`
	Tiers    []money.PriceTier   `json:"tiers,omitempty"`
}

type EffectivePricingSnapshot struct {
	Revision string                 `json:"revision"`
	Items    []EffectivePricingItem `json:"items"`
}

// UnknownPricingPolicyInput mirrors the config policy without importing the
// config package into the store boundary.
type UnknownPricingPolicyInput struct {
	Policy  string
	Default *money.PricePerMTok
}

// EffectivePricingSnapshotFor resolves every requested pair in ONE consistent
// read transaction together with the revision, because a snapshot whose items
// and revision straddle a write would price events against a rulebook that
// never existed. Empty input returns the revision only.
// (ref: DL-005)
func (s *Store) EffectivePricingSnapshotFor(ctx context.Context, pairs []EffectivePair, policy UnknownPricingPolicyInput) (EffectivePricingSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return EffectivePricingSnapshot{}, fmt.Errorf("begin effective pricing read: %w", err)
	}
	defer tx.Rollback()

	rules, err := listPricingRulesTx(ctx, tx)
	if err != nil {
		return EffectivePricingSnapshot{}, err
	}
	revision, err := effectiveRevisionLocked(rules, policy)
	if err != nil {
		return EffectivePricingSnapshot{}, err
	}
	snapshot := EffectivePricingSnapshot{Revision: revision, Items: []EffectivePricingItem{}}
	for _, pair := range pairs {
		item, err := resolveEffectiveItem(rules, pair, policy)
		if err != nil {
			return EffectivePricingSnapshot{}, err
		}
		snapshot.Items = append(snapshot.Items, item)
	}
	if err := tx.Commit(); err != nil {
		return EffectivePricingSnapshot{}, fmt.Errorf("commit effective pricing read: %w", err)
	}
	return snapshot, nil
}

func listPricingRulesTx(ctx context.Context, tx *sql.Tx) ([]PricingRule, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+pricingRuleColumns+`
		FROM pricing_rules ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list pricing rules in read transaction: %w", err)
	}
	defer rows.Close()
	var out []PricingRule
	for rows.Next() {
		rule, err := scanPricingRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

// resolveEffectiveItem reuses WinningPricingRule so resolution order can never
// drift from settlement; a disabled winner still wins and yields cost-unknown,
// matching the settlement path that rejects the model.
func resolveEffectiveItem(rules []PricingRule, pair EffectivePair, policy UnknownPricingPolicyInput) (EffectivePricingItem, error) {
	item := EffectivePricingItem{
		Provider: strings.TrimSpace(pair.Provider),
		Model:    strings.TrimSpace(pair.Model),
	}
	if item.Model == "" {
		return item, fmt.Errorf("%w: model is required for every effective pricing pair", ErrInvalidArgument)
	}
	rule, err := WinningPricingRule(rules, item.Model)
	switch {
	case err == nil:
		id := rule.ID
		item.RuleID = &id
		if !rule.Enabled {
			// A disabled winner means cost unknown, never zero: returning the
			// price here would let keepers fabricate cost for a disabled model.
			item.Status = EffectiveStatusDisabled
			item.Allowed = false
			return item, nil
		}
		item.Status = EffectiveStatusPriced
		item.Allowed = true
		price := rule.Price
		item.Price = &price
		item.Tiers = append([]money.PriceTier(nil), rule.Tiers...)
		return item, nil
	case err != nil && err != ErrPricingRuleNotFound:
		// Invalid stored rule or regex must surface as a hard failure; a partial
		// snapshot is never acceptable downstream.
		return item, err
	}

	switch policy.Policy {
	case "deny":
		item.Status = EffectiveStatusDenied
		item.Allowed = false
		return item, nil
	case "allow":
		item.Status = EffectiveStatusAllowedZero
		item.Allowed = true
		zero := money.PricePerMTok{}
		item.Price = &zero
		return item, nil
	case "default":
		if policy.Default == nil {
			item.Status = EffectiveStatusDenied
			item.Allowed = false
			return item, nil
		}
		item.Status = EffectiveStatusDefaultPriced
		item.Allowed = true
		item.RuleID = nil // default card carries no rule identity
		price := *policy.Default
		item.Price = &price
		return item, nil
	default:
		// Unknown or disabled policy never invents permission.
		item.Status = EffectiveStatusDenied
		item.Allowed = false
		return item, nil
	}
}

// EffectiveRevision computes the public revision alone for callers that only
// need change detection; it shares the exact encoding with the snapshot path.
func (s *Store) EffectiveRevision(ctx context.Context, policy UnknownPricingPolicyInput) (string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", fmt.Errorf("begin pricing revision read: %w", err)
	}
	defer tx.Rollback()
	rules, err := listPricingRulesTx(ctx, tx)
	if err != nil {
		return "", err
	}
	return effectiveRevisionLocked(rules, policy)
}

// effectiveRevisionLocked hashes a deterministic ordered encoding of EVERY
// rule including disabled ones plus the default policy/pricing, because a
// coworker cost engine must detect rule deletion, disabling, and default-card
// edits alike; numeric ordering is not required, only determinism.
// (ref: DL-005)
func effectiveRevisionLocked(rules []PricingRule, policy UnknownPricingPolicyInput) (string, error) {
	sorted := append([]PricingRule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	type revisionRule struct {
		ID        string             `json:"id"`
		MatchKind MatchKind          `json:"match_kind"`
		Pattern   string             `json:"pattern"`
		Priority  int                `json:"priority"`
		Price     money.PricePerMTok `json:"price"`
		Tiers     []money.PriceTier  `json:"tiers"`
		Enabled   bool               `json:"enabled"`
	}
	type revisionDocument struct {
		Rules   []revisionRule      `json:"rules"`
		Policy  string              `json:"unknown_policy"`
		Default *money.PricePerMTok `json:"default,omitempty"`
	}
	doc := revisionDocument{Rules: make([]revisionRule, 0, len(sorted)), Policy: policy.Policy, Default: policy.Default}
	for _, rule := range sorted {
		tiers := rule.Tiers
		if tiers == nil {
			tiers = []money.PriceTier{}
		}
		doc.Rules = append(doc.Rules, revisionRule{
			ID: rule.ID, MatchKind: rule.MatchKind, Pattern: rule.Pattern, Priority: rule.Priority,
			Price: rule.Price, Tiers: tiers, Enabled: rule.Enabled,
		})
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode pricing revision document: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
