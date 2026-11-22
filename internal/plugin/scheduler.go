package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/yuluo688/credit-manager/internal/service"
	"github.com/yuluo688/credit-manager/internal/store"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func pickAuth(raw []byte) ([]byte, error) {
	svc := service.Current()
	if svc == nil {
		return okEnvelope(pluginapi.SchedulerPickResponse{})
	}
	var req pluginapi.SchedulerPickRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	candidates := make([]service.AuthPickCandidate, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		id := firstNonEmpty(candidate.ID)
		if id == "" {
			continue
		}
		candidates = append(candidates, service.AuthPickCandidate{ID: id, Provider: candidate.Provider, Priority: candidate.Priority, Status: candidate.Status})
	}
	if authID, handled, err := warmupTarget(req.Options.Headers, candidates); handled {
		if err != nil {
			return errorEnvelope("warmup_rejected", err.Error()), nil
		}
		return okEnvelope(pluginapi.SchedulerPickResponse{AuthID: authID, Handled: true})
	}
	ctx := context.Background()
	reservationID := headerValueFold(req.Options.Headers, reservationHeader)
	if svc.Config().Scheduler.AllowProviderExclusions && reservationID != "" {
		if excluded := parseExcludedProviders(req.Options.Headers); len(excluded) > 0 {
			filtered := make([]service.AuthPickCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if _, drop := excluded[strings.ToLower(strings.TrimSpace(candidate.Provider))]; !drop {
					filtered = append(filtered, candidate)
				}
			}
			switch {
			case len(filtered) > 0:
				candidates = filtered
			case svc.HasPriorAuthPick(reservationID):
				// A retry re-pick where exclusion removed the last candidates:
				// reject so CPA returns the earlier upstream error instead of
				// routing the retry back to an excluded provider.
				return okEnvelope(pluginapi.SchedulerPickResponse{
					Handled:      true,
					Reject:       true,
					RejectCode:   "provider_excluded",
					RejectReason: "all remaining candidates excluded for this request",
				})
			}
			// First pick with every candidate excluded: retain the unfiltered
			// list so the selected provider's after-auth path can answer with a
			// clear local rejection instead of a generic auth_not_found.
		}
	}
	authID, handled, err := svc.PickAuthForRequest(ctx, req.Model, headerValueFold(req.Options.Headers, sessionAffinityHeader), reservationID, candidates)
	if err != nil {
		recordRejectedFromHeaders(ctx, svc, req.Options.Headers, req.Options.Metadata, firstNonEmpty(req.Model, "unknown"), "failed", service.SourceAttemptRejected, "scheduler_reject:"+err.Error())
		if errors.Is(err, store.ErrConcurrentLimit) {
			// Every candidate in every tier is full: terminate selection
			// cleanly instead of failing the RPC.
			return okEnvelope(pluginapi.SchedulerPickResponse{Handled: true, Reject: true, RejectCode: "concurrency_limit_reached", RejectReason: err.Error()})
		}
		return errorEnvelope("limit_rejected", err.Error()), nil
	}
	return okEnvelope(pluginapi.SchedulerPickResponse{
		AuthID:  authID,
		Handled: handled,
	})
}

// parseExcludedProviders collects the lowercase provider set from every
// X-Credit-Manager-Exclude-Providers value; the header may repeat and each
// value may be comma-separated.
func parseExcludedProviders(headers http.Header) map[string]struct{} {
	out := map[string]struct{}{}
	for name, values := range headers {
		if !strings.EqualFold(name, excludeProvidersHeader) {
			continue
		}
		for _, value := range values {
			for _, token := range strings.Split(value, ",") {
				if token = strings.ToLower(strings.TrimSpace(token)); token != "" {
					out[token] = struct{}{}
				}
			}
		}
	}
	return out
}
