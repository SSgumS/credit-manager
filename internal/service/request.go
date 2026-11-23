package service

import (
	"github.com/yuluo688/credit-manager/internal/requestmeta"
)

func estimateTokens(body []byte, defaultOutput, maxTotal int64) (input, output int64) {
	return estimateTokensWithLimit(body, extractOutputLimit(body), defaultOutput, maxTotal)
}

// estimateTokensWithLimit applies the reserve ceiling to an already extracted
// protocol output limit.
func estimateTokensWithLimit(body []byte, explicitOutput, defaultOutput, maxTotal int64) (input, output int64) {
	output = defaultOutput
	if body == nil {
		input = 1
	} else {
		// Conservative character-based upper bound: 1 token per rune-ish byte pair, floor 1.
		input = int64(len(body)/2) + 1
	}
	// Prefer explicit protocol limits when present.
	if explicitOutput > 0 {
		output = explicitOutput
	}
	if input > maxTotal {
		input = maxTotal
	}
	if output > maxTotal {
		output = maxTotal
	}
	if input+output > maxTotal {
		// Keep output preference and shrink input.
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

// requestMetadata decodes the body once, on first use, so paths that reject a
// request before reading any field (a supplied model that is explicitly
// unpriced) never touch the body. The body stays unmodified while it is in use.
type requestMetadata struct {
	body   []byte
	fields requestmeta.Metadata
	loaded bool
}

func readRequestMetadata(body []byte) *requestMetadata {
	return &requestMetadata{body: body}
}

func (m *requestMetadata) get() requestmeta.Metadata {
	if !m.loaded {
		m.fields = requestmeta.Read(m.body)
		m.loaded = true
	}
	return m.fields
}

func (m *requestMetadata) model() string {
	return m.get().String("model")
}

func (m *requestMetadata) serviceTier() string {
	return m.get().String("service_tier", "tier")
}

// outputLimit mirrors the legacy scan: the first positive *float* wins and its
// truncated value is returned as-is, so 0.1 stops the scan with 0 rather than
// falling through to a later key.
func (m *requestMetadata) outputLimit() int64 {
	fields := m.get()
	for _, key := range []string{"max_tokens", "max_output_tokens", "max_completion_tokens"} {
		if limit, ok := fields.Float(key); ok && limit > 0 {
			return int64(limit)
		}
	}
	if limit, ok := fields.Object("generationConfig").Float("maxOutputTokens"); ok && limit > 0 {
		return int64(limit)
	}
	return 0
}

// imageCount mirrors the legacy scan: a key whose truncated count is not
// positive is skipped, so n=0.1 falls through to n_images.
func (m *requestMetadata) imageCount() int64 {
	const maxImages int64 = 32
	fields := m.get()
	for _, key := range []string{"n", "n_images", "num_images", "image_count"} {
		if value, ok := fields.Float(key); ok && value > 0 {
			if count := int64(value); count > 0 {
				if count > maxImages {
					return maxImages
				}
				return count
			}
		}
	}
	return 1
}

func extractModel(body []byte) string {
	return readRequestMetadata(body).model()
}

func extractServiceTier(body []byte) string {
	return readRequestMetadata(body).serviceTier()
}

func extractOutputLimit(body []byte) int64 {
	return readRequestMetadata(body).outputLimit()
}
