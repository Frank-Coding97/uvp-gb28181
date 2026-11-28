// Package backendorigin proves the address observed by the media server without
// authorizing the probe to read media. Every probe must be denied by on_play.
package backendorigin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

const QueryParameter = "uvp_origin_probe"

var ErrUnavailable = errors.New("backend media origin proof unavailable")

type Target struct {
	NodeID              int64
	UUID                string
	Revision            uint64
	MediaGeneration     uint64
	EndpointFingerprint string
	App, Stream         string
}

type pending struct {
	ctx      context.Context
	target   Target
	validate func(context.Context, Target) error
	result   chan string
	consumed bool
}

type Registry struct {
	mu      sync.Mutex
	pending map[string]*pending
	max     int
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	client  *http.Client
}

func New(max int) *Registry {
	if max < 1 {
		max = 32
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{pending: make(map[string]*pending), max: max, ctx: ctx, cancel: cancel, client: &http.Client{
		Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (r *Registry) Close() {
	r.mu.Lock()
	r.closed = true
	r.cancel()
	r.mu.Unlock()
	r.client.CloseIdleConnections()
}

func (r *Registry) Prove(ctx context.Context, target Target, rawURL string, validate func(context.Context, Target) error) (string, error) {
	if target.NodeID <= 0 || target.UUID == "" || target.MediaGeneration == 0 || target.App == "" || target.Stream == "" || validate == nil {
		return "", ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	if validate(ctx, target) != nil {
		return "", ErrUnavailable
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", ErrUnavailable
	}
	nonce := hex.EncodeToString(bytes[:])
	p := &pending{ctx: ctx, target: target, validate: validate, result: make(chan string, 1)}
	r.mu.Lock()
	if r.closed || len(r.pending) >= r.max {
		r.mu.Unlock()
		return "", ErrUnavailable
	}
	r.pending[nonce] = p
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, nonce); r.mu.Unlock() }()
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil {
		return "", ErrUnavailable
	}
	q := u.Query()
	q.Del(playauth.QueryParameter)
	q.Set(QueryParameter, nonce)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", ErrUnavailable
	}
	response, err := r.client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	response.Body.Close()
	// A valid receipt never grants access. A node which serves the probe anyway
	// has not enforced the expected hook contract and cannot establish a proof.
	if response.StatusCode != http.StatusUnauthorized {
		return "", ErrUnavailable
	}
	select {
	case ip := <-p.result:
		if ip == "" || ctx.Err() != nil || validate(ctx, target) != nil {
			return "", ErrUnavailable
		}
		return ip, nil
	default:
		return "", ErrUnavailable
	}
}

// Observe is called only after the existing hook credential middleware has
// authenticated the node. The fresh validator checks configuration and current
// media generation; the receipt itself never authorizes playback.
func (r *Registry) Observe(ctx context.Context, nonce string, nodeID int64, uuid, app, stream, ip string) error {
	r.mu.Lock()
	p := r.pending[nonce]
	if r.closed || p == nil || p.consumed || p.ctx.Err() != nil {
		r.mu.Unlock()
		return ErrUnavailable
	}
	p.consumed = true
	r.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	actualIP, err := playauth.NormalizeClientIP(ip)
	target := p.target
	if err != nil || nodeID != target.NodeID || uuid != target.UUID || app != target.App || stream != target.Stream || p.validate(ctx, target) != nil || ctx.Err() != nil {
		p.result <- ""
		return ErrUnavailable
	}
	p.result <- actualIP
	return nil
}
