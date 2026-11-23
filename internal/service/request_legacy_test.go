package service

import (
	"encoding/json"
	"strings"
)

// legacyRequestMetadata mirrors the pre-change extraction behaviour: every
// field read decodes the whole body into map[string]any. It is the oracle the
// focused request-metadata reader must match.
func legacyRequestMetadata(body []byte) (model, tier string, outputLimit, images int64) {
	model = legacyExtractString(body, "model")
	tier = legacyExtractString(body, "service_tier", "tier")
	outputLimit = legacyExtractOutputLimit(body)
	images = legacyExtractImageCount(body)
	return model, tier, outputLimit, images
}

func legacyEstimateTokens(body []byte, defaultOutput, maxTotal int64) (input, output int64) {
	output = defaultOutput
	if body == nil {
		input = 1
	} else {
		input = int64(len(body)/2) + 1
	}
	if explicit := legacyExtractOutputLimit(body); explicit > 0 {
		output = explicit
	}
	if input > maxTotal {
		input = maxTotal
	}
	if output > maxTotal {
		output = maxTotal
	}
	if input+output > maxTotal {
		if output >= maxTotal {
			output = maxTotal
			input = 0
		} else {
			input = maxTotal - output
		}
	}
	if input <= 0 {
		input = 1
	}
	if output <= 0 {
		output = 1
	}
	return input, output
}

func legacyExtractString(body []byte, keys ...string) string {
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

func legacyExtractOutputLimit(body []byte) int64 {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return 0
	}
	for _, key := range []string{"max_tokens", "max_output_tokens", "max_completion_tokens"} {
		if value, ok := root[key]; ok {
			switch typed := value.(type) {
			case float64:
				if typed > 0 {
					return int64(typed)
				}
			case int64:
				if typed > 0 {
					return typed
				}
			case int:
				if typed > 0 {
					return int64(typed)
				}
			}
		}
	}
	if gen, ok := root["generationConfig"].(map[string]any); ok {
		if value, ok := gen["maxOutputTokens"].(float64); ok && value > 0 {
			return int64(value)
		}
	}
	return 0
}

func legacyExtractImageCount(body []byte) int64 {
	const maxImages int64 = 32
	if len(body) == 0 {
		return 1
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return 1
	}
	for _, key := range []string{"n", "n_images", "num_images", "image_count"} {
		if value, ok := root[key]; ok {
			if count := legacyPositiveInt64(value); count > 0 {
				if count > maxImages {
					return maxImages
				}
				return count
			}
		}
	}
	return 1
}

func legacyPositiveInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int64(typed)
		}
	case int64:
		if typed > 0 {
			return typed
		}
	case int:
		if typed > 0 {
			return int64(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}
