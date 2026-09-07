package zlm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func rtpResourceFixture() RtpResourceOpenRequest {
	return RtpResourceOpenRequest{
		RtpResourceSelector: RtpResourceSelector{
			BootNonce: strings.Repeat("a", 32), ResourceID: "d1788750000000-" + strings.Repeat("b", 32),
			VHost: "__defaultVhost__", App: "rtp", Stream: "fixture-resource",
		},
		LocalIP: "127.0.0.1", SSRC: 12345,
	}
}

func newRtpResourceTLSFixture(t *testing.T, handler http.HandlerFunc) *OpenAPIRuntimeControl {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	control, err := NewOpenAPIRuntimeControl(node.Node{ID: 1, Revision: 1, MediaServerUUID: "fixture-node", APISecret: "fixture-secret"},
		OpenAPIControlTLS{Endpoint: server.URL + "/index/api", Roots: roots, SPKISHA256: sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	t.Cleanup(control.Close)
	return control
}

func TestOpenAPIRtpResourceIDIsRandomAndDeadlineBound(t *testing.T) {
	now := time.UnixMilli(1788750000000)
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := NewRtpResourceID(now)
		require.NoError(t, err)
		require.Regexp(t, `^d1788750025000-[0-9a-f]{32}$`, id)
		require.False(t, seen[id])
		seen[id] = true
	}
	id, err := newRtpResourceID(now, bytes.NewReader(make([]byte, 16)))
	require.NoError(t, err)
	require.Equal(t, "d1788750025000-"+strings.Repeat("0", 32), id)
	_, err = newRtpResourceID(now, bytes.NewReader(nil))
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = NewRtpResourceID(time.Time{})
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
}

func TestOpenAPIRtpResourceExactWireAndNonterminalResults(t *testing.T) {
	request := rtpResourceFixture()
	var response atomic.Value
	var calls atomic.Int32
	control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, []string{"fixture-secret"}, r.Header.Values("secret"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "no-store", r.Header.Get("Cache-Control"))
		require.Equal(t, 1, r.ProtoMajor)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var expected []byte
		switch r.URL.Path {
		case "/index/api/openRtpServerIfMatch":
			expected, err = json.Marshal(request)
		case "/index/api/closeRtpServerIfMatch":
			expected, err = json.Marshal(request.RtpResourceSelector)
		default:
			t.Errorf("unexpected identity probe or legacy request: %s", r.URL.Path)
		}
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(body))
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fields))
		require.NotContains(t, fields, "secret")
		require.NotContains(t, fields, "RtpResourceSelector")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, response.Load().(string))
	})
	for _, result := range []RtpResourceResult{
		RtpResourceCreated, RtpResourceExisting, RtpResourceClosePending, RtpResourceRetired,
		RtpResourceConflict, RtpResourceExpired, RtpResourceCapacity, RtpResourceRuntimeMismatch,
	} {
		data := `{"result":"` + string(result) + `"}`
		if result == RtpResourceCreated || result == RtpResourceExisting {
			data = `{"result":"` + string(result) + `","port":30000}`
		}
		response.Store(`{"code":0,"data":` + data + `}`)
		before := calls.Load()
		actual, err := control.OpenRtpServerIfMatch(context.Background(), request)
		require.NoError(t, err)
		require.Equal(t, result, actual.Result)
		require.Equal(t, before+1, calls.Load(), "no refresh/retry/fallback")
		if result == RtpResourceCreated || result == RtpResourceExisting {
			require.EqualValues(t, 30000, actual.Port)
		} else {
			require.Zero(t, actual.Port)
		}
	}
	for _, result := range []RtpResourceResult{
		RtpResourceShutdownScheduled, RtpResourceClosePending, RtpResourceNotActiveFenced,
		RtpResourceMismatch, RtpResourceRuntimeMismatch, RtpResourceCapacity,
	} {
		response.Store(`{"code":0,"data":{"result":"` + string(result) + `"}}`)
		before := calls.Load()
		actual, err := control.CloseRtpServerIfMatch(context.Background(), request.RtpResourceSelector)
		require.NoError(t, err)
		require.Equal(t, result, actual)
		require.Equal(t, before+1, calls.Load())
	}
}

func TestOpenAPIRtpResourceRejectsAmbiguousReplies(t *testing.T) {
	for _, data := range []string{
		`{"result":"closed"}`, `{"result":"not_found"}`, `{"result":null}`,
		`{"result":"close_pending","terminalRevision":1}`, `{"result":"created"}`,
		`{"result":"created","port":0}`, `{"result":"created","port":-1}`,
		`{"result":"created","port":65536}`, `{"result":"created","port":1.5}`,
		`{"result":"created","port":"30000"}`, `{"result":"created","port":null}`,
		`{"result":"close_pending","port":30000}`, `{"result":"close_pending","result":"closed"}`,
		`[]`, `null`, `{"result":"close_pending","extra":false}`,
	} {
		t.Run(data, func(t *testing.T) {
			var calls atomic.Int32
			control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Cache-Control", "no-store")
				_, _ = io.WriteString(w, `{"code":0,"data":`+data+`}`)
			})
			request := rtpResourceFixture()
			_, err := control.OpenRtpServerIfMatch(context.Background(), request)
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
			_, err = control.CloseRtpServerIfMatch(context.Background(), request.RtpResourceSelector)
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
			require.EqualValues(t, 2, calls.Load())
		})
	}
}

func TestOpenAPIRtpResourceInvalidInputNeverDispatches(t *testing.T) {
	var calls atomic.Int32
	control := newRtpResourceTLSFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	for index, change := range []func(*RtpResourceOpenRequest){
		func(r *RtpResourceOpenRequest) { r.BootNonce = "bad" },
		func(r *RtpResourceOpenRequest) { r.ResourceID += " " },
		func(r *RtpResourceOpenRequest) { r.ResourceID = strings.ToUpper(r.ResourceID) },
		func(r *RtpResourceOpenRequest) { r.VHost = "alias" },
		func(r *RtpResourceOpenRequest) { r.App = "a/b" },
		func(r *RtpResourceOpenRequest) { r.Stream = "" },
		func(r *RtpResourceOpenRequest) { r.Stream = strings.Repeat("a", 257) },
		func(r *RtpResourceOpenRequest) { r.LocalIP = "localhost" },
		func(r *RtpResourceOpenRequest) { r.Port = -1 },
		func(r *RtpResourceOpenRequest) { r.Port = 65535 },
		func(r *RtpResourceOpenRequest) { r.TCPMode = 2 },
		func(r *RtpResourceOpenRequest) { r.OnlyTrack = 3 },
	} {
		request := rtpResourceFixture()
		change(&request)
		_, err := control.OpenRtpServerIfMatch(context.Background(), request)
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		if index < 7 {
			_, err = control.CloseRtpServerIfMatch(context.Background(), request.RtpResourceSelector)
			require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		}
	}
	request := rtpResourceFixture()
	_, err := control.OpenRtpServerIfMatch(nil, request)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = control.CloseRtpServerIfMatch(nil, request.RtpResourceSelector)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	var absent *OpenAPIRuntimeControl
	_, err = absent.OpenRtpServerIfMatch(context.Background(), request)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = absent.CloseRtpServerIfMatch(context.Background(), request.RtpResourceSelector)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = control.OpenRtpServerIfMatch(cancelled, request)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	_, err = control.CloseRtpServerIfMatch(cancelled, request.RtpResourceSelector)
	require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
	require.Zero(t, calls.Load())
}

func TestOpenAPIRtpResourceNoRedirectOrLostResponseRetry(t *testing.T) {
	var escaped atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer target.Close()
	for _, lostResponse := range []bool{false, true} {
		var calls atomic.Int32
		control := newRtpResourceTLSFixture(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if lostResponse {
				connection, _, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				_ = connection.Close()
				return
			}
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		})
		request := rtpResourceFixture()
		_, err := control.OpenRtpServerIfMatch(context.Background(), request)
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		require.NotContains(t, err.Error(), "fixture-secret")
		_, err = control.CloseRtpServerIfMatch(context.Background(), request.RtpResourceSelector)
		require.ErrorIs(t, err, ErrRuntimeControlUnavailable)
		require.EqualValues(t, 2, calls.Load())
	}
	require.Zero(t, escaped.Load())
}
