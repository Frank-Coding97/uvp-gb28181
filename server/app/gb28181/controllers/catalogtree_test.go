package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func init() {
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
	if app.Response == nil {
		app.Response = response.NewResponseHandler()
	}
	gin.SetMode(gin.TestMode)
}

func newCatalogTreeRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelMount{},
		&gbmodels.GbAnomalyRecord{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDevice{},
	))

	ctrl := gbcontrollers.NewCatalogTreeController()
	ctrl.SetDB(func() *gorm.DB { return db })

	r := gin.New()
	r.Use(gin.Recovery())
	gr := r.Group("/api/gb28181/device-mgmt")
	{
		gr.GET("/catalog/tree", ctrl.Tree)
		gr.GET("/catalog/tree/:id", ctrl.Node)
		gr.GET("/catalog/tree/:id/children", ctrl.Children)
		gr.GET("/catalog/tree/:id/subtree", ctrl.Subtree)
		gr.GET("/catalog/anomaly/count", ctrl.AnomalyCount)
	}
	return r, db
}

func seedTree(t *testing.T, db *gorm.DB) (rootID, jinanID uint) {
	t.Helper()
	root := &gbmodels.GbCatalogNode{TenantID: 1, NodeType: gbmodels.NodeTypeCivilCode, Path: "/", Depth: 0, Name: "山东省", Code: "37"}
	require.NoError(t, db.Create(root).Error)
	root.Path = "/" + uintStr(root.ID) + "/"
	require.NoError(t, db.Model(root).Update("path", root.Path).Error)

	jn := &gbmodels.GbCatalogNode{TenantID: 1, NodeType: gbmodels.NodeTypeCivilCode, ParentID: &root.ID, Path: "", Depth: 1, Name: "济南", Code: "3701"}
	require.NoError(t, db.Create(jn).Error)
	jn.Path = root.Path + uintStr(jn.ID) + "/"
	require.NoError(t, db.Model(jn).Update("path", jn.Path).Error)

	ch1 := &gbmodels.GbCatalogNode{TenantID: 1, NodeType: gbmodels.NodeTypeChannel, ParentID: &jn.ID, Path: "", Depth: 2, Name: "通道 1", Code: "37011200001310000001"}
	require.NoError(t, db.Create(ch1).Error)
	ch1.Path = jn.Path + uintStr(ch1.ID) + "/"
	require.NoError(t, db.Model(ch1).Update("path", ch1.Path).Error)

	return root.ID, jn.ID
}

func uintStr(u uint) string {
	return strconvFormatUint(uint64(u))
}

// 自实现局部 itoa,避免引入 strconv 单纯用
func strconvFormatUint(u uint64) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for u > 0 {
		p--
		b[p] = byte('0' + u%10)
		u /= 10
	}
	return string(b[p:])
}

func unmarshal(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m))
	return m
}

func TestCatalogTree_Tree(t *testing.T) {
	r, db := newCatalogTreeRouter(t)
	rootID, _ := seedTree(t, db)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/catalog/tree", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	assert.Len(t, list, 1, "1 个根节点")
	first := list[0].(map[string]any)
	assert.EqualValues(t, rootID, first["id"])
	assert.Equal(t, "山东省", first["name"])
}

func TestCatalogTree_Children(t *testing.T) {
	r, db := newCatalogTreeRouter(t)
	_, jnID := seedTree(t, db)

	w := httptest.NewRecorder()
	url := "/api/gb28181/device-mgmt/catalog/tree/" + uintStr(jnID) + "/children?withMountCount=1"
	req, _ := http.NewRequest("GET", url, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	assert.Len(t, list, 1, "济南下应有 1 个直接子(通道 1)")
	first := list[0].(map[string]any)
	assert.Equal(t, "通道 1", first["name"])
	assert.Contains(t, first, "mountCount", "withMountCount=1 时 VO 应含 mountCount")
}

func TestCatalogTree_Subtree(t *testing.T) {
	r, db := newCatalogTreeRouter(t)
	rootID, _ := seedTree(t, db)

	w := httptest.NewRecorder()
	url := "/api/gb28181/device-mgmt/catalog/tree/" + uintStr(rootID) + "/subtree"
	req, _ := http.NewRequest("GET", url, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	list := data["list"].([]any)
	assert.GreaterOrEqual(t, len(list), 3, "整子树应含 root + 济南 + 通道 1")
}

func TestCatalogTree_Node_NotFound(t *testing.T) {
	r, _ := newCatalogTreeRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/catalog/tree/9999", nil)
	r.ServeHTTP(w, req)

	// 404 不存在 → controller 用 FailAndAbort,返回失败响应(HTTP 200 + success=false)
	resp := unmarshal(t, w)
	assert.NotEqual(t, "success", resp["status"])
}

func TestCatalogTree_AnomalyCount(t *testing.T) {
	r, db := newCatalogTreeRouter(t)
	// 加 2 条 anomaly(1 已处理 / 1 未处理)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: 1, RawCode: "X", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: false}).Error)
	require.NoError(t, db.Create(&gbmodels.GbAnomalyRecord{TenantID: 1, CatalogNodeID: 2, RawCode: "Y", FallbackType: gbmodels.FallbackTypeVirtualOrg, Resolved: true}).Error)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/device-mgmt/catalog/anomaly/count", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	resp := unmarshal(t, w)
	data := resp["data"].(map[string]any)
	assert.EqualValues(t, 1, data["count"], "只数未 resolved")
}
