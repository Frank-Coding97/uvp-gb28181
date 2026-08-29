package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMediaIdentityRoundTripPreservesSpecialCharactersInQuery(t *testing.T) {
	want := MediaIdentity{
		Schema: "rtsp",
		Vhost:  "__defaultVhost__",
		App:    "摄像头 / 北门",
		Stream: "主/码流 ? 01",
	}
	require.NoError(t, want.Validate())

	encoded, err := json.Marshal(want)
	require.NoError(t, err)
	var got MediaIdentity
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.Equal(t, want, got)

	query := want.QueryValues()
	require.Equal(t, want.App, query.Get("app"))
	require.Equal(t, want.Stream, query.Get("stream"))
	require.Contains(t, query.Encode(), "app=%E6%91%84%E5%83%8F%E5%A4%B4+%2F+%E5%8C%97%E9%97%A8")
	require.Contains(t, query.Encode(), "stream=%E4%B8%BB%2F%E7%A0%81%E6%B5%81+%3F+01")

	// The contract deliberately exposes query values only. A media identity
	// containing slash characters must never be treated as a URL path segment.
	_, err = url.ParseQuery(query.Encode())
	require.NoError(t, err)
}

func TestMediaIdentityValidationReturnsStableFieldErrors(t *testing.T) {
	tooLong := strings.Repeat("x", MaxMediaIdentityFieldLength+1)
	tests := []struct {
		name  string
		input MediaIdentity
		field string
	}{
		{name: "missing schema", input: MediaIdentity{Vhost: "vhost", App: "app", Stream: "stream"}, field: "schema"},
		{name: "invalid schema", input: MediaIdentity{Schema: "not a schema", Vhost: "vhost", App: "app", Stream: "stream"}, field: "schema"},
		{name: "missing vhost", input: MediaIdentity{Schema: "rtsp", App: "app", Stream: "stream"}, field: "vhost"},
		{name: "missing app", input: MediaIdentity{Schema: "rtsp", Vhost: "vhost", Stream: "stream"}, field: "app"},
		{name: "missing stream", input: MediaIdentity{Schema: "rtsp", Vhost: "vhost", App: "app"}, field: "stream"},
		{name: "long vhost", input: MediaIdentity{Schema: "rtsp", Vhost: tooLong, App: "app", Stream: "stream"}, field: "vhost"},
		{name: "control app", input: MediaIdentity{Schema: "rtsp", Vhost: "vhost", App: "app\nname", Stream: "stream"}, field: "app"},
		{name: "invalid utf8 stream", input: MediaIdentity{Schema: "rtsp", Vhost: "vhost", App: "app", Stream: string([]byte{'s', 0xff})}, field: "stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			require.Error(t, err)

			var validationErr *ValidationError
			require.True(t, errors.As(err, &validationErr))
			require.Contains(t, validationErr.Fields, tt.field)
			require.NotContains(t, validationErr.Error(), "\n")
			require.NotContains(t, validationErr.Error(), "\r")
		})
	}

	first := (MediaIdentity{Schema: "bad schema", Vhost: "", App: "", Stream: ""}).Validate()
	second := (MediaIdentity{Schema: "bad schema", Vhost: "", App: "", Stream: ""}).Validate()
	require.Equal(t, first.Error(), second.Error())
}

func TestManagementErrorMapsStableHTTPStatusAndRetryability(t *testing.T) {
	tests := []struct {
		name      string
		make      func() *ManagementError
		code      ManagementErrorCode
		status    int
		retryable bool
		nodeID    string
	}{
		{name: "offline", make: func() *ManagementError { return NewNodeOfflineError("node-offline", nil) }, code: CodeNodeOffline, status: http.StatusServiceUnavailable, retryable: true, nodeID: "node-offline"},
		{name: "not found", make: func() *ManagementError { return NewNodeNotFoundError("node-not-found", nil) }, code: CodeNodeNotFound, status: http.StatusNotFound, retryable: false, nodeID: "node-not-found"},
		{name: "forbidden", make: func() *ManagementError { return NewForbiddenError("node-forbidden", nil) }, code: CodeForbidden, status: http.StatusForbidden, retryable: false, nodeID: "node-forbidden"},
		{name: "maintenance", make: func() *ManagementError { return NewNodeMaintenanceError("node-maintenance", nil) }, code: CodeNodeMaintenance, status: http.StatusConflict, retryable: false, nodeID: "node-maintenance"},
		{name: "timeout", make: func() *ManagementError { return NewUpstreamTimeoutError("node-timeout", nil) }, code: CodeUpstreamTimeout, status: http.StatusGatewayTimeout, retryable: true, nodeID: "node-timeout"},
		{name: "unsupported", make: func() *ManagementError {
			return NewUnsupportedCapabilityError("node-unsupported", "getAllSession", nil)
		}, code: CodeUnsupportedCapability, status: http.StatusUnprocessableEntity, retryable: false, nodeID: "node-unsupported"},
		{name: "conflict", make: func() *ManagementError { return NewOwnershipConflictError("node-conflict", "resource is owned", nil) }, code: CodeOwnershipConflict, status: http.StatusConflict, retryable: false, nodeID: "node-conflict"},
		{name: "internal", make: func() *ManagementError { return NewInternalError("node-internal", "unexpected failure", nil) }, code: CodeInternal, status: http.StatusInternalServerError, retryable: false, nodeID: "node-internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.make()
			require.Equal(t, tt.code, got.Code)
			require.Equal(t, tt.status, got.HTTPStatus())
			require.Equal(t, tt.retryable, got.Retryable)
			require.Equal(t, tt.nodeID, got.NodeID)
			require.NotEmpty(t, got.Message)

			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(encoded, &body))
			require.Equal(t, string(tt.code), body["code"])
			require.Contains(t, body, "message")
			require.Equal(t, tt.nodeID, body["nodeId"])
			require.Equal(t, tt.retryable, body["retryable"])
		})
	}
}

func TestNormalizeErrorMapsWrappedCausesWithoutExposingCause(t *testing.T) {
	wrapped := errors.New("transport: secret=should-not-be-serialized")
	normalized := NormalizeError(fmt.Errorf("request failed: %w", wrapped), "node-1")
	managementErr, ok := AsManagementError(normalized)
	require.True(t, ok)
	require.Equal(t, CodeInternal, managementErr.Code)
	require.ErrorIs(t, normalized, wrapped)
	require.NotContains(t, managementErr.Error(), "should-not-be-serialized")

	timeout := NormalizeError(context.DeadlineExceeded, "node-2")
	timeoutErr, ok := AsManagementError(timeout)
	require.True(t, ok)
	require.Equal(t, CodeUpstreamTimeout, timeoutErr.Code)
	require.ErrorIs(t, timeout, context.DeadlineExceeded)
}

func TestPageContractNormalizesRequestAndBoundsSource(t *testing.T) {
	require.Equal(t, DefaultPageSize, (PageRequest{Page: 0, PageSize: 0}).Normalize().PageSize)
	require.Equal(t, 1, (PageRequest{Page: 0, PageSize: 0}).Normalize().Page)
	require.Equal(t, 100, (PageRequest{Page: 1, PageSize: 100}).Normalize().PageSize)
	require.Equal(t, MaxPageSize, (PageRequest{Page: 1, PageSize: MaxPageSize + 1}).Normalize().PageSize)

	items := make([]string, MaxResponseItems+17)
	for i := range items {
		items[i] = "stream-" + string(rune('a'+i%26))
	}
	page := Paginate(items, PageRequest{Page: 1, PageSize: 100})
	require.Equal(t, 1, page.Page)
	require.Equal(t, 100, page.PageSize)
	require.Len(t, page.List, 100)
	require.Equal(t, int64(MaxResponseItems), page.Total)
	require.True(t, page.Truncated)

	last := Paginate(items, PageRequest{Page: MaxResponseItems / MaxPageSize, PageSize: MaxPageSize})
	require.NotEmpty(t, last.List)
	require.True(t, last.Truncated)

	clamped := Paginate([]string{"one", "two"}, PageRequest{Page: 1, PageSize: MaxPageSize + 1})
	require.Equal(t, MaxPageSize, clamped.PageSize)
	require.False(t, clamped.Truncated, "clamping a page request does not claim the source list was truncated")
}

func TestManagementErrorAndDTORedaction(t *testing.T) {
	const secret = "super-secret-value"
	err := NewInternalError("node-1", `ZLM response {"code":-1,"msg":"denied"}; apiSecret=`+secret+` https://user:password@example.test/live?token=`+secret, nil)

	encoded, marshalErr := json.Marshal(err)
	require.NoError(t, marshalErr)
	text := string(encoded)
	require.NotContains(t, text, "apiSecret")
	require.NotContains(t, text, secret)
	require.NotContains(t, text, "user:password@")
	require.NotContains(t, text, "?token=")
	require.NotContains(t, text, `{"code":-1,"msg":"denied"}`)
	require.NotContains(t, err.Error(), "apiSecret")
	require.NotContains(t, err.Error(), secret)
	require.NotContains(t, err.Error(), "user:password@")

	identityJSON, marshalErr := json.Marshal(MediaIdentity{Schema: "rtsp", Vhost: "vhost", App: "app", Stream: "stream"})
	require.NoError(t, marshalErr)
	require.NotContains(t, string(identityJSON), "apiSecret")
	require.NotContains(t, string(identityJSON), "secret")
}
