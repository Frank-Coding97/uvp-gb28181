package directory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func setupTestController(t *testing.T) (*gin.Engine, *gorm.DB) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&models.GbCatalogNode{})
	assert.NoError(t, err)

	ctrl := NewDirectoryController()
	ctrl.SetDB(func() *gorm.DB { return db })

	// 挂到 tree 路径(跟 spec 一致)
	router.GET("/api/gb28181/directory/tree", ctrl.Tree)

	return router, db
}

func TestDirectoryTree_Native_Roots(t *testing.T) {
	router, db := setupTestController(t)

	// 准备:根节点 + 子节点
	rootNode := models.GbCatalogNode{
		OwnerDeptID: 0,
		Name:        "根节点",
		NodeType:    models.NodeTypeCivilCode,
		CivilCode:   "110000",
	}
	assert.NoError(t, db.Create(&rootNode).Error)

	// 请求
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/directory/tree?dimension=native", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Code int `json:"code"`
		Data struct {
			Dimension string `json:"dimension"`
			List      []Node `json:"list"`
			Total     int    `json:"total"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 0, response.Code)
	assert.Equal(t, "native", response.Data.Dimension)
	assert.Equal(t, 1, response.Data.Total)
	assert.Equal(t, "根节点", response.Data.List[0].Name)
}

func TestDirectoryTree_MissingDimension(t *testing.T) {
	router, _ := setupTestController(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/directory/tree", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDirectoryTree_InvalidDimension(t *testing.T) {
	router, _ := setupTestController(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/directory/tree?dimension=invalid_dim", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDirectoryTree_NotImplementedDimension(t *testing.T) {
	router, _ := setupTestController(t)

	// biz_group / civil_code 目前是 not-implemented stub
	for _, dim := range []string{"biz_group", "civil_code"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/gb28181/directory/tree?dimension="+dim, nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code, "dim=%s", dim)
	}
}
