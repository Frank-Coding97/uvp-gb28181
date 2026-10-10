package ginhelper

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLoggerRedactsPlaybackCredentialsWithoutChangingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router, output := loggingRouter(t)
	router.GET("/index/hook/on_stream_not_found", func(c *gin.Context) {
		if c.Query("play_token") != "play-secret" || c.Query("media_access_token") != "media-secret" || c.Query("cap") != "callback-secret" || c.Query("keep") != "visible" {
			t.Fatalf("handler query was modified: %s", c.Request.URL.RawQuery)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet,
		"/index/hook/on_stream_not_found?play_token=play-secret&media_access_token=media-secret&cap=callback-secret&keep=visible", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	loggedBytes, _ := json.Marshal(output.rows(t))
	logged := string(loggedBytes)
	if bytes.Contains(loggedBytes, []byte("play-secret")) || bytes.Contains(loggedBytes, []byte("media-secret")) || bytes.Contains(loggedBytes, []byte("callback-secret")) {
		t.Fatalf("access log leaked credentials: %s", logged)
	}
	if bytes.Contains(loggedBytes, []byte("keep=visible")) || bytes.Contains(loggedBytes, []byte("?")) {
		t.Fatalf("access log retained query: %s", logged)
	}
}
