package controllers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// CatalogTreeController 国标多级目录树 REST 接口(plan §4.2 B1)
//
//	GET /api/gb28181/device-mgmt/catalog/tree                  树根
//	GET /api/gb28181/device-mgmt/catalog/tree/:id              单节点
//	GET /api/gb28181/device-mgmt/catalog/tree/:id/children     子节点(支持 ?withMountCount=1)
//	GET /api/gb28181/device-mgmt/catalog/tree/:id/subtree      整子树(按 path 前缀 LIKE)
//	GET /api/gb28181/device-mgmt/catalog/anomaly/count         anomaly 未处理总数(左侧底部入口用)
type CatalogTreeController struct {
	controllers.Common
	db func() *gorm.DB // 注入,默认走 app.GormDbMysql,便于测试替换
}

// NewCatalogTreeController 默认 DB provider 用 app.GormDbMysql
func NewCatalogTreeController() *CatalogTreeController {
	return &CatalogTreeController{
		db: func() *gorm.DB { return app.GormDbMysql },
	}
}

// SetDB 测试用注入点
func (cc *CatalogTreeController) SetDB(provider func() *gorm.DB) {
	cc.db = provider
}

// catalogNodeVO 单节点 VO,可附 mountCount / anomaly count
type catalogNodeVO struct {
	*gbmodels.GbCatalogNode
	MountCount   int64 `json:"mountCount,omitempty"`
	ChildCount   int64 `json:"childCount,omitempty"`
	AnomalyCount int64 `json:"anomalyCount,omitempty"`
}

// Tree 根节点列表(parent_id IS NULL)
// GET /api/gb28181/device-mgmt/catalog/tree
func (cc *CatalogTreeController) Tree(c *gin.Context) {
	db := cc.db()
	if db == nil {
		cc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}

	var roots []gbmodels.GbCatalogNode
	if err := db.WithContext(c).Scopes(ownerDeptScope(c)).
		Where("parent_id IS NULL").
		Order("sort_order, id").
		Find(&roots).Error; err != nil {
		cc.FailAndAbort(c, "查询根节点失败", err)
		return
	}
	cc.Success(c, gin.H{"list": roots, "total": len(roots)})
}

// Children 子节点列表
// GET /api/gb28181/device-mgmt/catalog/tree/:id/children?withMountCount=1
func (cc *CatalogTreeController) Children(c *gin.Context) {
	db := cc.db()
	if db == nil {
		cc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	parentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		cc.FailAndAbort(c, "节点 ID 不合法", err)
		return
	}

	var children []gbmodels.GbCatalogNode
	if err := db.WithContext(c).Scopes(ownerDeptScope(c)).
		Where("parent_id = ?", parentID).
		Order("sort_order, id").
		Find(&children).Error; err != nil {
		cc.FailAndAbort(c, "查询子节点失败", err)
		return
	}

	if c.Query("withMountCount") == "1" && len(children) > 0 {
		vos := cc.attachMountCount(c, db, children)
		cc.Success(c, gin.H{"list": vos, "total": len(vos)})
		return
	}

	cc.Success(c, gin.H{"list": children, "total": len(children)})
}

// Subtree 整子树(按物化路径 LIKE 'pathprefix%')
// GET /api/gb28181/device-mgmt/catalog/tree/:id/subtree
func (cc *CatalogTreeController) Subtree(c *gin.Context) {
	db := cc.db()
	if db == nil {
		cc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		cc.FailAndAbort(c, "节点 ID 不合法", err)
		return
	}

	var root gbmodels.GbCatalogNode
	res := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&root)
	if res.Error != nil {
		cc.FailAndAbort(c, "查询节点失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		cc.FailAndAbort(c, "节点不存在", nil)
		return
	}

	var sub []gbmodels.GbCatalogNode
	if err := db.WithContext(c).Scopes(ownerDeptScope(c)).
		Where("path LIKE ?", root.Path+"%").
		Order("depth, sort_order, id").
		Find(&sub).Error; err != nil {
		cc.FailAndAbort(c, "查询子树失败", err)
		return
	}
	cc.Success(c, gin.H{"list": sub, "total": len(sub), "root": root})
}

// Node 单节点
// GET /api/gb28181/device-mgmt/catalog/tree/:id
func (cc *CatalogTreeController) Node(c *gin.Context) {
	db := cc.db()
	if db == nil {
		cc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		cc.FailAndAbort(c, "节点 ID 不合法", err)
		return
	}

	var n gbmodels.GbCatalogNode
	res := db.WithContext(c).Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&n)
	if res.Error != nil {
		cc.FailAndAbort(c, "查询失败", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		cc.FailAndAbort(c, "节点不存在", nil)
		return
	}
	cc.Success(c, n)
}

// AnomalyCount anomaly 未处理总数(左侧底部入口用)
// GET /api/gb28181/device-mgmt/catalog/anomaly/count
func (cc *CatalogTreeController) AnomalyCount(c *gin.Context) {
	db := cc.db()
	if db == nil {
		cc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	var count int64
	if err := db.WithContext(c).Model(&gbmodels.GbAnomalyRecord{}).Scopes(ownerDeptScope(c)).
		Where("resolved = ?", false).
		Count(&count).Error; err != nil {
		cc.FailAndAbort(c, "查 anomaly count 失败", err)
		return
	}
	cc.Success(c, gin.H{"count": count})
}

// attachMountCount 给一批节点附 mountCount(节点下挂载几个通道)
//
// 走子查询:对 node_type='channel' 直接看 mount.parent_node_id;
// 对其他类型节点(civil_code/biz_group/virtual_org)用物化路径下属 channel 计数
func (cc *CatalogTreeController) attachMountCount(c *gin.Context, db *gorm.DB, nodes []gbmodels.GbCatalogNode) []catalogNodeVO {
	out := make([]catalogNodeVO, 0, len(nodes))
	for i := range nodes {
		n := nodes[i]
		var cnt int64
		// 节点子树下所有 channel 节点
		_ = db.WithContext(c).
			Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
			Where("node_type = ? AND path LIKE ?", gbmodels.NodeTypeChannel, n.Path+"%").
			Count(&cnt).Error
		out = append(out, catalogNodeVO{GbCatalogNode: &n, MountCount: cnt})
	}
	return out
}
