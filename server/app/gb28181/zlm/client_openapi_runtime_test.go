package zlm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIRuntimeControlHeaderAndConditionalResult(t *testing.T) {
	boot := strings.Repeat("a", 32)
	for _, result := range []string{"shutdown_scheduled", "not_found", "runtime_mismatch"} {
		t.Run(result, func(t *testing.T) {
			var calls atomic.Int32
			c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Empty(t, r.URL.RawQuery)
				require.Equal(t, []string{"test-secret"}, r.Header.Values("secret"))
				require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
				w.Header().Set("Cache-Control", "no-store")
				if r.URL.Path == "/index/api/getRuntimeIdentity" {
					require.Equal(t, "GET", r.Method)
					_, _ = io.WriteString(w, `{"code":0,"data":{"protocolVersion":1,"bootNonce":"`+boot+`"}}`)
					return
				}
				require.Equal(t, "/index/api/kick_session_if_match", r.URL.Path)
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, map[string]string{"bootNonce": boot, "id": "12-9"}, body)
				_, _ = io.WriteString(w, `{"code":0,"data":{"result":"`+result+`"}}`)
			})
			defer server.Close()
			identity, err := c.GetRuntimeIdentity(context.Background())
			require.NoError(t, err)
			require.Equal(t, boot, identity.BootNonce)
			require.Equal(t, 1, identity.ProtocolVersion)
			outcome, err := c.KickSessionIfMatch(context.Background(), boot, "12-9")
			require.NoError(t, err)
			require.Equal(t, ConditionalKickResult(result), outcome)
			require.EqualValues(t, 2, calls.Load(), "no identity refresh or legacy retry")
		})
	}
}

func TestOpenAPIRuntimeControlRejectsMalformedAndUnavailable(t *testing.T) {
	for _, body := range []string{
		`{"data":{"protocolVersion":1,"bootNonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"code":0,"code":0,"data":{"protocolVersion":1,"bootNonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"code":0,"data":{"protocolVersion":1,"bootNonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bootNonce":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`,
		`{"code":0,"data":{"protocolVersion":2,"bootNonce":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
		`{"code":0,"data":{"protocolVersion":1,"bootNonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}`,
		`{"code":-1,"msg":"test-secret"}`,
		`{"code":0,"data":null}`, `[]`, strings.Repeat("test-secret", 4096),
	} {
		c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/index/api/getRuntimeIdentity", r.URL.Path)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, body)
		})
		_, err := c.GetRuntimeIdentity(context.Background())
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		require.NotContains(t, err.Error(), "test-secret")
		server.Close()
	}
	for _, status := range []int{401, 404, 500, 503} {
		var calls atomic.Int32
		c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) })
		_, err := c.KickSessionIfMatch(context.Background(), strings.Repeat("a", 32), "1-5")
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		require.EqualValues(t, 1, calls.Load())
		server.Close()
	}
}

func TestOpenAPIRuntimeControlNeverFollowsRedirectOrRetries(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	c, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	defer server.Close()
	_, err := c.GetRuntimeIdentity(context.Background())
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.Zero(t, leaked.Load())
	c.http.Timeout = 10 * time.Millisecond
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	_, err = c.KickSessionIfMatch(context.Background(), strings.Repeat("a", 32), "1-5")
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = c.KickSessionIfMatch(context.Background(), "invalid", "1-5")
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
}
