package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 查询类请求没有 Body，审计只捕获 Body 会让详情页「请求参数」恒为空白。
// 这里锁定「无 Body 时回落到 query string」的契约。
func TestOperationLogRequestDataFallsBackToQueryString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/zlm/sessions/network?pageNum=1&pageSize=10&keyword=%E6%B5%8B%E8%AF%95&order=id+desc", nil)

	value := operationLogRequestData(ctx, nil)
	require.Contains(t, value, "pageNum=1")
	require.Contains(t, value, "keyword=测试")
	// 还原后的 + 是空格语义，不该再出现百分号转义
	require.NotContains(t, value, "%")
}

// Body 存在时仍然以 Body 为准，不能被 query 覆盖。
func TestOperationLogRequestDataPrefersBodyOverQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/sysMenu/add?from=web", strings.NewReader(`{"name":"m"}`))

	require.Equal(t, `{"name":"m"}`, operationLogRequestData(ctx, []byte(`{"name":"m"}`)))
}

// query 里的凭据必须脱敏：审计落库等于长期保存接入凭据。
func TestSanitizeQueryStringRedactsCredentials(t *testing.T) {
	value := sanitizeQueryString("token=abc123&apiKey=k1&name=ok&Secret=s3")
	require.NotContains(t, value, "abc123")
	require.NotContains(t, value, "k1")
	require.NotContains(t, value, "s3")
	require.Contains(t, value, "token=***")
	require.Contains(t, value, "name=ok")
}

func TestSanitizeQueryStringEmptyAndBroken(t *testing.T) {
	require.Empty(t, sanitizeQueryString(""))
	// 解析失败宁可丢弃，也不把原文落库
	require.Empty(t, sanitizeQueryString("a=%zz&token=leak"))
}

// 无 body 且无 query 时保持空，前端据此显示占位而不是渲染空块。
func TestOperationLogRequestDataEmptyWhenNothingCaptured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/gb28181/device/list", nil)

	require.Empty(t, operationLogRequestData(ctx, nil))
}
