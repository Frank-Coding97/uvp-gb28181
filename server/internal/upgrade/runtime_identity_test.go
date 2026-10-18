package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRuntimeIdentity() RuntimeIdentity {
	return RuntimeIdentity{
		Version:       "1.2.3",
		BuildID:       "build-20261016",
		TaskID:        strings.Repeat("a", 32),
		DatabaseReady: true,
	}
}

func TestRuntimeIdentityProbeRequiresThreeMatchingCandidateResponses(t *testing.T) {
	expected := testRuntimeIdentity()
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != RuntimeIdentityPath {
			t.Fatalf("unexpected probe request: %s %s", r.Method, r.URL.Path)
		}
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1.2.3","build_id":"build-20261016","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true}`))
	}))
	defer server.Close()

	probe := RuntimeIdentityProbe{URL: server.URL + RuntimeIdentityPath, Attempts: 3, Interval: time.Millisecond, Timeout: time.Second}
	if err := probe.Validate(context.Background(), expected); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 3 {
		t.Fatalf("probe requests = %d, want 3", requests)
	}
}

func TestRuntimeIdentityProbeRejectsMismatchedCandidateAndMalformedResponse(t *testing.T) {
	expected := testRuntimeIdentity()
	for name, body := range map[string]string{
		"wrong-task":         `{"version":"1.2.3","build_id":"build-20261016","task_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","database_ready":true}`,
		"database-not-ready": `{"version":"1.2.3","build_id":"build-20261016","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":false}`,
		"unknown-field":      `{"version":"1.2.3","build_id":"build-20261016","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true,"unexpected":true}`,
		"multiple-values":    `{"version":"1.2.3","build_id":"build-20261016","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			probe := RuntimeIdentityProbe{URL: server.URL + RuntimeIdentityPath, Attempts: 1, Interval: time.Millisecond, Timeout: 10 * time.Millisecond}
			if err := probe.Validate(context.Background(), expected); err == nil {
				t.Fatal("invalid candidate response was accepted")
			}
		})
	}
}

func TestRuntimeIdentityProbeResetsConsecutiveMatchesAfterTransientFailure(t *testing.T) {
	expected := testRuntimeIdentity()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"version":"1.2.3","build_id":"build-20261016","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true}`))
	}))
	defer server.Close()

	probe := RuntimeIdentityProbe{URL: server.URL + RuntimeIdentityPath, Attempts: 3, Interval: time.Millisecond, Timeout: time.Second}
	if err := probe.Validate(context.Background(), expected); err != nil {
		t.Fatal(err)
	}
	if requests != 6 {
		t.Fatalf("probe requests = %d, want 6 after the transient failure reset", requests)
	}
}

func TestRuntimeIdentityProbeFailsFastForMismatchedIdentity(t *testing.T) {
	expected := testRuntimeIdentity()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"version":"1.2.3","build_id":"build-20261016","task_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","database_ready":true}`))
	}))
	defer server.Close()

	probe := RuntimeIdentityProbe{URL: server.URL + RuntimeIdentityPath, Attempts: 3, Interval: time.Second, Timeout: time.Second}
	if err := probe.Validate(context.Background(), expected); err == nil {
		t.Fatal("mismatched candidate identity was accepted")
	}
	if requests != 1 {
		t.Fatalf("probe requests = %d, want fail-fast after one mismatch", requests)
	}
}

func TestRuntimeIdentityProbeRejectsNonLoopbackAndNonCanonicalURLs(t *testing.T) {
	expected := testRuntimeIdentity()
	for _, rawURL := range []string{
		"https://127.0.0.1:8443" + RuntimeIdentityPath,
		"http://localhost:8080" + RuntimeIdentityPath,
		"http://127.0.0.1" + RuntimeIdentityPath,
		"http://127.0.0.1:8080/not-readiness",
		"http://127.0.0.1:8080" + RuntimeIdentityPath + "?next=/",
		"http://admin@127.0.0.1:8080" + RuntimeIdentityPath,
	} {
		probe := RuntimeIdentityProbe{URL: rawURL, Attempts: 1}
		if err := probe.Validate(context.Background(), expected); err == nil {
			t.Fatalf("unsafe readiness URL was accepted: %s", rawURL)
		}
	}
}
