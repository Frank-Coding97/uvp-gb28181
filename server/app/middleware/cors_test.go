package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorsNextAllowsPlaybackIdempotencyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsNext())
	router.POST("/playback", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodOptions, "/playback", nil)
	request.Header.Set("Origin", "http://localhost:5177")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "idempotency-key,content-type")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", recorder.Code)
	}
	allowed := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowed), "idempotency-key") {
		t.Fatalf("expected idempotency-key in allowed headers, got %q", allowed)
	}
}
