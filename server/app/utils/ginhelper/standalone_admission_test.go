package ginhelper

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestStandaloneAdmissionDrainsAcceptedRequests(t *testing.T) {
	g := NewStandaloneAdmission()
	r := gin.New()
	r.Use(g.Middleware())
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r.GET("/business", func(c *gin.Context) { close(entered); <-release; c.Status(204) })
	go func() {
		defer close(finished)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/business", nil))
	}()
	<-entered
	g.BeginDrain()
	if !g.Draining() {
		t.Fatal("draining state missing")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/business", nil))
	if w.Code != 503 {
		t.Fatalf("new business request: %d", w.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := g.Wait(ctx); err != context.DeadlineExceeded {
		t.Fatalf("accepted request not awaited: %v", err)
	}
	close(release)
	<-finished
	if err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.BeginDrain()
	if err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneAdmissionPreservesOnlyFinalizationHooksAndTheirAuth(t *testing.T) {
	g := NewStandaloneAdmission()
	r := gin.New()
	r.Use(g.Middleware())
	for _, path := range []string{"/index/hook/on_record_mp4", "/index/hook/on_stream_changed", "/index/hook/on_flow_report"} {
		r.POST(path, func(c *gin.Context) {
			if c.GetHeader("Authorization") != "fixture" {
				c.AbortWithStatus(403)
				return
			}
			c.Status(204)
		})
	}
	r.POST("/index/hook/on_play", func(c *gin.Context) { c.Status(204) })
	r.POST("/index/hook/on_publish", func(c *gin.Context) { c.Status(204) })
	r.GET("/index/hook/on_record_mp4", func(c *gin.Context) { c.Status(204) })
	g.BeginDrain()
	for _, tc := range []struct {
		method, path, auth string
		want               int
	}{
		{"POST", "/index/hook/on_record_mp4", "fixture", 204},
		{"POST", "/index/hook/on_record_mp4", "", 403},
		{"POST", "/index/hook/on_stream_changed", "fixture", 204},
		{"POST", "/index/hook/on_flow_report", "fixture", 204},
		{"POST", "/index/hook/on_play", "fixture", 503},
		{"POST", "/index/hook/on_publish", "fixture", 503},
		{"POST", "/index/hook/unknown", "fixture", 503},
		{"GET", "/index/hook/on_record_mp4", "fixture", 503},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", tc.auth)
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s %s = %d want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if err := g.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
