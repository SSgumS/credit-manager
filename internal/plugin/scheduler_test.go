package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/yuluo688/credit-manager/internal/service"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func configureSchedulerTestService(t *testing.T, extraYAML string) {
	t.Helper()
	service.Shutdown()
	yaml := "data_dir: \"" + strings.ReplaceAll(t.TempDir(), "\\", "\\\\") + "\"\n" + extraYAML
	if err := service.Configure(context.Background(), []byte(yaml)); err != nil {
		t.Fatalf("configure service: %v", err)
	}
	t.Cleanup(service.Shutdown)
}

func schedulerPickRequestBody(model, reservation string, extraHeaders map[string][]string, candidates ...pluginapi.SchedulerAuthCandidate) []byte {
	headers := map[string][]string{}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	if reservation != "" {
		headers[reservationHeader] = []string{reservation}
	}
	body, err := json.Marshal(pluginapi.SchedulerPickRequest{
		Model:     model,
		Options:   pluginapi.SchedulerOptions{Headers: headers},
		Candidates: candidates,
	})
	if err != nil {
		panic(err)
	}
	return body
}

func decodePickResponse(t *testing.T, raw []byte) pluginapi.SchedulerPickResponse {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("pickAuth error envelope: %+v", env.Error)
	}
	var resp pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("decode pick response: %v", err)
	}
	return resp
}

func kimiCandidate(id string) pluginapi.SchedulerAuthCandidate {
	return pluginapi.SchedulerAuthCandidate{ID: id, Provider: "kimi", Status: "active"}
}

func claudeCandidate(id string) pluginapi.SchedulerAuthCandidate {
	return pluginapi.SchedulerAuthCandidate{ID: id, Provider: "claude", Status: "active"}
}

func exclusionHeader() map[string][]string {
	return map[string][]string{excludeProvidersHeader: {"kimi,kimi-ai,kimi.ai,kimi.com"}}
}

func TestPickAuthExclusionDisabledByDefault(t *testing.T) {
	configureSchedulerTestService(t, "")
	raw, err := pickAuth(schedulerPickRequestBody("model", "res-1", exclusionHeader(), kimiCandidate("kimi-1"), claudeCandidate("claude-1")))
	if err != nil {
		t.Fatalf("pickAuth: %v", err)
	}
	resp := decodePickResponse(t, raw)
	if resp.Reject {
		t.Fatalf("unexpected reject: %+v", resp)
	}
	// With the gate off the header is inert: both candidates stay eligible and
	// either pick is legitimate. The enabled counterpart asserts kimi is
	// actually filtered.
	if resp.AuthID != "kimi-1" && resp.AuthID != "claude-1" {
		t.Fatalf("unexpected pick %q", resp.AuthID)
	}
}

func TestPickAuthExclusionRequiresReservation(t *testing.T) {
	configureSchedulerTestService(t, "scheduler:\n  allow_provider_exclusions: true\n")
	raw, err := pickAuth(schedulerPickRequestBody("model", "", exclusionHeader(), kimiCandidate("kimi-1"), claudeCandidate("claude-1")))
	if err != nil {
		t.Fatalf("pickAuth: %v", err)
	}
	resp := decodePickResponse(t, raw)
	if resp.Reject {
		t.Fatalf("unexpected reject: %+v", resp)
	}
	// Without a trusted reservation the header is ignored: kimi stays eligible.
	// Selection between the two is implementation-defined; it must not reject.
	if resp.AuthID != "kimi-1" && resp.AuthID != "claude-1" {
		t.Fatalf("unexpected pick %q", resp.AuthID)
	}
}

func TestPickAuthExcludesKimiWithReservation(t *testing.T) {
	configureSchedulerTestService(t, "scheduler:\n  allow_provider_exclusions: true\n")
	raw, err := pickAuth(schedulerPickRequestBody("model", "res-9", exclusionHeader(), kimiCandidate("kimi-1"), claudeCandidate("claude-1")))
	if err != nil {
		t.Fatalf("pickAuth: %v", err)
	}
	resp := decodePickResponse(t, raw)
	if resp.Reject {
		t.Fatalf("unexpected reject: %+v", resp)
	}
	if resp.AuthID != "claude-1" {
		t.Fatalf("kimi not excluded, picked %q", resp.AuthID)
	}
}

func TestPickAuthAllExcludedFirstPickRetainsCandidates(t *testing.T) {
	configureSchedulerTestService(t, "scheduler:\n  allow_provider_exclusions: true\n")
	raw, err := pickAuth(schedulerPickRequestBody("model", "res-first", exclusionHeader(), kimiCandidate("kimi-1")))
	if err != nil {
		t.Fatalf("pickAuth: %v", err)
	}
	resp := decodePickResponse(t, raw)
	if resp.Reject {
		t.Fatalf("first pick must not reject; after-auth should 413 instead: %+v", resp)
	}
	if resp.AuthID != "kimi-1" {
		t.Fatalf("first pick should retain unfiltered candidates, got %q", resp.AuthID)
	}
}

func TestPickAuthAllExcludedRetryRejects(t *testing.T) {
	configureSchedulerTestService(t, "scheduler:\n  allow_provider_exclusions: true\n")
	svc := service.Current()
	ctx := context.Background()
	// Simulate the completed first attempt: a reservation captured an auth pick
	// (the non-Kimi candidate CPA already tried).
	svc.TrackAuthCapture("res-retry", "model")
	if _, _, err := svc.PickAuthForRequest(ctx, "model", "", "res-retry", []service.AuthPickCandidate{
		{ID: "claude-1", Provider: "claude", Status: "active"},
	}); err != nil {
		t.Fatalf("seed prior pick: %v", err)
	}
	if !svc.HasPriorAuthPick("res-retry") {
		t.Fatal("HasPriorAuthPick false after seeded pick")
	}
	// Retry pick: only kimi remains, exclusion still active.
	raw, err := pickAuth(schedulerPickRequestBody("model", "res-retry", exclusionHeader(), kimiCandidate("kimi-1")))
	if err != nil {
		t.Fatalf("pickAuth: %v", err)
	}
	resp := decodePickResponse(t, raw)
	if !resp.Reject {
		t.Fatalf("retry pick must reject instead of routing to kimi: %+v", resp)
	}
	if resp.RejectCode != "provider_excluded" {
		t.Fatalf("reject code = %q, want provider_excluded", resp.RejectCode)
	}
}

func TestParseExcludedProviders(t *testing.T) {
	cases := []struct {
		name    string
		headers http.Header
		want    []string
	}{
		{name: "empty", headers: http.Header{}, want: nil},
		{
			name:    "single",
			headers: http.Header{excludeProvidersHeader: {"kimi"}},
			want:    []string{"kimi"},
		},
		{
			name:    "csv with spaces and case",
			headers: http.Header{excludeProvidersHeader: {" Kimi , KIMI-AI, ,kimi.ai "}},
			want:    []string{"kimi", "kimi-ai", "kimi.ai"},
		},
		{
			name: "repeated header values",
			headers: http.Header{
				excludeProvidersHeader:          {"kimi", "claude"},
				strings.ToLower(excludeProvidersHeader): {"codex"},
			},
			want: []string{"kimi", "claude", "codex"},
		},
		{
			name:    "unrelated headers ignored",
			headers: http.Header{"X-Other": {"kimi"}},
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseExcludedProviders(tc.headers)
			if len(got) != len(tc.want) {
				t.Fatalf("parseExcludedProviders = %v, want %v", got, tc.want)
			}
			for _, w := range tc.want {
				if _, ok := got[w]; !ok {
					t.Fatalf("missing %q in %v", w, got)
				}
			}
		})
	}
}

func TestForwardHeadersStripsProviderExclusions(t *testing.T) {
	in := http.Header{
		excludeProvidersHeader: {"kimi"},
		"X-Other":             {"keep"},
	}
	out := forwardHeaders(in, nil, "res-42")
	if got := out.Values(excludeProvidersHeader); len(got) != 0 {
		t.Fatalf("client-supplied exclusion header leaked: %v", got)
	}
	if got := out.Get("X-Other"); got != "keep" {
		t.Fatalf("unrelated header dropped: %v", out)
	}
}

func TestCallHostErrorCarriesHTTPStatus(t *testing.T) {
	old := HostCall
	t.Cleanup(func() { HostCall = old })
	HostCall = func(method string, payload []byte) ([]byte, int, error) {
		return []byte(`{"ok":false,"error":{"code":"host_call_failed","message":"request terminated by plugin","http_status":413}}`), 0, nil
	}
	_, err := callHost("test.method", map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	status, ok := hostCallHTTPStatus(err)
	if !ok || status != http.StatusRequestEntityTooLarge {
		t.Fatalf("hostCallHTTPStatus = %d,%t; err=%v", status, ok, err)
	}
	var hce *hostCallError
	if !errors.As(err, &hce) {
		t.Fatalf("error type %T, want *hostCallError", err)
	}
}

func TestErrorEnvelopeWithStatus(t *testing.T) {
	raw := errorEnvelopeWithStatus("request_too_large", "too big", http.StatusRequestEntityTooLarge)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.OK || env.Error == nil {
		t.Fatalf("expected error envelope: %s", raw)
	}
	if env.Error.HTTPStatus != http.StatusRequestEntityTooLarge {
		t.Fatalf("http_status = %d", env.Error.HTTPStatus)
	}
	if env.Error.Code != "request_too_large" {
		t.Fatalf("code = %q", env.Error.Code)
	}
}

func TestClosePluginStreamErrorDetail(t *testing.T) {
	old := HostCall
	t.Cleanup(func() { HostCall = old })
	var gotMethod string
	var gotPayload []byte
	HostCall = func(method string, payload []byte) ([]byte, int, error) {
		gotMethod, gotPayload = method, payload
		return []byte(`{"ok":true,"result":{}}`), 0, nil
	}
	closePluginStreamError("stream-1", &hostCallError{Code: "host_call_failed", Message: "too big", HTTPStatus: http.StatusRequestEntityTooLarge})
	if gotMethod != pluginabi.MethodHostStreamClose {
		t.Fatalf("method = %q", gotMethod)
	}
	var req struct {
		Error       string           `json:"error"`
		ErrorDetail *pluginabi.Error `json:"error_detail"`
	}
	if err := json.Unmarshal(gotPayload, &req); err != nil {
		t.Fatalf("decode close payload: %v", err)
	}
	if req.ErrorDetail == nil {
		t.Fatalf("missing error_detail: %s", gotPayload)
	}
	if req.ErrorDetail.HTTPStatus != http.StatusRequestEntityTooLarge {
		t.Fatalf("error_detail.http_status = %d", req.ErrorDetail.HTTPStatus)
	}
}
