package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// HostCall is the CGO host RPC. main sets this during init.
var HostCall func(method string, payload []byte) (response []byte, code int, err error)

// hostCallError preserves the HTTP status embedded in a host RPC error
// envelope (e.g. a nested after-auth 413) that a plain error string drops.
type hostCallError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *hostCallError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func hostCallHTTPStatus(err error) (int, bool) {
	var hce *hostCallError
	if errors.As(err, &hce) && hce.HTTPStatus > 0 {
		return hce.HTTPStatus, true
	}
	// Errors that self-report a status (e.g. terminatedRequestError) keep it.
	var sc interface{ StatusCode() int }
	if errors.As(err, &sc) && sc.StatusCode() > 0 {
		return sc.StatusCode(), true
	}
	return 0, false
}

// terminatedRequestError preserves the HTTP status while carrying the
// unwrapped nested-interceptor termination message to stream-close details.
type terminatedRequestError struct {
	msg    string
	status int
}

func (e *terminatedRequestError) Error() string   { return e.msg }
func (e *terminatedRequestError) StatusCode() int { return e.status }

// terminatedRequestMessage unwraps a nested-interceptor termination so the
// plugin-defined message reaches the client instead of the RPC prefix chain
// ("host_call_failed: request terminated by plugin").
func terminatedRequestMessage(err error) string {
	var hce *hostCallError
	if errors.As(err, &hce) && hce.Code == "host_call_failed" && strings.TrimSpace(hce.Message) != "" {
		return hce.Message
	}
	return err.Error()
}

func callHost(method string, payload any) (json.RawMessage, error) {
	if HostCall == nil {
		return nil, fmt.Errorf("host callback %s: not configured", method)
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal host callback %s: %w", method, err)
	}
	rawResponse, callCode, err := HostCall(method, rawPayload)
	if err != nil {
		return nil, err
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, callCode)
	}
	var env envelope
	if err := json.Unmarshal(rawResponse, &env); err != nil {
		return nil, fmt.Errorf("decode host envelope %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, &hostCallError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: env.Error.HTTPStatus}
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, callCode)
	}
	return append(json.RawMessage(nil), env.Result...), nil
}
