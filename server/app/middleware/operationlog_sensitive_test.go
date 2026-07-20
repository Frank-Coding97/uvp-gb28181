package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponseWriterDoesNotCaptureSensitivePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	underlying := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(underlying)
	writer := &responseWriter{body: bytes.NewBuffer(nil), ResponseWriter: ctx.Writer, context: ctx}
	MarkSensitiveOperation(ctx, map[string]any{"messageId": "event-1", "purpose": "incident-42"})

	_, err := writer.Write([]byte("Authorization: secret"))
	require.NoError(t, err)
	require.Empty(t, writer.body.Bytes())
	require.Contains(t, underlying.Body.String(), "Authorization: secret")
	require.NotContains(t, operationLogRequestData(ctx, []byte("ignored")), "Authorization")
	require.Contains(t, operationLogRequestData(ctx, nil), "incident-42")
}
