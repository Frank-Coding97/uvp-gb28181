package ginhelper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLoggerRedactsPlaybackCredentialsWithoutChangingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	router := gin.New()
	router.Use(accessLogger(&output))
	router.GET("/index/hook/on_stream_not_found", func(c *gin.Context) {
		if c.Query("play_token") != "play-secret" || c.Query("cap") != "callback-secret" || c.Query("keep") != "visible" {
			t.Fatalf("handler query was modified: %s", c.Request.URL.RawQuery)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet,
		"/index/hook/on_stream_not_found?play_token=play-secret&cap=callback-secret&keep=visible", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	logged := output.String()
	if bytes.Contains(output.Bytes(), []byte("play-secret")) || bytes.Contains(output.Bytes(), []byte("callback-secret")) {
		t.Fatalf("access log leaked credentials: %s", logged)
	}
	if !bytes.Contains(output.Bytes(), []byte("keep=visible")) || !bytes.Contains(output.Bytes(), []byte("REDACTED")) {
		t.Fatalf("access log did not preserve safe query or redact credentials: %s", logged)
	}
}
