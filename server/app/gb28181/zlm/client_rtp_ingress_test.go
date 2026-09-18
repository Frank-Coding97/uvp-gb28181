package zlm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIRtpIngressV2ExactIdentityAndSeparateResult(t *testing.T) {
	target := rtpResourceFixture().RtpResourceSelector
	var response atomic.Value
	var calls atomic.Int32
	control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/index/api/closeRtpIngressIfMatchV2", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, []string{"fixture-secret"}, r.Header.Values("secret"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		expected, err := json.Marshal(target)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(body))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, response.Load().(string))
	})
	for _, result := range []RtpIngressResult{RtpIngressDrained, RtpIngressShutdownScheduled, RtpIngressClosePending,
		RtpIngressNotActiveFenced, RtpIngressMismatch, RtpIngressRuntimeMismatch, RtpIngressCapacity} {
		response.Store(`{"code":0,"data":{"result":"` + string(result) + `"}}`)
		before := calls.Load()
		actual, err := control.CloseRtpIngressIfMatchV2(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, result, actual)
		require.Equal(t, before+1, calls.Load(), "one fixed request, no retry/probe/fallback")
	}
}

func TestOpenAPIRtpIngressV2RejectsWholeResourceAndAmbiguousClaims(t *testing.T) {
	for _, data := range []string{`{"result":"closed"}`, `{"result":"device_complete"}`, `{"result":"not_found"}`,
		`{"result":"rtp_ingress_drained","closed":true}`, `{"result":"rtp_ingress_drained","terminalRevision":1}`,
		`{"result":"rtp_ingress_drained","result":"rtp_ingress_drained"}`, `{"result":null}`,
		`{"result":true}`, `{"result":"created"}`, `null`, `[]`, `{}`} {
		t.Run(data, func(t *testing.T) {
			var calls atomic.Int32
			control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Cache-Control", "no-store")
				_, _ = io.WriteString(w, `{"code":0,"data":`+data+`}`)
			})
			result, err := control.CloseRtpIngressIfMatchV2(context.Background(), rtpResourceFixture().RtpResourceSelector)
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
			require.Empty(t, result)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestOpenAPIRtpIngressV2InvalidInputAndMissingAPINeverFallback(t *testing.T) {
	var calls atomic.Int32
	control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/index/api/closeRtpIngressIfMatchV2", r.URL.Path)
		http.NotFound(w, r)
	})
	target := rtpResourceFixture().RtpResourceSelector
	var absent *OpenAPIRuntimeControl
	_, err := absent.CloseRtpIngressIfMatchV2(context.Background(), target)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = control.CloseRtpIngressIfMatchV2(nil, target)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	bad := target
	bad.BootNonce = "bad"
	_, err = control.CloseRtpIngressIfMatchV2(context.Background(), bad)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = control.CloseRtpIngressIfMatchV2(cancelled, target)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.Zero(t, calls.Load())
	_, err = control.CloseRtpIngressIfMatchV2(context.Background(), target)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenAPIRtpIngressV2LostReplyNeverRedispatches(t *testing.T) {
	var calls atomic.Int32
	control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		_ = connection.Close()
	})
	result, err := control.CloseRtpIngressIfMatchV2(context.Background(), rtpResourceFixture().RtpResourceSelector)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.Empty(t, result)
	require.EqualValues(t, 1, calls.Load())
}
