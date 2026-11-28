package videoshareprobe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// These tests observe network bytes independently of any page/status polling.
func TestRelayRevocation(t *testing.T) {
	for _, mode := range []string{"revoke", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			upstream := continuousSource(t)
			ctx, cancel := context.WithCancel(context.Background())
			var deadline time.Time
			if mode == "deadline" {
				cancel()
				deadline = time.Now().Add(500 * time.Millisecond)
				ctx, cancel = context.WithDeadline(context.Background(), deadline)
			}
			defer cancel()
			done := make(chan error, 1)
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				done <- relay(ctx, w, upstream.URL)
			}))
			defer downstream.Close()
			client := &http.Client{Timeout: 4 * time.Second}
			resp, err := client.Get(downstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			first := make([]byte, 64)
			if _, err = io.ReadFull(resp.Body, first); err != nil {
				t.Fatalf("no media before cancellation: %v", err)
			}
			revoked := time.Now()
			if mode == "deadline" {
				revoked = deadline
			} else {
				cancel()
			}
			last := time.Now()
			var received int
			b := make([]byte, 4096)
			for {
				n, e := resp.Body.Read(b)
				if n > 0 {
					received += n
					last = time.Now()
				}
				if e != nil {
					break
				}
			}
			latency := last.Sub(revoked)
			if latency > 2*time.Second {
				t.Fatalf("receiver got bytes %s after invalidation", latency)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("handler leaked")
			}
			t.Logf("receiver last-byte lag=%s, bytes after first chunk=%d", latency, received)
			if got := resp.Header.Get("Cache-Control"); got != "no-store, private" {
				t.Fatalf("cache policy %q", got)
			}
			if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
				t.Fatalf("buffering policy %q", got)
			}
		})
	}
}

func TestRelayInterruptsBlockedUpstream(t *testing.T) {
	started := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_ = http.NewResponseController(w).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer source.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { done <- relay(ctx, w, source.URL) }))
	defer downstream.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	response := make(chan error, 1)
	go func() {
		resp, err := client.Get(downstream.URL)
		if resp != nil {
			resp.Body.Close()
		}
		response <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt blocked upstream Read")
	}
	<-response
}

func TestRelayInterruptsBlockedDownstream(t *testing.T) {
	var writes atomic.Int64
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 32*1024)
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			writes.Add(1)
		}
	}))
	defer source.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { done <- relay(ctx, w, source.URL) }))
	defer downstream.Close()
	conn, err := net.Dial("tcp", downstream.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(1024)
	}
	_, err = fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: probe\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	// The receiver deliberately never reads and never closes until after assertion.
	time.Sleep(150 * time.Millisecond)
	if writes.Load() == 0 {
		t.Fatal("upstream never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt blocked downstream Write")
	}
}

func TestRelayRejectsRedirectAndExpiredContext(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	if err := relay(context.Background(), httptest.NewRecorder(), redirect.URL); err == nil {
		t.Fatal("redirect accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("followed upstream redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := relay(ctx, httptest.NewRecorder(), target.URL); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired authorization: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid authorization contacted upstream")
	}
}

func TestRelayDoesNotForwardPrivateHeaders(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "private=secret")
		w.Header().Set("Location", "http://private/media")
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = io.WriteString(w, "FLV")
	}))
	defer source.Close()
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = relay(r.Context(), w, source.URL) }))
	defer downstream.Close()
	resp, err := http.Get(downstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "FLV" {
		t.Fatalf("body=%q", body)
	}
	for _, key := range []string{"Set-Cookie", "Location"} {
		if resp.Header.Get(key) != "" {
			t.Fatalf("leaked %s", key)
		}
	}
}

func continuousSource(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
				if _, err := w.Write(bytes.Repeat([]byte("f"), 1024)); err != nil {
					return
				}
				if http.NewResponseController(w).Flush() != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(s.Close)
	return s
}
