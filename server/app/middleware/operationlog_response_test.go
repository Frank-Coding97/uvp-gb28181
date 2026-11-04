package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newAuditContext(method, target string) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(method, target, nil)
	return ctx
}

// 写操作的响应体要落库，这是「我改了什么、接口回了什么」的唯一凭据。
func TestOperationLogResponseDataRecordsWriteResponse(t *testing.T) {
	body := []byte(`{"code":0,"message":"success","data":{"id":12}}`)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		ctx := newAuditContext(method, "/api/sysMenu")
		require.Equal(t, string(body), operationLogResponseData(ctx, body), method)
	}
}

// 查询类不记:列表响应可达上百 KB，逐条落库会把审计表撑爆。
func TestOperationLogResponseDataSkipsReadRequests(t *testing.T) {
	body := []byte(`{"code":0,"data":{"list":[]}}`)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		ctx := newAuditContext(method, "/api/sysMenu/list")
		require.Empty(t, operationLogResponseData(ctx, body), method)
	}
}

func TestOperationLogResponseDataTruncatesLargePayload(t *testing.T) {
	ctx := newAuditContext(http.MethodPost, "/api/sysMenu")
	large := []byte(strings.Repeat("a", maxAuditResponseBytes+500))

	value := operationLogResponseData(ctx, large)
	require.True(t, strings.HasSuffix(value, "...(truncated)"))
	require.Len(t, strings.TrimSuffix(value, "...(truncated)"), maxAuditResponseBytes)
}

// 截断不能把一个中文字劈成两半，否则详情页会显示乱码。
func TestOperationLogResponseDataKeepsUTF8Boundary(t *testing.T) {
	ctx := newAuditContext(http.MethodPost, "/api/sysMenu")
	// 每个汉字 3 字节，用不能被 4096 整除的填充把边界压在字符中间
	value := operationLogResponseData(ctx, []byte(strings.Repeat("中", maxAuditResponseBytes/3+10)))

	require.True(t, strings.HasSuffix(value, "...(truncated)"))
	require.Contains(t, value, "中")
	require.NotContains(t, value, "\ufffd")
}

// MarkSensitiveOperation 标记的端点连捕获缓冲都不写，响应绝不能绕过。
func TestOperationLogResponseDataSkipsSensitiveOperation(t *testing.T) {
	ctx := newAuditContext(http.MethodPost, "/api/system/https/certificate")
	MarkSensitiveOperation(ctx, map[string]any{"action": "certificate.update"})

	require.Empty(t, operationLogResponseData(ctx, []byte(`{"privateKey":"secret"}`)))
}

// 二进制响应不进 text 字段。
func TestOperationLogResponseDataSkipsBinaryPayload(t *testing.T) {
	ctx := newAuditContext(http.MethodPost, "/api/sysAffix/upload")

	require.Equal(t, "[非文本响应，未记录]", operationLogResponseData(ctx, []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0xff}))
}

func TestOperationLogResponseDataEmptyBody(t *testing.T) {
	ctx := newAuditContext(http.MethodDelete, "/api/sysMenu/delete")

	require.Empty(t, operationLogResponseData(ctx, nil))
}
