package openapisign

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type observedRequest struct {
	timestamp string
	nonce     string
	signature string
}

func TestOpenAPIHTTPExampleGETRetries429And503WithFreshSignatures(t *testing.T) {
	fixture := loadFixture(t)
	for _, retryStatus := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(retryStatus), func(t *testing.T) {
			count := 0
			observed := make([]observedRequest, 0, 2)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				verifySignedRequest(t, fixture, r, body)
				observed = append(observed, observedRequest{
					timestamp: r.Header.Get("X-UVP-Timestamp"),
					nonce:     r.Header.Get("X-UVP-Nonce"),
					signature: r.Header.Get("X-UVP-Signature"),
				})
				count++
				if count == 1 {
					w.WriteHeader(retryStatus)
					_, _ = w.Write([]byte("retry"))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			}))

			caller := newHTTPExampleCaller(server.Client())
			caller.wait = func(context.Context, time.Duration) error { return nil }
			clockValue := int64(1700000000)
			caller.now = func() time.Time {
				value := clockValue
				clockValue++
				return time.Unix(value, 0)
			}
			nonces := []string{strings.Repeat("1", 32), strings.Repeat("2", 32)}
			caller.nonce = func() (string, error) {
				value := nonces[0]
				nonces = nonces[1:]
				return value, nil
			}

			result, err := caller.get(context.Background(), server.URL, "/openapi/v1/devices", "page=1", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
			if err != nil {
				t.Fatalf("GET returned error: %v", err)
			}
			if result.StatusCode != http.StatusOK || string(result.Body) != "ok" || result.Attempts != 2 {
				t.Fatalf("GET result = %#v, want final 200/ok after two attempts", result)
			}
			if count != 2 || len(observed) != 2 {
				t.Fatalf("GET attempts = %d, want 2", count)
			}
			if observed[0].timestamp == observed[1].timestamp || observed[0].nonce == observed[1].nonce || observed[0].signature == observed[1].signature {
				t.Fatal("retry reused timestamp, nonce, or signature")
			}
			server.Close()
		})
	}
}

func TestOpenAPIHTTPExampleGET401DoesNotRetry(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		verifySignedRequest(t, fixture, r, body)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))

	result, err := Get(context.Background(), server.Client(), server.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil {
		t.Fatalf("GET returned error: %v", err)
	}
	if result.StatusCode != http.StatusUnauthorized || result.Attempts != 1 || count != 1 {
		t.Fatalf("401 GET result = %#v, count=%d, want one non-retried attempt", result, count)
	}
	server.Close()
}

func TestOpenAPIHTTPExampleGETTimeoutRetriesOnceWithFreshSignature(t *testing.T) {
	fixture := loadFixture(t)
	observed := make([]observedRequest, 0, 2)
	count := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		count++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		verifySignedRequest(t, fixture, r, body)
		observed = append(observed, observedRequest{
			timestamp: r.Header.Get("X-UVP-Timestamp"),
			nonce:     r.Header.Get("X-UVP-Nonce"),
			signature: r.Header.Get("X-UVP-Signature"),
		})
		if count == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})
	caller := newHTTPExampleCaller(&http.Client{Transport: transport})
	clockValue := int64(1700000000)
	caller.now = func() time.Time {
		value := clockValue
		clockValue++
		return time.Unix(value, 0)
	}
	nonces := []string{strings.Repeat("3", 32), strings.Repeat("4", 32)}
	caller.nonce = func() (string, error) {
		value := nonces[0]
		nonces = nonces[1:]
		return value, nil
	}

	result, err := caller.get(context.Background(), "https://example.invalid", "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil {
		t.Fatalf("timeout GET returned error: %v", err)
	}
	if result.StatusCode != http.StatusOK || string(result.Body) != "ok" || result.Attempts != 2 || count != 2 {
		t.Fatalf("timeout GET result = %#v, count=%d, want final 200 after two attempts", result, count)
	}
	if observed[0].timestamp == observed[1].timestamp || observed[0].nonce == observed[1].nonce || observed[0].signature == observed[1].signature {
		t.Fatal("timeout retry reused timestamp, nonce, or signature")
	}
}

func TestOpenAPIHTTPExampleClientTimeoutIsBoundedInCopy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input time.Duration
		want  time.Duration
	}{
		{name: "zero", input: 0, want: defaultHTTPTimeout},
		{name: "too-long", input: defaultHTTPTimeout + time.Second, want: defaultHTTPTimeout},
		{name: "short", input: time.Second, want: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Timeout: tc.input}
			caller := newHTTPExampleCaller(client)
			if caller.client.Timeout != tc.want {
				t.Fatalf("copied timeout = %s, want %s", caller.client.Timeout, tc.want)
			}
			if client.Timeout != tc.input {
				t.Fatalf("caller mutated input client timeout to %s", client.Timeout)
			}
		})
	}
}

func TestOpenAPIHTTPExampleRejectsOversizedResponseBodyAndClosesIt(t *testing.T) {
	fixture := loadFixture(t)
	closed := false
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: &trackingReadCloser{
				Reader: strings.NewReader(strings.Repeat("x", maxHTTPResponseBytes+1)),
				closed: &closed,
			},
		}, nil
	})
	caller := newHTTPExampleCaller(&http.Client{Transport: transport})
	result, err := caller.get(context.Background(), "https://example.invalid", "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err == nil || result.Attempts != 1 || !closed {
		t.Fatalf("oversized response = %#v, err=%v, closed=%v, want one generic failure and a closed body", result, err, closed)
	}
}

func TestOpenAPIHTTPExampleGETHonorsRetryAfter(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	waits := make([]time.Duration, 0, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		verifySignedRequest(t, fixture, r, body)
		if count == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	caller := newHTTPExampleCaller(server.Client())
	caller.wait = func(ctx context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	result, err := caller.get(context.Background(), server.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil || result.StatusCode != http.StatusOK || result.Attempts != 2 || count != 2 {
		t.Fatalf("Retry-After result = %#v, err=%v, count=%d, want two attempts ending 200", result, err, count)
	}
	if len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("Retry-After waits = %v, want [1s]", waits)
	}
}

func TestOpenAPIHTTPExampleGETTooLongRetryAfterReturnsOriginalResponse(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	waitCalled := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("retry later"))
	}))
	defer server.Close()

	caller := newHTTPExampleCaller(server.Client())
	caller.wait = func(context.Context, time.Duration) error {
		waitCalled = true
		return nil
	}
	result, err := caller.get(context.Background(), server.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil || result.StatusCode != http.StatusTooManyRequests || result.Attempts != 1 || count != 1 || waitCalled {
		t.Fatalf("long Retry-After result = %#v, err=%v, count=%d, waitCalled=%v, want original 429 without wait/retry", result, err, count, waitCalled)
	}
}

func TestOpenAPIHTTPExampleGETRetryWaitHonorsContextCancellation(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("retry later"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	caller := newHTTPExampleCaller(server.Client())
	caller.wait = func(ctx context.Context, delay time.Duration) error {
		cancel()
		return waitContext(ctx, delay)
	}
	result, err := caller.get(ctx, server.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if !errors.Is(err, context.Canceled) || result.StatusCode != http.StatusTooManyRequests || result.Attempts != 1 || count != 1 {
		t.Fatalf("canceled Retry-After result = %#v, err=%v, count=%d, want canceled one-attempt response", result, err, count)
	}
}

func TestOpenAPIHTTPExampleGETRetryBoundStopsAfterTwoFailures(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	}))
	defer server.Close()

	caller := newHTTPExampleCaller(server.Client())
	caller.wait = func(context.Context, time.Duration) error { return nil }
	result, err := caller.get(context.Background(), server.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil || result.StatusCode != http.StatusServiceUnavailable || result.Attempts != 2 || count != 2 {
		t.Fatalf("two failed GET responses = %#v, err=%v, count=%d, want exactly two attempts", result, err, count)
	}
}

func TestOpenAPIHTTPExampleRejectsPlainHTTP(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		count++
		return nil, errors.New("transport must not be called")
	})}
	result, err := Get(context.Background(), client, "http://example.invalid", "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err == nil || result.Attempts != 1 || count != 0 {
		t.Fatalf("plain HTTP result = %#v, err=%v, transport calls=%d, want rejection before transport", result, err, count)
	}
}

func TestOpenAPIHTTPExamplePOSTDoesNotRetryStatusResponses(t *testing.T) {
	fixture := loadFixture(t)
	body := []byte(`{"protocol":"https-flv"}`)
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			count := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				requestBody, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				if string(requestBody) != string(body) || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("POST body/content type changed: %q/%q", requestBody, r.Header.Get("Content-Type"))
				}
				verifySignedRequest(t, fixture, r, requestBody)
				w.WriteHeader(status)
				_, _ = w.Write([]byte("result"))
			}))

			result, err := Post(context.Background(), server.Client(), server.URL, "/openapi/v1/devices/34020000001320000001/channels/34020000001320000002/live-authorizations", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience, body)
			if err != nil {
				t.Fatalf("POST returned error: %v", err)
			}
			if result.StatusCode != status || result.Attempts != 1 || count != 1 {
				t.Fatalf("POST status %d result = %#v, count=%d, want one attempt", status, result, count)
			}
			server.Close()
		})
	}
}

func TestOpenAPIHTTPExamplePOSTUnknownResultDoesNotRetry(t *testing.T) {
	fixture := loadFixture(t)
	count := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		count++
		return nil, context.DeadlineExceeded
	})
	caller := newHTTPExampleCaller(&http.Client{Transport: transport})
	result, err := caller.post(context.Background(), "https://example.invalid", "/openapi/v1/devices/34020000001320000001/channels/34020000001320000002/live-authorizations", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience, []byte(`{"protocol":"https-flv"}`))
	if err == nil || result.Attempts != 1 || count != 1 {
		t.Fatalf("unknown POST result = %#v, err=%v, count=%d, want one failed attempt", result, err, count)
	}
}

func TestOpenAPIHTTPExampleRejectsRedirectWithoutForwardingSignature(t *testing.T) {
	fixture := loadFixture(t)
	redirected := 0
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected++
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/openapi/v1/devices", http.StatusFound)
	}))
	defer source.Close()

	result, err := Get(context.Background(), source.Client(), source.URL, "/openapi/v1/devices", "", fixture.AccessKey, fixture.SecretKey, fixture.Audience)
	if err != nil {
		t.Fatalf("redirect GET returned error: %v", err)
	}
	if result.StatusCode != http.StatusFound || result.Attempts != 1 || redirected != 0 {
		t.Fatalf("redirect result = %#v, destination requests=%d, want one local response and no cross-host follow", result, redirected)
	}
}

func verifySignedRequest(t *testing.T, fixture fixture, request *http.Request, body []byte) {
	t.Helper()
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
	if err := Verify(input, fixture.SecretKey, request.Header.Get("X-UVP-Signature")); err != nil {
		t.Errorf("request signature invalid: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingReadCloser struct {
	io.Reader
	closed *bool
}

func (r *trackingReadCloser) Close() error {
	*r.closed = true
	return nil
}
