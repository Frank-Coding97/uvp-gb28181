package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// deadlineSink records the connection deadlines that reach the real writer.
// It stands in for *http.response, which is the only layer that can actually
// extend the socket deadline.
type deadlineSink struct {
	http.ResponseWriter
	readDeadlines  []time.Time
	writeDeadlines []time.Time
}

func (w *deadlineSink) SetReadDeadline(deadline time.Time) error {
	w.readDeadlines = append(w.readDeadlines, deadline)
	return nil
}

func (w *deadlineSink) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadlines = append(w.writeDeadlines, deadline)
	return nil
}

// TestUpgradeImportDeadlineSurvivesOperationLogWriter guards the middleware
// order registered in routes.go: OperationLogMiddleware wraps c.Writer before
// TimeoutMiddleware runs. If that wrapper does not forward Unwrap, the
// response controller cannot reach the connection, the deadline extension
// fails silently and the ordinary httpserver.read_timeout (30s) aborts the
// upload with "read upgrade upload: i/o timeout".
func TestUpgradeImportDeadlineSurvivesOperationLogWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &deadlineSink{ResponseWriter: httptest.NewRecorder()}
	ctx, _ := gin.CreateTestContext(sink)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", nil)

	// Mirror OperationLogMiddleware.
	ctx.Writer = &responseWriter{body: bytes.NewBuffer(nil), ResponseWriter: ctx.Writer, context: ctx}

	TimeoutMiddleware(time.Second)(ctx)

	require.Len(t, sink.writeDeadlines, 1)
	require.Len(t, sink.readDeadlines, 1)
	require.WithinDuration(t, time.Now().Add(upgradeUploadTimeout), sink.readDeadlines[0], 5*time.Second)
}

// TestOperationLogWriterForwardsUnwrap states the interface requirement
// directly, so a future rewrite of the wrapper cannot silently drop it.
func TestOperationLogWriterForwardsUnwrap(t *testing.T) {
	var _ interface{ Unwrap() http.ResponseWriter } = (*responseWriter)(nil)
}
