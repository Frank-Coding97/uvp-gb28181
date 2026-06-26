package controllers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

// mockResponse 实现 app.Response 最小子集,直接 c.JSON 写出(参考 dashboard_test.go)
type mockResponse struct{}

func (m mockResponse) ReturnJson(c *gin.Context, httpCode int, dataCode int, msg string, data interface{}) {
	c.JSON(httpCode, gin.H{"code": dataCode, "data": data, "message": msg})
}
func (m mockResponse) Success(c *gin.Context, data ...interface{}) {
	var payload interface{}
	if len(data) > 0 {
		payload = data[0]
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": payload, "message": "ok"})
}
func (m mockResponse) Fail(c *gin.Context, msg string, data ...interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 1, "message": msg})
}
func (m mockResponse) ErrorSystem(c *gin.Context, msg string, data interface{}) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": msg, "data": data})
}

func init() {
	app.Response = mockResponse{}
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
}

// abortRecover 拦截 FailAndAbort 的 panic(consts.RequestAborted) — 生产由 gin 中间件兜底,测试自己 recover
func abortRecover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				if r == consts.RequestAborted {
					return
				}
				panic(r)
			}
		}()
		c.Next()
	}
}

// 复用一份 service 测试用的 fake repo/probe(不导入 service_test 的私货)
type memoryRepo struct {
	rows   map[int64]node.Node
	nextID int64
}

func newMemoryRepo() *memoryRepo { return &memoryRepo{rows: map[int64]node.Node{}} }
func (r *memoryRepo) List(_ context.Context) ([]node.Node, error) {
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}
func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	if n, ok := r.rows[id]; ok {
		copy := n
		return &copy, nil
	}
	return nil, nil
}
func (r *memoryRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}
func (r *memoryRepo) Update(_ context.Context, n node.Node) error {
	r.rows[n.ID] = n
	return nil
}
func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	delete(r.rows, id)
	return nil
}

type fakeProbe struct{}

func (fakeProbe) GetServerConfig(_ context.Context, _ *node.Node) (map[string]string, error) {
	return map[string]string{"http.port": "80"}, nil
}
func (fakeProbe) ApplyConfigForNode(_ context.Context, _ *node.Node, _ service.MediaTuning) error {
	return nil
}

func setupRouter(t *testing.T) (*gin.Engine, *service.NodeService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	svc := service.NewNodeService(reg, fakeProbe{}, service.MediaTuning{})
	ctrl := gbcontrollers.NewZLMNodeController(svc)

	r := gin.New()
	r.Use(abortRecover())
	g := r.Group("/api/gb28181/zlm")
	{
		g.GET("/nodes", ctrl.List)
		g.POST("/nodes", ctrl.Create)
		g.GET("/nodes/:id", ctrl.Get)
		g.PUT("/nodes/:id", ctrl.Update)
		g.DELETE("/nodes/:id", ctrl.Delete)
		g.POST("/nodes/:id/maintenance", ctrl.SetMaintenance)
		g.POST("/nodes/:id/activate", ctrl.Activate)
	}
	return r, svc
}

func do(t *testing.T, r http.Handler, method, path string, body interface{}) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var bb *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		bb = bytes.NewBuffer(b)
	} else {
		bb = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, bb)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestZLMNodeAPI_CreateAndList_E2E(t *testing.T) {
	r, _ := setupRouter(t)

	w, resp := do(t, r, "POST", "/api/gb28181/zlm/nodes", service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.Equal(t, 200, w.Code)
	require.NotNil(t, resp["data"])

	w2, resp2 := do(t, r, "GET", "/api/gb28181/zlm/nodes", nil)
	require.Equal(t, 200, w2.Code)
	data := resp2["data"].(map[string]any)
	list := data["list"].([]any)
	require.Len(t, list, 1)
}

func TestZLMNodeAPI_Get_NotFound(t *testing.T) {
	r, _ := setupRouter(t)
	w, _ := do(t, r, "GET", "/api/gb28181/zlm/nodes/9999", nil)
	// FailAndAbort 仍返回 200 但 success=false(项目 Common 风格);
	// 关键是不能 panic/500
	require.NotEqual(t, 500, w.Code)
}

func TestZLMNodeAPI_LifecycleMaintenanceDelete(t *testing.T) {
	r, _ := setupRouter(t)

	w, resp := do(t, r, "POST", "/api/gb28181/zlm/nodes", service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	require.Equal(t, 200, w.Code)
	idF := resp["data"].(map[string]any)["id"].(float64)
	id := int64(idF)
	idStr := pathInt(id)

	// 活跃态直接删 → 失败
	w1, _ := do(t, r, "DELETE", "/api/gb28181/zlm/nodes/"+idStr, nil)
	require.NotEqual(t, 500, w1.Code)

	// 切维护
	w2, _ := do(t, r, "POST", "/api/gb28181/zlm/nodes/"+idStr+"/maintenance", nil)
	require.Equal(t, 200, w2.Code)

	// 删
	w3, _ := do(t, r, "DELETE", "/api/gb28181/zlm/nodes/"+idStr, nil)
	require.Equal(t, 200, w3.Code)

	// 列表空
	_, listResp := do(t, r, "GET", "/api/gb28181/zlm/nodes", nil)
	data := listResp["data"].(map[string]any)
	list, _ := data["list"].([]any)
	require.Empty(t, list)
}

func TestZLMNodeAPI_Update(t *testing.T) {
	r, _ := setupRouter(t)
	_, resp := do(t, r, "POST", "/api/gb28181/zlm/nodes", service.CreateNodeReq{
		Name: "n1", Host: "1.2.3.4", APIPort: 18080, APISecret: "s",
	})
	idF := resp["data"].(map[string]any)["id"].(float64)
	idStr := pathInt(int64(idF))

	weight := 90
	w, _ := do(t, r, "PUT", "/api/gb28181/zlm/nodes/"+idStr, service.UpdateNodeReq{
		Weight: &weight,
	})
	require.Equal(t, 200, w.Code)

	_, getResp := do(t, r, "GET", "/api/gb28181/zlm/nodes/"+idStr, nil)
	dto := getResp["data"].(map[string]any)
	require.Equal(t, float64(90), dto["weight"])
}

func pathInt(id int64) string {
	if id == 0 {
		return "0"
	}
	digits := []byte{}
	for id > 0 {
		digits = append([]byte{byte('0' + id%10)}, digits...)
		id /= 10
	}
	return string(digits)
}
