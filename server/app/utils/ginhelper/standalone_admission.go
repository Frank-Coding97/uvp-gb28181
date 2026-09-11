package ginhelper

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// StandaloneAdmission stops new business requests without closing the listener
// needed by final recording callbacks. Hook authentication remains in the route.
// Wait covers admitted business handlers only; http.Server.Shutdown must later
// drain callbacks after the launcher has finalized the media process.
type StandaloneAdmission struct {
	mu       sync.Mutex
	draining bool
	active   int
	done     chan struct{}
}

func NewStandaloneAdmission() *StandaloneAdmission {
	return &StandaloneAdmission{done: make(chan struct{})}
}

func (g *StandaloneAdmission) Draining() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.draining
}

func (g *StandaloneAdmission) BeginDrain() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.draining {
		g.draining = true
		if g.active == 0 {
			close(g.done)
		}
	}
}

func (g *StandaloneAdmission) Wait(ctx context.Context) error {
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *StandaloneAdmission) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// These are notification routes, not on_play/on_publish admission or
		// on_stream_not_found, which can start a new stream during shutdown.
		if c.Request.Method == http.MethodPost && finalizationHook(c.FullPath()) {
			c.Next()
			return
		}
		g.mu.Lock()
		if g.draining {
			g.mu.Unlock()
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "平台正在停止"})
			return
		}
		g.active++
		g.mu.Unlock()
		defer func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.active--
			if g.draining && g.active == 0 {
				close(g.done)
			}
		}()
		c.Next()
	}
}

func finalizationHook(path string) bool {
	switch path {
	case "/index/hook/on_record_mp4", "/index/hook/on_stream_changed",
		"/index/hook/on_flow_report", "/index/hook/on_rtp_server_timeout",
		"/index/hook/on_stream_none_reader":
		return true
	default:
		return false
	}
}
