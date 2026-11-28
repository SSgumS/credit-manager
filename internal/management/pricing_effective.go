package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yuluo688/credit-manager/internal/money"
	"github.com/yuluo688/credit-manager/internal/service"
	"github.com/yuluo688/credit-manager/internal/store"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// maxEffectivePricingPairs caps one request at 1000 pairs so a keeper sync
// round stays a single bounded read transaction.
const maxEffectivePricingPairs = 1000

func effectivePolicyInput(svc *service.Service) store.UnknownPricingPolicyInput {
	cfg := svc.Config()
	input := store.UnknownPricingPolicyInput{Policy: string(cfg.Pricing.UnknownPolicy)}
	if cfg.Pricing.Default != nil {
		input.Default = &money.PricePerMTok{
			Input:         money.MicroUSD(cfg.Pricing.Default.Input),
			Output:        money.MicroUSD(cfg.Pricing.Default.Output),
			Reasoning:     money.MicroUSD(cfg.Pricing.Default.Reasoning),
			Cached:        money.MicroUSD(cfg.Pricing.Default.Cached),
			CacheRead:     money.MicroUSD(cfg.Pricing.Default.CacheRead),
			CacheCreation: money.MicroUSD(cfg.Pricing.Default.CacheCreation),
		}
	}
	return input
}

// postPricingEffective returns 200 only for a complete snapshot. Error mapping
// is contractual: bad request shape or pair-cap violation = 400, invalid stored
// rule or regex = 500, storage failure = 503. Anything less than completeness
// must fail because keeper sync retains its previous snapshot on any error.
// (ref: DL-005, DL-006)
func postPricingEffective(ctx context.Context, svc *service.Service, body []byte) (pluginapi.ManagementResponse, error) {
	var req struct {
		Items []store.EffectivePair `json:"items"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonErrNoStore(http.StatusBadRequest, "invalid json"), nil
	}
	if len(req.Items) > maxEffectivePricingPairs {
		return jsonErrNoStore(http.StatusBadRequest, "at most 1000 provider/model pairs"), nil
	}
	for _, pair := range req.Items {
		if strings.TrimSpace(pair.Model) == "" {
			return jsonErrNoStore(http.StatusBadRequest, "model is required for every pair"), nil
		}
	}
	snapshot, err := svc.Store().EffectivePricingSnapshotFor(ctx, req.Items, effectivePolicyInput(svc))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidArgument):
			return jsonErrNoStore(http.StatusBadRequest, "invalid effective pricing request"), nil
		case isInvalidStoredRule(err):
			// A corrupt stored rule or regex means the rulebook itself cannot be
			// interpreted; that is a server fault, not a store outage.
			return jsonErrNoStore(http.StatusInternalServerError, "pricing rulebook invalid"), nil
		default:
			return jsonErrNoStore(http.StatusServiceUnavailable, "pricing snapshot unavailable"), nil
		}
	}
	return jsonOKNoStore(snapshot), nil
}

func isInvalidStoredRule(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "invalid match kind") || strings.Contains(message, "error parsing regexp")
}
