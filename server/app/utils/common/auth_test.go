package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTokenTestContext(t *testing.T, method, target string, headers map[string]string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		c.Request.Header.Set(key, value)
	}
	return c
}

func TestGetAccessTokenPrefersAuthorizationHeader(t *testing.T) {
	c := newTokenTestContext(t, "GET", "/api/users?token=query-token",
		map[string]string{"Authorization": "Bearer header-token"})

	token, err := GetAccessToken(c)
	require.NoError(t, err)
	require.Equal(t, "header-token", token, "请求头存在时不能被 URL 参数顶替")
}

// 白名单端点（EventSource / <img>）必须继续支持 ?token=，否则实时流与图像库全部 401。
func TestGetAccessTokenAllowsQueryTokenOnAllowlistedEndpoints(t *testing.T) {
	cases := []string{
		"/api/gb28181/sip/dashboard/stream?window=60m&token=query-token",
		"/api/gb28181/sip-traces/stream?token=query-token",
		"/api/gb28181/logs/stream?token=query-token",
		"/api/gb28181/security/stream?token=query-token",
		"/api/gb28181/device-mgmt/snapshots/42/content?token=query-token",
	}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			c := newTokenTestContext(t, "GET", target, nil)
			token, err := GetAccessToken(c)
			require.NoError(t, err)
			require.Equal(t, "query-token", token)
		})
	}
}

// 其余接口一律拒绝 URL 令牌 —— 这是本次收口的核心断言：
// 令牌不再出现在普通业务接口的 URL 里（nginx 日志、浏览器历史、Referer）。
func TestGetAccessTokenRejectsQueryTokenOutsideAllowlist(t *testing.T) {
	cases := []struct {
		method string
		target string
	}{
		{"GET", "/api/users?token=query-token"},
		{"GET", "/api/gb28181/device-mgmt/channels?token=query-token"},
		// 只是长得像：路径不在白名单内，或不是取图接口。
		{"GET", "/api/gb28181/device-mgmt/snapshots?token=query-token"},
		{"GET", "/api/gb28181/device-mgmt/snapshots/42?token=query-token"},
		{"GET", "/api/gb28181/device-mgmt/snapshots/42/content/extra?token=query-token"},
		// 写操作即使落在白名单路径上也只认请求头。
		{"POST", "/api/gb28181/security/stream?token=query-token"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			c := newTokenTestContext(t, tc.method, tc.target, nil)
			token, err := GetAccessToken(c)
			require.Error(t, err)
			require.Empty(t, token)
		})
	}
}

func TestGetAccessTokenRejectsMalformedHeader(t *testing.T) {
	c := newTokenTestContext(t, "GET", "/api/users", map[string]string{"Authorization": "token-only"})

	token, err := GetAccessToken(c)
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "Bearer")
}
