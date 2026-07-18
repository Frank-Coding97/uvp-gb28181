package directory_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/directory"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&models.GbCatalogNode{})
	assert.NoError(t, err)

	// 注册路由
	directory.RegisterRoutes(router.Group("/api/gb28181"), db)

	return router, db
}

func TestGetRoots_Native(t *testing.T) {
	router, db := setupTestRouter(t)

	// 准备测试数据
	rootNode := models.GbCatalogNode{
		OwnerDeptID: 1,
		Name:        "根节点",
		NodeType:    models.NodeTypeCivilCode,
		CivilCode:   "110000",
	}
	assert.NoError(t, db.Create(&rootNode).Error)

	// 请求
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/directory/native?withCounts=false", nil)
	req.Header.Set("X-Owner-Dept-ID", "1")
	router.ServeHTTP(w, req)

	// 验证
	assert.Equal(t, http.StatusOK, w.Code)

	var response struct {
		Code int                `json:"code"`
		Data []directory.Node   `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, 0, response.Code)
	assert.Len(t, response.Data, 1)
	assert.Equal(t, "根节点", response.Data[0].Name)
}

func TestGetRoots_InvalidDimension(t *testing.T) {
	router, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/gb28181/directory/invalid_dim?withCounts=false", nil)
	req.Header.Set("X-Owner-Dept-ID", "1")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
