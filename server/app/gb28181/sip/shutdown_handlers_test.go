package sip

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	siplib "github.com/emiago/sipgo/sip"
)

func TestShutdownWaitsAcceptedSIPHandler(t *testing.T) {
	s := &Server{}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	wrapped := s.drainHandler(func(*siplib.Request, siplib.ServerTransaction) { close(entered); <-release })
	go func() { defer close(finished); wrapped(nil, nil) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handler not drained: %v", err)
	}
	tx := &quiesceTransaction{}
	wrapped(siplib.NewRequest(siplib.MESSAGE, siplib.Uri{Host: "127.0.0.1"}), tx)
	if tx.response == nil || tx.response.StatusCode != 503 {
		t.Fatal("new handler entered during shutdown")
	}
	close(release)
	<-finished
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownClosesEstablishedTCPConnection(t *testing.T) {
	s, err := NewServer(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer s.Shutdown(ctx)
	var conn net.Conn
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:15060", 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A framed OPTIONS request and its response prove the connection was accepted
	// into the SIP transport pool before shutdown.
	request := "OPTIONS sip:test@127.0.0.1 SIP/2.0\r\nVia: SIP/2.0/TCP 127.0.0.1:5061;branch=z9hG4bK-shutdown\r\nFrom: <sip:test@127.0.0.1>;tag=shutdown\r\nTo: <sip:test@127.0.0.1>\r\nCall-ID: shutdown-transport\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n"
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err = conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	if _, err = conn.Read(buffer); err != nil {
		t.Fatal(err)
	}
	if err = s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, err = conn.Read(buffer)
	if err == nil {
		t.Fatal("established connection remained readable")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("established TCP transport remained open after Shutdown")
	}
}
