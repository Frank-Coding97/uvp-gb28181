package directory

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/datascope"
)

// DirectoryController 设备目录三维视图 REST 接口
//
//	GET /api/gb28181/directory/tree?dimension=X&parentId=Y&withCounts=1
//
// 支持的 dimension:
//   - native      国标自动注册维度(gb_catalog_node 原始层级)
//   - biz_group   业务分组维度(TODO T-1.4)
//   - civil_code  行政区划维度(TODO T-1.5)
type DirectoryController struct {
	db func() *gorm.DB // 注入,默认走 app.GormDbMysql,便于测试替换
}

// NewDirectoryController 默认 DB provider 用 app.GormDbMysql
func NewDirectoryController() *DirectoryController {
	return &DirectoryController{
		db: func() *gorm.DB { return app.GormDbMysql },
	}
}

// SetDB 测试用注入点
func (dc *DirectoryController) SetDB(provider func() *gorm.DB) {
	dc.db = provider
}

// Tree 三维视图统一入口
// GET /api/gb28181/directory/tree?dimension=native&parentId=1&withCounts=1
//
// 参数:
//   - dimension  必填,维度名(native / biz_group / civil_code)
//   - parentId   可选,父节点 ID;不传则返回根节点列表
//   - withCounts 可选,是否附加 mount_count / channel_count(1=是)
func (dc *DirectoryController) Tree(c *gin.Context) {
	db := dc.db()
	if db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    503,
			"message": "database unavailable",
		})
		return
	}

	dimension := c.Query("dimension")
	if dimension == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "missing required parameter: dimension",
		})
		return
	}

	parentID := c.Query("parentId")
	withCounts := c.Query("withCounts") == "1" || c.Query("withCounts") == "true"

	// dept scope(admin=全量,普通用户按 dept 过滤)
	scope := datascope.OwnerDeptScope(c, "owner_dept_id")

	dim, err := createDimension(dimension, db, scope)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": err.Error(),
		})
		return
	}

	var nodes []Node
	if parentID == "" {
		nodes, err = dim.GetRoots(withCounts)
	} else {
		nodes, err = dim.GetChildren(parentID, withCounts)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"dimension": dimension,
			"list":      nodes,
			"total":     len(nodes),
		},
	})
}

// createDimension 根据维度名称创建对应的维度实例
func createDimension(dimensionName string, db *gorm.DB, scope ScopeFunc) (Dimension, error) {
	switch dimensionName {
	case DimensionNative:
		return NewNativeDimension(db, scope), nil
	case DimensionBizGroup, DimensionCivilCode:
		return nil, ErrDimensionNotImplemented
	default:
		return nil, ErrInvalidDimension
	}
}
