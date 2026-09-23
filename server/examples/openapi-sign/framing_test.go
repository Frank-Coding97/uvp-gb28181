package openapisign

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignedRequestUsesExactContentLengthWithoutChunkedEncoding(t *testing.T) {
	fixture := loadFixture(t)
	caller := newHTTPExampleCaller(&http.Client{})
	caller.now = func() time.Time { return time.Unix(1700000000, 0) }
	caller.nonce = func() (string, error) { return strings.Repeat("a", 32), nil }
	body := []byte(`{"protocol":"https-flv"}`)

	request, err := caller.signedRequest(context.Background(), http.MethodPost, "https://example.invalid", "/openapi/v1/devices/34020000001320000001/channels/34020000001320000002/live-authorizations", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience, body)
	require.NoError(t, err)
	require.Equal(t, int64(len(body)), request.ContentLength)
	require.Empty(t, request.TransferEncoding)
	require.NotNil(t, request.GetBody)
	require.Equal(t, body, readRequestBody(t, request.GetBody))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))

	input := Request{
		Method:      request.Method,
		Path:        request.URL.Path,
		RawQuery:    request.URL.RawQuery,
		ContentType: request.Header.Get("Content-Type"),
		Body:        body,
		AccessKey:   request.Header.Get("X-UVP-Access-Key"),
		Timestamp:   request.Header.Get("X-UVP-Timestamp"),
		Nonce:       request.Header.Get("X-UVP-Nonce"),
		Audience:    fixture.Audience,
	}
	canonical, err := CanonicalString(input)
	require.NoError(t, err)
	require.Len(t, strings.Split(canonical, "\n"), 10)
	require.NotContains(t, canonical, "Content-Length")
	require.NoError(t, Verify(input, fixture.SecretKey, request.Header.Get("X-UVP-Signature")))
}

func TestPostTLSHTTP11SendsFixedLengthUnchangedBody(t *testing.T) {
	fixture := loadFixture(t)
	body := []byte(`{"protocol":"https-flv"}`)
	var observed struct {
		proto            string
		contentLength    int64
		transferEncoding []string
		transferHeader   []string
		contentEncoding  []string
		contentType      []string
		receivedBody     []byte
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed.proto = r.Proto
		observed.contentLength = r.ContentLength
		observed.transferEncoding = append([]string(nil), r.TransferEncoding...)
		observed.transferHeader = append([]string(nil), r.Header.Values("Transfer-Encoding")...)
		observed.contentEncoding = append([]string(nil), r.Header.Values("Content-Encoding")...)
		observed.contentType = append([]string(nil), r.Header.Values("Content-Type")...)
		var err error
		observed.receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read POST body: %v", err)
		}
		verifySignedRequest(t, fixture, r, observed.receivedBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = false
	server.StartTLS()
	defer server.Close()

	result, err := Post(context.Background(), server.Client(), server.URL, "/openapi/v1/devices/34020000001320000001/channels/34020000001320000002/live-authorizations", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, result.StatusCode)
	require.Equal(t, "HTTP/1.1", observed.proto)
	require.Equal(t, int64(len(body)), observed.contentLength)
	require.Empty(t, observed.transferEncoding)
	require.Empty(t, observed.transferHeader)
	require.Empty(t, observed.contentEncoding)
	require.Equal(t, []string{"application/json"}, observed.contentType)
	require.Equal(t, body, observed.receivedBody)
}

func TestPostDoesNotAutomaticallyRetryUnknownTransportResult(t *testing.T) {
	fixture := loadFixture(t)
	body := []byte(`{"protocol":"https-flv"}`)
	requests := 0
	client := &http.Client{Transport: framingRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return nil, context.DeadlineExceeded
	})}

	result, err := Post(context.Background(), client, "https://example.invalid", "/openapi/v1/devices/34020000001320000001/channels/34020000001320000002/live-authorizations", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience, body)
	require.Error(t, err)
	require.Equal(t, 1, result.Attempts)
	require.Equal(t, 1, requests)
}

type framingRoundTripFunc func(*http.Request) (*http.Response, error)

func (f framingRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func readRequestBody(t *testing.T, getBody func() (io.ReadCloser, error)) []byte {
	t.Helper()
	body, err := getBody()
	require.NoError(t, err)
	defer body.Close()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	return data
}
