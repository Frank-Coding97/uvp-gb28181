package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
)

// passwordValidatorRouter 用给定 safe 配置起一个只挂密码中间件的路由。
// 返回的密码提示语必须与前端 web/src/utils/password-policy.ts 一一对应。
func passwordValidatorRouter(t *testing.T, safeYml string) *gin.Engine {
	t.Helper()
	oldConfig := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = oldConfig })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte(safeYml), 0600))
	app.ConfigYml = ymlconfig.CreateYamlFactory(dir)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.PUT("/p", PasswordValidatorMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func performPasswordRequest(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/p", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestPasswordValidatorEnforcesLengthCaseAndSpecialChar(t *testing.T) {
	router := passwordValidatorRouter(t, "safe:\n  minpasswordlength: 8\n  requirespecialchar: true\n")
	for _, tc := range []struct {
		name, password, message string
		status                  int
	}{
		{"短于最小长度", "Ab1!", "密码长度不能少于8位", http.StatusBadRequest},
		{"缺大写", "new-password!", "密码必须同时包含大写和小写字母", http.StatusBadRequest},
		{"缺小写", "NEW-PASSWORD!", "密码必须同时包含大写和小写字母", http.StatusBadRequest},
		{"缺特殊字符", "Newpassword", "密码必须包含特殊字符 !@#$%", http.StatusBadRequest},
		{"三类齐全", "New-password!", "", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := performPasswordRequest(t, router, `{"password":"`+tc.password+`"}`)
			require.Equal(t, tc.status, res.Code)
			if tc.message != "" {
				require.Contains(t, res.Body.String(), tc.message)
			}
		})
	}
}

func TestPasswordValidatorSkipsRequestsWithoutPassword(t *testing.T) {
	router := passwordValidatorRouter(t, "safe:\n  minpasswordlength: 8\n  requirespecialchar: true\n")
	require.Equal(t, http.StatusOK, performPasswordRequest(t, router, `{"username":"admin"}`).Code)
}

func TestPasswordValidatorRespectsSpecialCharSwitch(t *testing.T) {
	router := passwordValidatorRouter(t, "safe:\n  minpasswordlength: 8\n  requirespecialchar: false\n")
	require.Equal(t, http.StatusOK, performPasswordRequest(t, router, `{"password":"Newpassword"}`).Code)
	require.Equal(t, http.StatusBadRequest, performPasswordRequest(t, router, `{"password":"newpassword"}`).Code)
}
