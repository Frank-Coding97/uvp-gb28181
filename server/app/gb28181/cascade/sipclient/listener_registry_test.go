package sipclient

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestListenerRegistrySharesAndReleasesEndpoints(t *testing.T) {
	var binds atomic.Int32
	var closes atomic.Int32
	registry := NewListenerRegistry(func(endpoint ListenerEndpoint) (func() error, error) {
		binds.Add(1)
		return func() error { closes.Add(1); return nil }, nil
	})
	endpoint := ListenerEndpoint{IP: "192.0.2.20", Port: 5060, Transport: "udp"}
	releaseA, err := registry.Acquire(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	releaseB, err := registry.Acquire(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if binds.Load() != 1 {
		t.Fatalf("binds=%d", binds.Load())
	}
	if err = releaseA(); err != nil || closes.Load() != 0 {
		t.Fatalf("first release err=%v closes=%d", err, closes.Load())
	}
	if err = releaseA(); err != nil || closes.Load() != 0 {
		t.Fatalf("duplicate release err=%v closes=%d", err, closes.Load())
	}
	if err = releaseB(); err != nil || closes.Load() != 1 {
		t.Fatalf("last release err=%v closes=%d", err, closes.Load())
	}
}

func TestListenerRegistryIsolatesBindingFailures(t *testing.T) {
	bindErr := errors.New("address in use")
	registry := NewListenerRegistry(func(endpoint ListenerEndpoint) (func() error, error) {
		if endpoint.Port == 5060 {
			return nil, bindErr
		}
		return func() error { return nil }, nil
	})
	if _, err := registry.Acquire(ListenerEndpoint{IP: "192.0.2.20", Port: 5060, Transport: "UDP"}); !errors.Is(err, bindErr) {
		t.Fatalf("err=%v", err)
	}
	release, err := registry.Acquire(ListenerEndpoint{IP: "192.0.2.20", Port: 5061, Transport: "TCP"})
	if err != nil {
		t.Fatalf("unrelated endpoint failed: %v", err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
}
