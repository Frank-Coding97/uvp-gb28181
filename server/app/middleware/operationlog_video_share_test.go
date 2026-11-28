package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/models"
)

// Exercise admission failures as well as successful handlers: skipping must
// happen before any request/response capture, independent of controller marks.
func TestPublicVideoShareOperationLogNeverReadsOrPersistsCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/api/public/video-share/resolve", http.StatusOK},
		{http.MethodPost, "/api/public/video-share/viewers", http.StatusOK},
		{http.MethodPost, "/api/public/video-share/viewers/status", http.StatusOK},
		{http.MethodDelete, "/api/public/video-share/viewers/private-viewer-token", http.StatusNoContent},
		{http.MethodPost, "/api/public/video-share/resolve", http.StatusTooManyRequests},
		{http.MethodGet, "/api/public/video-share/viewers/private-viewer-token/live.flv", http.StatusOK},
		{http.MethodGet, "/api/public/video-share/viewers/private-viewer-token/hls/secret-resource", http.StatusOK},
	} {
		t.Run(tc.method+tc.path+http.StatusText(tc.status), func(t *testing.T) {
			body := &countingBody{reader: strings.NewReader(`{"signature":"private-signature","viewerToken":"private-viewer-token"}`)}
			router := gin.New()
			router.Use(OperationLogMiddleware())
			router.Handle(tc.method, tc.path, func(c *gin.Context) {
				_, buffered := c.Writer.(*responseWriter)
				require.False(t, buffered, "no response capture buffer may wrap anonymous media or credentials")
				if tc.status == http.StatusNoContent {
					c.Status(tc.status)
					return
				}
				c.String(tc.status, `{"token":"private-viewer-token"}`)
			})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Body = body
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			require.Zero(t, body.reads, "audit must never read the public share credential body")
			require.Equal(t, tc.status, recorder.Code)
		})
	}
}

func TestPublicVideoShareOperationLogBoundaryIsExact(t *testing.T) {
	for _, path := range []string{"/api/public/video-share", "/api/public/video-share/resolve", "/api/public/video-share/viewers/status"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
		require.True(t, shouldSkipLog(ctx), path)
	}
	for _, path := range []string{"/api/public/video-share-other/resolve", "/api/gb28181/channel-shares/42/link", "/api/public/video-sharing/resolve", "/api/example?next=/api/public/video-share/resolve"} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
		require.False(t, shouldSkipLog(ctx), path)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/channel-shares/42/link", nil)
	MarkSensitiveOperation(ctx, map[string]any{"event": "video_share.acquire", "channel_row_id": 42})
	record := buildOperationLogRecord(ctx, time.Now(), nil, []byte(`{"signature":"never-log"}`))
	require.NotNil(t, record)
	require.Equal(t, models.OperationCreate, record.Operation)
	require.Empty(t, record.ResponseData)
	require.Contains(t, record.RequestData, "video_share.acquire")
}
