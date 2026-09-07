//go:build windows

package launcher

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const (
	t18AdmissionInstallEnv      = "UVP_T18_ADMISSION_INSTALL_DIR"
	t18AdmissionRetryInstallEnv = "UVP_T18_ADMISSION_RETRY_INSTALL_DIR"
)

// TestWindowsStandaloneT18AdmissionConcurrentAdminSubmissions exercises the
// real loopback HTTP endpoint and the durable SQLite first-use transaction.
// The fixture must be new: the test owns the launcher and its component
// processes until the cleanup hook returns.
func TestWindowsStandaloneT18AdmissionConcurrentAdminSubmissions(t *testing.T) {
	fixture := t18AdmissionStart(t, t18AdmissionInstallEnv)

	credentials := []struct {
		username string
		password string
	}{
		{username: "t18-admission-a", password: t18RandomPassword(t)},
		{username: "t18-admission-b", password: t18RandomPassword(t)},
	}
	requestBodies := make([][]byte, len(credentials))
	for i, credential := range credentials {
		requestBodies[i] = t18AdminBody(t, credential.username, credential.password)
	}

	start := make(chan struct{})
	results := make([]t18AdmissionHTTPResult, len(credentials))
	var group sync.WaitGroup
	group.Add(len(credentials))
	for i := range credentials {
		go func(index int) {
			defer group.Done()
			<-start
			results[index] = t18AdmissionRequest(
				fixture.client.client,
				fixture.client.baseURL,
				fixture.client.origin,
				http.MethodPost,
				"/api/standalone/setup/admin",
				fixture.token,
				requestBodies[index],
				"")
		}(i)
	}
	close(start)
	group.Wait()

	successes := 0
	winner := -1
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent administrator request %d failed", i)
		}
		t18AdmissionAssertResponseSafe(t, result.headers, result.body, append([]string{fixture.token}, credentialPasswords(credentials)...)...)
		switch result.status {
		case http.StatusCreated:
			successes++
			winner = i
		case http.StatusConflict, http.StatusForbidden, http.StatusServiceUnavailable:
		default:
			t.Fatalf("concurrent administrator request %d returned unexpected HTTP status %d", i, result.status)
		}
	}
	if successes != 1 || winner < 0 {
		t.Fatalf("concurrent administrator submissions succeeded %d times, want exactly once", successes)
	}

	// Login is an application-level cross-check that the exact winning payload
	// is the only account persisted. The SQLite count below is the durable check.
	for i := range credentials {
		body := requestBodies[i]
		result := t18AdmissionRequest(
			fixture.client.client,
			fixture.client.baseURL,
			fixture.client.origin,
			http.MethodPost,
			"/api/login",
			"",
			body,
			"")
		if result.err != nil {
			t.Fatalf("administrator login request %d failed", i)
		}
		t18AdmissionAssertResponseSafe(t, result.headers, result.body, append([]string{fixture.token}, credentialPasswords(credentials)...)...)
		loggedIn := result.status == http.StatusOK
		if loggedIn != (i == winner) {
			t.Fatalf("administrator login result %d did not match the winning setup submission", i)
		}
	}

	if count := t18AdmissionUserCount(t, fixture.paths.DatabasePath); count != 1 {
		t.Fatalf("SQLite sys_users row count = %d, want exactly one administrator", count)
	}
}

// TestWindowsStandaloneT18AdmissionWrongTokenDoesNotConsumeAndIgnoresXFF
// checks that a canonical token with the same length is rejected without
// consuming the real token, while a forged forwarding header cannot change the
// actual loopback peer policy.
func TestWindowsStandaloneT18AdmissionWrongTokenDoesNotConsumeAndIgnoresXFF(t *testing.T) {
	fixture := t18AdmissionStart(t, t18AdmissionRetryInstallEnv)
	password := t18RandomPassword(t)
	body := t18AdminBody(t, "t18-admission-retry", password)
	wrongToken := t18AdmissionDifferentToken(t, fixture.token)
	if len(wrongToken) != len(fixture.token) || wrongToken == fixture.token {
		t.Fatal("wrong admission token did not preserve canonical token length")
	}

	wrong := t18AdmissionRequest(
		fixture.client.client,
		fixture.client.baseURL,
		fixture.client.origin,
		http.MethodPost,
		"/api/standalone/setup/admin",
		wrongToken,
		body,
		"127.0.0.1")
	if wrong.err != nil {
		t.Fatal("same-length wrong-token request failed")
	}
	t18AdmissionAssertResponseSafe(t, wrong.headers, wrong.body, fixture.token, wrongToken, password)
	if wrong.status != http.StatusForbidden {
		t.Fatalf("same-length wrong-token request returned HTTP %d, want %d", wrong.status, http.StatusForbidden)
	}

	valid := t18AdmissionRequest(
		fixture.client.client,
		fixture.client.baseURL,
		fixture.client.origin,
		http.MethodPost,
		"/api/standalone/setup/admin",
		fixture.token,
		body,
		"203.0.113.7")
	if valid.err != nil {
		t.Fatal("valid-token request with forged X-Forwarded-For failed")
	}
	t18AdmissionAssertResponseSafe(t, valid.headers, valid.body, fixture.token, wrongToken, password)
	if valid.status != http.StatusCreated {
		t.Fatalf("valid-token request with forged X-Forwarded-For returned HTTP %d, want %d", valid.status, http.StatusCreated)
	}
	t18AssertSetupStatus(t, valid.body, true, "pending_sip")

	if count := t18AdmissionUserCount(t, fixture.paths.DatabasePath); count != 1 {
		t.Fatalf("SQLite sys_users row count = %d after token retry, want exactly one administrator", count)
	}
}

type t18AdmissionFixture struct {
	client t18HTTPClient
	paths  standalone.Paths
	run    *t18Launch
	token  string
}

type t18AdmissionHTTPResult struct {
	status  int
	headers http.Header
	body    []byte
	err     error
}

func t18AdmissionStart(t *testing.T, envName string) t18AdmissionFixture {
	t.Helper()
	installDir := strings.TrimSpace(os.Getenv(envName))
	if installDir == "" {
		t.Skip("requires an explicitly prepared fresh t18-setup fixture in " + envName)
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil || release.Version != "t18-setup" {
		t.Fatal("admission test requires the isolated t18-setup release")
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir:  installDir,
		ConfigDir:   filepath.Join(installDir, "config"),
		ResourceDir: release.ResourceDir,
		WebDir:      release.WebDir,
		DataDir:     filepath.Join(installDir, "data"),
	})
	if err != nil {
		t.Fatal("admission test could not resolve fixture paths")
	}
	if _, err := os.Stat(paths.DatabasePath); err == nil {
		t.Fatal("admission test requires a fresh fixture without an existing database")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("admission test could not inspect the fixture database")
	}

	browserURLs := make(chan string, 1)
	run := t18Start(t, installDir, browserURLs)
	t.Cleanup(func() {
		run.cancel()
		t18WaitFinished(t, run, 90*time.Second)
	})
	entry, ok := t18WaitBrowserEntry(run, browserURLs, 90*time.Second)
	if !ok {
		t.Fatal("admission fixture did not publish a browser entry")
	}
	baseURL, origin, token, ok := t18BrowserEndpoint(entry, true)
	if !ok {
		t.Fatal("admission fixture published an invalid loopback bootstrap URL")
	}
	client := t18HTTPClient{client: t18HTTPTransport(), baseURL: baseURL, origin: origin}
	status := t18AdmissionRequest(client.client, client.baseURL, client.origin, http.MethodGet, "/api/standalone/setup/status", "", nil, "")
	if status.err != nil {
		t.Fatal("admission fixture status request failed")
	}
	t18AdmissionAssertResponseSafe(t, status.headers, status.body, token)
	if status.status != http.StatusOK {
		t.Fatalf("initial admission setup status = %d, want %d", status.status, http.StatusOK)
	}
	t18AssertSetupStatus(t, status.body, true, "pending_admin")
	return t18AdmissionFixture{client: client, paths: paths, run: run, token: token}
}

func t18AdmissionRequest(client *http.Client, baseURL, origin, method, path, token string, body []byte, forwardedFor string) t18AdmissionHTTPResult {
	request, err := http.NewRequest(method, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return t18AdmissionHTTPResult{err: err}
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if token != "" {
		request.Header.Set("X-UVP-Setup-Token", token)
	}
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return t18AdmissionHTTPResult{err: err}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return t18AdmissionHTTPResult{err: errors.New("response body unavailable")}
	}
	return t18AdmissionHTTPResult{status: response.StatusCode, headers: response.Header.Clone(), body: raw}
}

func t18AdmissionAssertResponseSafe(t *testing.T, headers http.Header, body []byte, secrets ...string) {
	t.Helper()
	values := []string{string(body)}
	for _, entries := range headers {
		values = append(values, entries...)
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, value := range values {
			if strings.Contains(value, secret) {
				t.Fatal("admission response exposed a credential")
			}
		}
	}
}

func credentialPasswords(credentials []struct {
	username string
	password string
}) []string {
	secrets := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		secrets = append(secrets, credential.password)
	}
	return secrets
}

func t18AdmissionDifferentToken(t *testing.T, token string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) == 0 {
		t.Fatal("could not decode bootstrap token for negative test")
	}
	raw[0] ^= 1
	return base64.RawURLEncoding.EncodeToString(raw)
}

func t18AdmissionUserCount(t *testing.T, path string) int64 {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(path)
	if err != nil {
		t.Fatal("could not open the standalone SQLite database for a read-only count")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal("could not obtain the standalone SQLite connection")
	}
	defer raw.Close()
	var count int64
	if err := db.Unscoped().Table("sys_users").Count(&count).Error; err != nil {
		t.Fatal("could not count standalone administrators")
	}
	return count
}
