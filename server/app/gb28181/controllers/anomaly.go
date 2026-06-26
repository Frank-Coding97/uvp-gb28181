package controllers

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// AnomalyController 异常治理(plan §4.2 B4)
//
//   GET /anomaly?resolved=0&page=1
//   POST /anomaly/:id/resolve
//   POST /anomaly/batch-resolve
type AnomalyController struct {
	controllers.Common
	db func() *gorm.DB
}

func NewAnomalyController() *AnomalyController {
	return &AnomalyController{db: func() *gorm.DB { return app.GormDbMysql }}
}

func (ac *AnomalyController) SetDB(p func() *gorm.DB) { ac.db = p }

type anomalyVO struct {
	*gbmodels.GbAnomalyRecord
	NodeName string `json:"nodeName"`
	NodePath string `json:"nodePath"`
}

// List anomaly 列表(默认未处理)
func (ac *AnomalyController) List(c *gin.Context) {
	db := ac.db()
	if db == nil {
		ac.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	q := db.WithContext(c).Model(&gbmodels.GbAnomalyRecord{}).Where("tenant_id = ?", tid)
	if r := c.DefaultQuery("resolved", "0"); r == "0" {
		q = q.Where("resolved = ?", false)
	} else if r == "1" {
		q = q.Where("resolved = ?", true)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		ac.FailAndAbort(c, "统计失败", err)
		return
	}
	var rs []gbmodels.GbAnomalyRecord
	if err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rs).Error; err != nil {
		ac.FailAndAbort(c, "查询失败", err)
		return
	}

	out := make([]anomalyVO, 0, len(rs))
	for i := range rs {
		r := rs[i]
		var n gbmodels.GbCatalogNode
		db.WithContext(c).Select("id, name, path").Where("id = ?", r.CatalogNodeID).Limit(1).Find(&n)
		out = append(out, anomalyVO{
			GbAnomalyRecord: &r,
			NodeName:        n.Name,
			NodePath:        n.Path,
		})
	}
	ac.Success(c, gin.H{"list": out, "total": total, "page": page, "pageSize": pageSize})
}

// resolveAction body
type resolveAction struct {
	Action         string `json:"action" binding:"required"`         // "change-type" / "change-mount" / "mark-resolved"
	TargetType     string `json:"targetType"`                          // change-type 时:civil_code/biz_group/virtual_org
	TargetParentID uint   `json:"targetParentId"`                       // change-mount 时:目标父节点
	Note           string `json:"note"`
}

// Resolve 单条 resolve
// POST /anomaly/:id/resolve
func (ac *AnomalyController) Resolve(c *gin.Context) {
	db := ac.db()
	if db == nil {
		ac.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		ac.FailAndAbort(c, "ID 不合法", err)
		return
	}
	var body resolveAction
	if err := c.ShouldBindJSON(&body); err != nil {
		ac.FailAndAbort(c, "body 解析失败", err)
		return
	}

	if err := ac.applyResolve(c, db, tid, uint(id), body); err != nil {
		ac.FailAndAbort(c, "resolve 失败", err)
		return
	}
	ac.Success(c, gin.H{"id": id, "ok": true})
}

// BatchResolve 批量 resolve
// POST /anomaly/batch-resolve
func (ac *AnomalyController) BatchResolve(c *gin.Context) {
	db := ac.db()
	if db == nil {
		ac.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	tid := tenantOf(c)
	var body struct {
		IDs            []uint `json:"ids" binding:"required"`
		Action         string `json:"action" binding:"required"`
		TargetType     string `json:"targetType"`
		TargetParentID uint   `json:"targetParentId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		ac.FailAndAbort(c, "body 解析失败", err)
		return
	}

	succeeded := make([]uint, 0, len(body.IDs))
	failed := make([]map[string]any, 0)
	for _, id := range body.IDs {
		err := ac.applyResolve(c, db, tid, id, resolveAction{
			Action:         body.Action,
			TargetType:     body.TargetType,
			TargetParentID: body.TargetParentID,
		})
		if err != nil {
			failed = append(failed, map[string]any{"id": id, "error": err.Error()})
		} else {
			succeeded = append(succeeded, id)
		}
	}
	ac.Success(c, gin.H{"succeeded": succeeded, "failed": failed})
}

// applyResolve 实际执行 resolve(单条,事务内)
func (ac *AnomalyController) applyResolve(c *gin.Context, db *gorm.DB, tid uint, id uint, body resolveAction) error {
	return db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var rec gbmodels.GbAnomalyRecord
		res := tx.Where("tenant_id = ? AND id = ?", tid, id).Limit(1).Find(&rec)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if rec.Resolved {
			return nil // 已处理:幂等,不报错
		}

		// change-type:更新 catalog_node.node_type + 清 anomaly
		if body.Action == "change-type" && body.TargetType != "" {
			if err := tx.Model(&gbmodels.GbCatalogNode{}).
				Where("tenant_id = ? AND id = ?", tid, rec.CatalogNodeID).
				Updates(map[string]any{
					"node_type":      body.TargetType,
					"anomaly":        false,
					"anomaly_reason": "",
				}).Error; err != nil {
				return err
			}
		}
		if body.Action == "change-mount" && body.TargetParentID != 0 {
			if err := tx.Model(&gbmodels.GbCatalogNode{}).
				Where("tenant_id = ? AND id = ?", tid, rec.CatalogNodeID).
				Update("parent_id", body.TargetParentID).Error; err != nil {
				return err
			}
		}

		// 标 resolved
		now := time.Now()
		return tx.Model(&rec).Updates(map[string]any{
			"resolved":        true,
			"resolved_at":     now,
			"resolved_action": body.Action,
		}).Error
	})
}
