package installationhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/internal/standalone/bootstrapcredential"
)

func TestStatusIsLoopbackOnlyAndReturnsRawPhase(t *testing.T) {
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error { return nil }, func(context.Context) error { return nil })
	if !fixture.handler.CredentialAccepted() || !fixture.handler.Ready() {
		t.Fatalf("initial coordinator state accepted=%v ready=%v", fixture.handler.CredentialAccepted(), fixture.handler.Ready())
	}
	response := fixture.request(http.MethodGet, "/api/standalone/setup/status", "", "127.0.0.1:32100", fixture.server.URL, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Standalone bool   `json:"standalone"`
		Phase      string `json:"phase"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Standalone || body.Phase != PhasePendingAdmin {
		t.Fatalf("status body = %+v", body)
	}
	if strings.Contains(response.Body.String(), fixture.token) {
		t.Fatal("status response leaked setup token")
	}

	for _, testCase := range []struct {
		name       string
		remoteAddr string
		host       string
		xForwarded string
		wantCode   int
	}{
		{name: "non-loopback with forged XFF", remoteAddr: "203.0.113.10:32100", host: fixture.host, xForwarded: "127.0.0.1", wantCode: http.StatusForbidden},
		{name: "host mismatch", remoteAddr: "127.0.0.1:32100", host: "localhost:32100", wantCode: http.StatusForbidden},
		{name: "ipv6 loopback", remoteAddr: "[::1]:32100", host: fixture.host, wantCode: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.request(http.MethodGet, "/api/standalone/setup/status", "", testCase.remoteAddr, fixture.server.URL, func(request *http.Request) {
				request.Host = testCase.host
				if testCase.xForwarded != "" {
					request.Header.Set("X-Forwarded-For", testCase.xForwarded)
				}
			})
			if response.Code != testCase.wantCode {
				t.Fatalf("status code = %d, want %d", response.Code, testCase.wantCode)
			}
		})
	}
}

func TestAdminRequiresExactLocalOriginAndSingleTokenHeader(t *testing.T) {
	var calls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error {
		calls.Add(1)
		return nil
	}, func(context.Context) error { return nil })
	validBody := `{"username":"admin","password":"strong-password"}`
	cases := []struct {
		name       string
		remoteAddr string
		origin     string
		host       string
		tokens     []string
		content    string
		wantCode   int
	}{
		{name: "remote with forged XFF", remoteAddr: "198.51.100.10:32100", origin: fixture.server.URL, host: fixture.host, tokens: []string{fixture.token}, content: "application/json", wantCode: http.StatusForbidden},
		{name: "missing origin", remoteAddr: "127.0.0.1:32100", host: fixture.host, tokens: []string{fixture.token}, content: "application/json", wantCode: http.StatusForbidden},
		{name: "different origin", remoteAddr: "127.0.0.1:32100", origin: "http://localhost:1", host: fixture.host, tokens: []string{fixture.token}, content: "application/json", wantCode: http.StatusForbidden},
		{name: "host mismatch", remoteAddr: "127.0.0.1:32100", origin: fixture.server.URL, host: "localhost:32100", tokens: []string{fixture.token}, content: "application/json", wantCode: http.StatusForbidden},
		{name: "duplicate token header", remoteAddr: "127.0.0.1:32100", origin: fixture.server.URL, host: fixture.host, tokens: []string{fixture.token, fixture.token}, content: "application/json", wantCode: http.StatusForbidden},
		{name: "wrong content type", remoteAddr: "127.0.0.1:32100", origin: fixture.server.URL, host: fixture.host, tokens: []string{fixture.token}, content: "text/plain", wantCode: http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", validBody, testCase.remoteAddr, testCase.origin, func(request *http.Request) {
				request.Host = testCase.host
				request.Header.Del("Content-Type")
				request.Header.Set("Content-Type", testCase.content)
				request.Header.Del(setupTokenHeader)
				for _, token := range testCase.tokens {
					request.Header.Add(setupTokenHeader, token)
				}
			})
			if response.Code != testCase.wantCode {
				t.Fatalf("status code = %d, want %d", response.Code, testCase.wantCode)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("createAdmin calls = %d, want 0", got)
	}
}

func TestAdminStrictBodyAndSizeErrorsDoNotLeakCredentials(t *testing.T) {
	secret := "password-with-sensitive-value"
	var calls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error {
		calls.Add(1)
		return nil
	}, func(context.Context) error { return nil })
	cases := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"username":"admin","password":"` + secret + `","extra":"x"}`},
		{name: "duplicate field", body: `{"username":"admin","username":"other","password":"` + secret + `"}`},
		{name: "trailing value", body: `{"username":"admin","password":"` + secret + `"} true`},
		{name: "invalid json", body: `{"username":"admin","password":"` + secret + `"`},
		{name: "missing password", body: `{"username":"admin"}`},
		{name: "empty username", body: `{"username":"   ","password":"` + secret + `"}`},
		{name: "wrong types", body: `{"username":7,"password":"` + secret + `"}`},
		{name: "oversize", body: `{"username":"admin","password":"` + strings.Repeat("x", adminBodyLimit) + `"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", testCase.body, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
				request.Header.Set("Content-Type", "application/json; charset=utf-8")
				request.Header.Set(setupTokenHeader, fixture.token)
			})
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), fixture.token) {
				t.Fatal("invalid request response leaked credential data")
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("createAdmin calls = %d, want 0", got)
	}
}

func TestAdminSuccessConsumesTokenAndMovesToPendingSIP(t *testing.T) {
	var username, password string
	var reloadCalls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(_ context.Context, gotUsername, gotPassword string) error {
		username, password = gotUsername, gotPassword
		return nil
	}, func(context.Context) error {
		reloadCalls.Add(1)
		return nil
	})
	response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if username != "admin" || password != "strong-password" {
		t.Fatalf("callback credentials = %q/%q", username, password)
	}
	if reloadCalls.Load() != 1 || !fixture.handler.CredentialAccepted() {
		t.Fatalf("reload calls = %d, accepted = %v", reloadCalls.Load(), fixture.handler.CredentialAccepted())
	}
	if fixture.handler.Phase() != PhasePendingSIP || !fixture.handler.Ready() || fixture.handler.AllowedPhase() != PhasePendingSIP {
		t.Fatalf("phase=%q ready=%v allowed=%q", fixture.handler.Phase(), fixture.handler.Ready(), fixture.handler.AllowedPhase())
	}
	var responseBody statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
		t.Fatal(err)
	}
	if responseBody.Phase != PhasePendingSIP || !responseBody.Standalone {
		t.Fatalf("success response = %+v", responseBody)
	}
	replay := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if replay.Code != http.StatusConflict && replay.Code != http.StatusForbidden {
		t.Fatalf("replay status = %d, want 409 or 403", replay.Code)
	}
	if err := fixture.handler.SetPhase(PhaseComplete); err != nil {
		t.Fatal(err)
	}
	if !fixture.handler.Ready() || fixture.handler.AllowedPhase() != PhaseComplete {
		t.Fatalf("ready=%v allowed=%q", fixture.handler.Ready(), fixture.handler.AllowedPhase())
	}
}

func TestAdminCallbackFailureKeepsTokenForRetry(t *testing.T) {
	transactionErr := errors.New("database transaction failed")
	var calls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error {
		if calls.Add(1) == 1 {
			return transactionErr
		}
		return nil
	}, func(context.Context) error { return nil })
	first := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if first.Code != http.StatusInternalServerError || strings.Contains(first.Body.String(), transactionErr.Error()) || strings.Contains(first.Body.String(), fixture.token) {
		t.Fatalf("first response = %d %q", first.Code, first.Body.String())
	}
	if !fixture.handler.CredentialAccepted() || fixture.handler.Phase() != PhasePendingAdmin || !fixture.handler.Ready() {
		t.Fatalf("failed transaction changed state: accepted=%v phase=%q", fixture.handler.CredentialAccepted(), fixture.handler.Phase())
	}
	second := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if second.Code != http.StatusCreated {
		t.Fatalf("retry status = %d, want %d", second.Code, http.StatusCreated)
	}
	if calls.Load() != 2 {
		t.Fatalf("createAdmin calls = %d, want 2", calls.Load())
	}
}

func TestAdminInputFailureReturnsBadRequestAndKeepsToken(t *testing.T) {
	var calls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error {
		if calls.Add(1) == 1 {
			return errors.Join(ErrInvalidInput, errors.New("password policy details"))
		}
		return nil
	}, func(context.Context) error { return nil })
	first := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if first.Code != http.StatusBadRequest || strings.Contains(first.Body.String(), "password policy details") || strings.Contains(first.Body.String(), fixture.token) {
		t.Fatalf("input failure response = %d %q", first.Code, first.Body.String())
	}
	second := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if second.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("retry status=%d callback calls=%d", second.Code, calls.Load())
	}
}

func TestConcurrentAdminSubmissionsConsumeOnce(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error {
		calls.Add(1)
		close(entered)
		<-release
		return nil
	}, func(context.Context) error { return nil })

	responses := make(chan int, 2)
	var wait sync.WaitGroup
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
				request.Header.Set(setupTokenHeader, fixture.token)
			})
			responses <- response.Code
		}()
	}
	<-entered
	close(release)
	wait.Wait()
	close(responses)
	created, rejected := 0, 0
	for status := range responses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict, http.StatusForbidden:
			rejected++
		default:
			t.Fatalf("unexpected concurrent response status %d", status)
		}
	}
	if created != 1 || rejected != 1 || calls.Load() != 1 {
		t.Fatalf("created=%d rejected=%d callbacks=%d", created, rejected, calls.Load())
	}
}

func TestReloadFailureLocksCurrentProcessUntilRestart(t *testing.T) {
	secret := "reload-secret-value"
	var reloadCalls atomic.Int32
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error { return nil }, func(context.Context) error {
		reloadCalls.Add(1)
		return errors.New("reload failed for " + secret)
	})
	response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), fixture.token) {
		t.Fatalf("reload failure response = %d %q", response.Code, response.Body.String())
	}
	if fixture.handler.Phase() != PhasePendingSIP || fixture.handler.AllowedPhase() != PhaseFailed || fixture.handler.Ready() || !fixture.handler.CredentialAccepted() {
		t.Fatalf("phase=%q allowed=%q ready=%v accepted=%v", fixture.handler.Phase(), fixture.handler.AllowedPhase(), fixture.handler.Ready(), fixture.handler.CredentialAccepted())
	}
	if err := fixture.handler.SetPhase(PhaseReady); !errors.Is(err, ErrPhaseLocked) {
		t.Fatalf("SetPhase after reload failure = %v, want ErrPhaseLocked", err)
	}
	replay := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) {
		request.Header.Set(setupTokenHeader, fixture.token)
	})
	if replay.Code != http.StatusServiceUnavailable {
		t.Fatalf("replay after reload failure = %d, want %d", replay.Code, http.StatusServiceUnavailable)
	}
	if reloadCalls.Load() != 1 {
		t.Fatalf("reload calls = %d, want 1", reloadCalls.Load())
	}
	status := fixture.request(http.MethodGet, "/api/standalone/setup/status", "", "127.0.0.1:32100", fixture.server.URL, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"phase":"pending_sip"`) {
		t.Fatalf("raw status after reload failure = %d %q", status.Code, status.Body.String())
	}
}

func TestNewInvalidatesVerifierOutsidePendingAdmin(t *testing.T) {
	for _, phase := range []string{PhasePendingSIP, PhaseComplete, PhaseFailed} {
		t.Run(phase, func(t *testing.T) {
			token, err := bootstrapcredential.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			verifier, err := bootstrapcredential.NewVerifier(token)
			if err != nil {
				t.Fatal(err)
			}
			fixture := newCoordinatorWithVerifierAndToken(t, phase, verifier, token, nil, nil)
			wantReady := phase != PhaseFailed
			if fixture.handler.Phase() != phase || !fixture.handler.CredentialAccepted() || fixture.handler.Ready() != wantReady {
				t.Fatalf("initial state phase=%q accepted=%v ready=%v", fixture.handler.Phase(), fixture.handler.CredentialAccepted(), fixture.handler.Ready())
			}
			if err := verifier.Use(token, func() error { return nil }); !errors.Is(err, bootstrapcredential.ErrInvalidCredential) {
				t.Fatalf("invalidated startup credential error = %v", err)
			}
		})
	}
}

func TestSetPhaseRejectsUnknownPhase(t *testing.T) {
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error { return nil }, func(context.Context) error { return nil })
	if err := fixture.handler.SetPhase("unknown"); !errors.Is(err, ErrInvalidPhase) {
		t.Fatalf("unknown phase error = %v", err)
	}
	if fixture.handler.Phase() != PhasePendingAdmin {
		t.Fatalf("phase changed after invalid SetPhase: %q", fixture.handler.Phase())
	}
}

func TestNewRejectsUnsafeBaseURLAndMissingPendingCallbacks(t *testing.T) {
	token, err := bootstrapcredential.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := bootstrapcredential.NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, baseURL := range []string{"", "not-a-url", "http://:1234", "http://127.0.0.1:bad-port", "http://127.0.0.1:1234/setup", "http://127.0.0.1:1234/?token=secret", "http://user:pass@127.0.0.1:1234"} {
		if _, err := New(baseURL, PhasePendingAdmin, verifier, func(context.Context, string, string) error { return nil }, func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("New(%q) error = %v, want ErrInvalidConfiguration", baseURL, err)
		}
	}
	if _, err := New("http://127.0.0.1:1234", PhasePendingAdmin, verifier, nil, func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("missing createAdmin error = %v", err)
	}
	if _, err := New("http://127.0.0.1:1234", PhasePendingAdmin, verifier, func(context.Context, string, string) error { return nil }, nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("missing reloadPolicy error = %v", err)
	}
}

type coordinatorFixture struct {
	engine  *gin.Engine
	server  *httptest.Server
	handler *Handler
	token   string
	host    string
}

func newCoordinator(t *testing.T, phase string, createAdmin func(context.Context, string, string) error, reloadPolicy func(context.Context) error) coordinatorFixture {
	token, err := bootstrapcredential.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := bootstrapcredential.NewVerifier(token)
	if err != nil {
		t.Fatal(err)
	}
	return newCoordinatorWithVerifierAndToken(t, phase, verifier, token, createAdmin, reloadPolicy)
}

func newCoordinatorWithVerifierAndToken(t *testing.T, phase string, verifier *bootstrapcredential.Verifier, token string, createAdmin func(context.Context, string, string) error, reloadPolicy func(context.Context) error) coordinatorFixture {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	server := httptest.NewUnstartedServer(engine)
	server.Start()
	t.Cleanup(server.Close)
	handler, err := New(server.URL, phase, verifier, createAdmin, reloadPolicy)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	handler.Register(engine)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return coordinatorFixture{engine: engine, server: server, handler: handler, token: token, host: parsed.Host}
}

func (fixture coordinatorFixture) request(method, path, body, remoteAddr, origin string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, fixture.server.URL+path, strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	request.Host = fixture.host
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	request.Header.Set("Content-Type", "application/json")
	if mutate != nil {
		mutate(request)
	}
	response := httptest.NewRecorder()
	fixture.engine.ServeHTTP(response, request)
	return response
}

func TestLoginAdmissionWaitsForPolicyReload(t *testing.T) {
	var handler *Handler
	fixture := newCoordinator(t, PhasePendingAdmin, func(context.Context, string, string) error { return nil }, func(context.Context) error {
		if handler.AllowedPhase() != PhasePendingAdmin {
			t.Error("login admission opened before policy reload finished")
		}
		return nil
	})
	handler = fixture.handler
	response := fixture.request(http.MethodPost, "/api/standalone/setup/admin", `{"username":"admin","password":"strong-password"}`, "127.0.0.1:32100", fixture.server.URL, func(request *http.Request) { request.Header.Set(setupTokenHeader, fixture.token) })
	if response.Code != http.StatusCreated || handler.AllowedPhase() != PhasePendingSIP {
		t.Fatal("policy completion did not open SIP setup")
	}
}
