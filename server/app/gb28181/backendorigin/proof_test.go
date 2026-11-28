package backendorigin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixtureTarget() Target {
	return Target{NodeID: 1, UUID: "node", MediaGeneration: 1, Revision: 2, EndpointFingerprint: "endpoint", App: "rtp", Stream: "stream"}
}

func TestProofUsesAuthenticatedObservedIPAndRejectsReplay(t *testing.T) {
	r := New(1)
	target := fixtureTarget()
	var nonce string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		nonce = req.URL.Query().Get(QueryParameter)
		require.Len(t, nonce, 64)
		require.Empty(t, req.URL.Query().Get("play_token"))
		require.NoError(t, r.Observe(req.Context(), nonce, target.NodeID, target.UUID, target.App, target.Stream, "203.0.113.42"))
		require.Error(t, r.Observe(req.Context(), nonce, target.NodeID, target.UUID, target.App, target.Stream, "203.0.113.42"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	ip, err := r.Prove(context.Background(), target, srv.URL, func(context.Context, Target) error { return nil })
	require.NoError(t, err)
	require.Equal(t, "203.0.113.42", ip)
	require.Error(t, r.Observe(context.Background(), nonce, 1, "node", "rtp", "stream", ip))
}

func TestProofRejectsMismatchConfigChangeAndAccidentalAdmission(t *testing.T) {
	for _, kind := range []string{"uuid", "app", "stream", "node", "ip", "config", "admission"} {
		t.Run(kind, func(t *testing.T) {
			r := New(1)
			target := fixtureTarget()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				actual := target
				ip := "203.0.113.42"
				switch kind {
				case "uuid":
					actual.UUID = "other"
				case "app":
					actual.App = "other"
				case "stream":
					actual.Stream = "other"
				case "node":
					actual.NodeID = 2
				case "ip":
					ip = "invalid"
				}
				err := r.Observe(req.Context(), req.URL.Query().Get(QueryParameter), actual.NodeID, actual.UUID, actual.App, actual.Stream, ip)
				if kind != "admission" {
					require.Error(t, err)
				}
				if kind == "admission" {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer srv.Close()
			_, err := r.Prove(context.Background(), target, srv.URL, func(context.Context, Target) error {
				if kind == "config" {
					return errors.New("changed")
				}
				return nil
			})
			require.Error(t, err)
		})
	}
}

func TestProofBoundsCancellationAndClose(t *testing.T) {
	r := New(1)
	target := fixtureTarget()
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { close(entered); <-release; w.WriteHeader(401) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Prove(ctx, target, srv.URL, func(context.Context, Target) error { return nil })
		done <- err
	}()
	<-entered
	_, err := r.Prove(context.Background(), target, srv.URL, func(context.Context, Target) error { return nil })
	require.Error(t, err)
	r.Close()
	close(release)
	select {
	case err = <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("close failed to release probe")
	}
	_, err = r.Prove(context.Background(), target, srv.URL, func(context.Context, Target) error { return nil })
	require.Error(t, err)
}

func TestProofDeadlineRemovesPendingReceipt(t *testing.T) {
	r := New(1)
	defer r.Close()
	nonceCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		nonceCh <- req.URL.Query().Get(QueryParameter)
		<-req.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := r.Prove(ctx, fixtureTarget(), srv.URL, func(context.Context, Target) error { return nil })
	require.Error(t, err)
	require.Error(t, r.Observe(context.Background(), <-nonceCh, 1, "node", "rtp", "stream", "203.0.113.42"))
	r.mu.Lock()
	require.Empty(t, r.pending)
	r.mu.Unlock()
}

func TestProofRevalidatesAfterReceiptAndRejectsRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed-after-receipt", true: "redirect"}[redirect], func(t *testing.T) {
			r := New(1)
			defer r.Close()
			target := fixtureTarget()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				require.NoError(t, r.Observe(req.Context(), req.URL.Query().Get(QueryParameter), 1, "node", "rtp", "stream", "203.0.113.42"))
				if redirect {
					w.Header().Set("Location", "/elsewhere")
					w.WriteHeader(http.StatusFound)
				} else {
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer srv.Close()
			_, err := r.Prove(context.Background(), target, srv.URL, func(context.Context, Target) error {
				calls++
				if calls >= 3 {
					return errors.New("changed after receipt")
				}
				return nil
			})
			require.Error(t, err)
		})
	}
}
