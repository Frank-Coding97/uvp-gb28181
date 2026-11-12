package sipclient

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

type ListenerEndpoint struct {
	IP        string
	Port      int
	Transport string
}

type ListenerBinder func(ListenerEndpoint) (close func() error, err error)

type listenerEntry struct {
	refs  int
	close func() error
}

type ListenerRegistry struct {
	mu      sync.Mutex
	binder  ListenerBinder
	entries map[ListenerEndpoint]*listenerEntry
}

func NewListenerRegistry(binder ListenerBinder) *ListenerRegistry {
	return &ListenerRegistry{binder: binder, entries: make(map[ListenerEndpoint]*listenerEntry)}
}

func (r *ListenerRegistry) Acquire(endpoint ListenerEndpoint) (func() error, error) {
	if r == nil || r.binder == nil {
		return nil, fmt.Errorf("cascade listener binder is unavailable")
	}
	normalized, err := normalizeListenerEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if entry := r.entries[normalized]; entry != nil {
		entry.refs++
		r.mu.Unlock()
		return r.releaseFunc(normalized), nil
	}
	closeListener, err := r.binder(normalized)
	if err != nil {
		r.mu.Unlock()
		return nil, err
	}
	if closeListener == nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("cascade listener binder returned no close function")
	}
	r.entries[normalized] = &listenerEntry{refs: 1, close: closeListener}
	r.mu.Unlock()
	return r.releaseFunc(normalized), nil
}

func (r *ListenerRegistry) releaseFunc(endpoint ListenerEndpoint) func() error {
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			r.mu.Lock()
			entry := r.entries[endpoint]
			if entry == nil {
				r.mu.Unlock()
				return
			}
			entry.refs--
			if entry.refs > 0 {
				r.mu.Unlock()
				return
			}
			delete(r.entries, endpoint)
			closeListener := entry.close
			r.mu.Unlock()
			releaseErr = closeListener()
		})
		return releaseErr
	}
}

func normalizeListenerEndpoint(endpoint ListenerEndpoint) (ListenerEndpoint, error) {
	ip := net.ParseIP(strings.TrimSpace(endpoint.IP))
	if ip == nil || ip.To4() == nil || endpoint.Port < 1 || endpoint.Port > 65535 {
		return ListenerEndpoint{}, fmt.Errorf("invalid cascade listener endpoint")
	}
	transport := strings.ToUpper(strings.TrimSpace(endpoint.Transport))
	if transport != "UDP" && transport != "TCP" {
		return ListenerEndpoint{}, fmt.Errorf("invalid cascade listener transport")
	}
	return ListenerEndpoint{IP: ip.To4().String(), Port: endpoint.Port, Transport: transport}, nil
}
