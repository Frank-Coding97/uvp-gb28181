package directory

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	// HTTP 响应码
	codeSuccess          = 0
	codeBadRequest       = 400
	codeInternalError    = 500

	// 请求头
	headerOwnerDeptID    = "X-Owner-Dept-ID"

	// 查询参数
	queryWithCounts      = "withCounts"
	queryWithCountsDefault = "false"
)

// RegisterRoutes 注册目录视图路由
//
// 路由结构:
//   GET /directory/:dimension              - 获取顶层节点
//   GET /directory/:dimension/children/:parentID - 获取子节点
//
// 支持的维度:
//   - native: 国标自动注册维度(原始目录树)
//
// 查询参数:
//   - withCounts: 是否返回统计数(mountCount/channelCount),默认 false
func RegisterRoutes(rg *gin.RouterGroup, db *gorm.DB) {
	rg.GET("/directory/:dimension", handleGetRoots(db))
	rg.GET("/directory/:dimension/children/:parentID", handleGetChildren(db))
}

// handleGetRoots 获取指定维度的顶层节点
func handleGetRoots(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		dimension := c.Param("dimension")
		withCounts := c.DefaultQuery(queryWithCounts, queryWithCountsDefault) == "true"
		ownerDeptID := getOwnerDeptID(c)

		dim, err := createDimension(dimension, db, ownerDeptID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    codeBadRequest,
				"message": err.Error(),
			})
			return
		}

		nodes, err := dim.GetRoots(withCounts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    codeInternalError,
				"message": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": codeSuccess,
			"data": nodes,
		})
	}
}

// handleGetChildren 获取指定节点的子节点
func handleGetChildren(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		dimension := c.Param("dimension")
		parentID := c.Param("parentID")
		withCounts := c.DefaultQuery(queryWithCounts, queryWithCountsDefault) == "true"
		ownerDeptID := getOwnerDeptID(c)

		dim, err := createDimension(dimension, db, ownerDeptID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    codeBadRequest,
				"message": err.Error(),
			})
			return
		}

		nodes, err := dim.GetChildren(parentID, withCounts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    codeInternalError,
				"message": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code": codeSuccess,
			"data": nodes,
		})
	}
}

// createDimension 根据维度名称创建对应的维度实例
func createDimension(dimensionName string, db *gorm.DB, ownerDeptID uint) (Dimension, error) {
	switch dimensionName {
	case DimensionNative:
		return NewNativeDimension(db, ownerDeptID), nil
	default:
		return nil, ErrInvalidDimension
	}
}

// getOwnerDeptID 从请求头获取 owner_dept_id
func getOwnerDeptID(c *gin.Context) uint {
	ownerDeptIDStr := c.GetHeader(headerOwnerDeptID)
	ownerDeptID, _ := strconv.ParseUint(ownerDeptIDStr, 10, 64)
	return uint(ownerDeptID)
}
