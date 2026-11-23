package plugin

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/yuluo688/credit-manager/internal/store"
)

// legacyUsageMetrics mirrors the pre-change metric extraction: every field read
// decodes the whole body into any. It is the oracle for the focused reader.
func legacyUsageMetrics(body []byte, startedAt, firstChunkAt, completedAt time.Time, result string) store.UsageMetrics {
	metrics := store.UsageMetrics{
		Tier:              optionalString(legacyRequestString(body, "service_tier", "tier")),
		Result:            optionalString(result),
		ThinkingIntensity: optionalString(legacyRequestNestedString(body, []string{"reasoning", "effort"}, []string{"thinking", "effort"}, []string{"reasoning_effort"})),
	}
	if !startedAt.IsZero() && !completedAt.IsZero() && !completedAt.Before(startedAt) {
		generationDuration := completedAt.Sub(startedAt)
		metrics.GenerationDuration = &generationDuration
	}
	if !startedAt.IsZero() && !firstChunkAt.IsZero() && !firstChunkAt.Before(startedAt) {
		firstTokenLatency := firstChunkAt.Sub(startedAt)
		metrics.FirstTokenLatency = &firstTokenLatency
	}
	return metrics
}

func legacyRequestString(body []byte, keys ...string) string {
	var value map[string]any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return ""
	}
	for _, key := range keys {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func legacyRequestNestedString(body []byte, paths ...[]string) string {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return ""
	}
	for _, path := range paths {
		current := value
		for _, key := range path {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[key]
		}
		if text, ok := current.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
