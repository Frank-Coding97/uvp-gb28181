package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
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

func TestForceLogoutAuditContainsActorAndMaskedTargetOnly(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/sysOnlineUser/forceLogout", strings.NewReader(`{"sid":"1234567890abcdef"}`))
	ctx.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 9, Username: "operator"}})
	MarkSensitiveOperation(ctx, map[string]any{
		"targetSession": "12345678...", "targetUsername": "target",
		"targetIP": "10.0.0.8", "result": "revoked",
	})

	record := buildOperationLogRecord(ctx, time.Now(), []byte(`{"sid":"1234567890abcdef"}`), nil)
	require.Equal(t, uint(9), record.UserID)
	require.Equal(t, "operator", record.Username)
	require.Equal(t, "在线用户管理", record.Module)
	require.Contains(t, record.RequestData, "12345678...")
	require.Contains(t, record.RequestData, "target")
	require.Contains(t, record.RequestData, "10.0.0.8")
	require.Contains(t, record.RequestData, "revoked")
	for _, secret := range []string{"1234567890abcdef", "authorization", "refresh_token_hash"} {
		require.NotContains(t, strings.ToLower(record.RequestData), secret)
	}
}
