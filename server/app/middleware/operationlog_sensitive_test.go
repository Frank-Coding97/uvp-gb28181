package middleware

import (
	"bytes"
	"errors"
	"io"
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

type countingBody struct {
	reader io.Reader
	reads  int
}

func (b *countingBody) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (b *countingBody) Close() error { return nil }

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

func TestSensitiveOperationErrorLogDoesNotPersistRawContextError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/gb28181/zlm/nodes/7/config", strings.NewReader(`{"changes":{"hook.timeoutSec":"secret-value"}}`))
	MarkSensitiveOperation(ctx, map[string]any{"action": "config.update", "nodeId": int64(7), "result": "failed"})
	ctx.Set("error", errors.New("upstream rejected secret-value at https://user:pass@example.invalid/?token=secret"))
	ctx.Status(http.StatusBadGateway)

	record := buildOperationLogRecord(ctx, time.Now(), []byte(`{"changes":{"hook.timeoutSec":"secret-value"}}`), []byte(`{"message":"secret-value"}`))
	require.Equal(t, "请求处理失败", record.ErrorMsg)
	encoded := record.RequestData + record.ErrorMsg
	require.NotContains(t, encoded, "secret-value")
	require.NotContains(t, encoded, "user:pass")
	require.NotContains(t, encoded, "token=secret")
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

func TestOperationLogDoesNotReadServerStartedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &countingBody{reader: strings.NewReader(`{"general.mediaServerId":"node-a","api.secret":"must-never-escape"}`)}
	request := httptest.NewRequest(http.MethodPost, "/index/hook/on_server_started", nil)
	request.Body = body
	recorder := httptest.NewRecorder()
	engine := gin.New()
	engine.Use(OperationLogMiddleware())
	engine.POST("/index/hook/on_server_started", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 0})
	})

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Zero(t, body.reads, "operation log must not inspect the secret-bearing hook body")
}
