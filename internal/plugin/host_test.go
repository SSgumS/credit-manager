package plugin

import (
	"fmt"
	"testing"
)

func TestTerminatedRequestMessage(t *testing.T) {
	// Unwrapped host termination: message surfaces without the RPC prefix.
	terminated := &hostCallError{Code: "host_call_failed", Message: "request body exceeds the request budget after permitted trimming", HTTPStatus: 413}
	if got := terminatedRequestMessage(terminated); got != "request body exceeds the request budget after permitted trimming" {
		t.Fatalf("got %q", got)
	}
	// Any host_call_failed unwraps to its message; the helper is only used in
	// the 413 branch, where the error is always a nested-interceptor
	// termination.
	other := &hostCallError{Code: "host_call_failed", Message: "connection reset", HTTPStatus: 502}
	if got := terminatedRequestMessage(other); got != "connection reset" {
		t.Fatalf("got %q", got)
	}
	// Non-host errors pass through unchanged.
	plain := fmt.Errorf("wrap: %w", &hostCallError{Code: "", Message: "plain failure"})
	if got := terminatedRequestMessage(plain); got != "wrap: plain failure" {
		t.Fatalf("got %q", got)
	}
}
