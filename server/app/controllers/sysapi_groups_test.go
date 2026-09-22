package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

// 这两个接口是「接口管理」防漂移的闭环：GET /sysApi/groups 给前端供下拉，
// POST /sysApi/add、PUT /sysApi/edit 把清单外的值挡回去。
// 处理器不碰数据库，所以这里用最小 gin 引擎就能验完。
func newSysApiTestEngine(method, path string, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// FailAndAbort 用 panic(consts.RequestAborted) 终止链路，
	// 这里静默接住它，避免 gin 默认 Recovery 打一屏栈影响看输出。
	engine.Use(func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				c.Abort()
			}
		}()
		c.Next()
	})
	engine.Handle(method, path, handler)
	return engine
}

func doJSON(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func withResponseHandler(t *testing.T) {
	t.Helper()
	previous := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = previous })
}

func TestSysApiGroupsEndpointReturnsWholeControlledList(t *testing.T) {
	withResponseHandler(t)
	controller := &SysApiController{}
	engine := newSysApiTestEngine(http.MethodGet, "/api/sysApi/groups", controller.Groups)

	recorder := doJSON(engine, http.MethodGet, "/api/sysApi/groups", "")

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Code int      `json:"code"`
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, models.SysApiGroupNames(), body.Data, "下拉数据源必须与受控清单逐项一致")
	require.Contains(t, body.Data, "设备管理")
	require.NotContains(t, body.Data, "按钮权限目录", "已解散的兜底分组不得回流")
}

// 写入校验：清单外的分组必须在落库之前被挡掉（HTTP 400 + 业务码 1）。
// 用 Add 验，因为它的分组校验排在所有数据库访问之前 —— 测试不需要数据库。
func TestSysApiAddRejectsGroupOutsideControlledList(t *testing.T) {
	payloads := map[string]string{
		"兜底分组": `{"title":"测试接口","path":"/api/unit-test/should-not-exist","method":"GET","apiGroup":"按钮权限目录"}`,
		"乱码分组": `{"title":"测试接口","path":"/api/unit-test/should-not-exist","method":"GET","apiGroup":"æŒ‰é’®æƒé™ç›®å½•"}`,
		"空白分组": `{"title":"测试接口","path":"/api/unit-test/should-not-exist","method":"GET","apiGroup":"自定义分组"}`,
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			withResponseHandler(t)
			controller := &SysApiController{}
			engine := newSysApiTestEngine(http.MethodPost, "/api/sysApi/add", controller.Add)

			recorder := doJSON(engine, http.MethodPost, "/api/sysApi/add", payload)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var body struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, 1, body.Code)
			require.Contains(t, body.Message, "受控清单")
		})
	}
}
