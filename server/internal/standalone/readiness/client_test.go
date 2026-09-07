package readiness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckRejectsUnrelatedAndIncompleteBackend(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sign   bool
		pid    int
		ready  bool
		code   int
		wantOK bool
	}{
		{"ready", true, 42, true, 200, true},
		{"unsigned HTTP 200", false, 42, true, 200, false},
		{"different process", true, 43, true, 200, false},
		{"dependency incomplete", true, 42, false, 200, false},
		{"unavailable", true, 42, true, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				challenge := r.Header.Get(ChallengeHeader)
				if !VerifyRequest("secret", challenge, r.Header.Get(ProofHeader)) {
					t.Error("invalid request proof")
				}
				body, _ := json.Marshal(Status{BackendReady: true, DatabaseReady: true, RedisReady: tc.ready, AuthorizationReady: true, PID: tc.pid, SIPState: "unconfigured"})
				if tc.sign {
					w.Header().Set(ProofHeader, ResponseProof("secret", challenge, body))
				}
				w.WriteHeader(tc.code)
				_, _ = w.Write(body)
			}))
			defer srv.Close()
			state, err := Check(context.Background(), srv.Client(), srv.URL, "secret", 42)
			if (err == nil) != tc.wantOK {
				t.Fatalf("success=%v, err=%v", tc.wantOK, err)
			}
			if tc.wantOK && state.SIPState != "unconfigured" {
				t.Fatal("lost pending configuration state")
			}
		})
	}
}

func TestCheckDoesNotFollowRedirects(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer srv.Close()
	_, err := Check(context.Background(), srv.Client(), srv.URL, "secret", 42)
	if err == nil || reached {
		t.Fatalf("redirect followed=%v err=%v", reached, err)
	}
}
