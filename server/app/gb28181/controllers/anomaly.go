package controllers

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/controllers"
	gbcatalog "uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// AnomalyController 异常治理(plan §4.2 B4)
//
// GET /anomaly?resolved=0&page=1
// POST /anomaly/:id/resolve
// POST /anomaly/batch-resolve
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
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	q := db.WithContext(c.Request.Context()).Model(&gbmodels.GbAnomalyRecord{}).Scopes(ownerDeptScope(c))
	switch c.DefaultQuery("resolved", "0") {
	case "0":
		q = q.Where("resolved = ?", false)
	case "1":
		q = q.Where("resolved = ?", true)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		ac.FailAndAbort(c, "统计 anomaly 失败", err)
		return
	}

	var list []gbmodels.GbAnomalyRecord
	if err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		ac.FailAndAbort(c, "查询 anomaly 失败", err)
		return
	}

	vos := make([]anomalyVO, 0, len(list))
	for _, r := range list {
		var n gbmodels.GbCatalogNode
		_ = db.WithContext(c.Request.Context()).
			Scopes(ownerDeptScope(c)).
			Select("id, name, path").
			Where("id = ?", r.CatalogNodeID).
			Limit(1).
			Find(&n).Error
		vos = append(vos, anomalyVO{
			GbAnomalyRecord: &r,
			NodeName:        n.Name,
			NodePath:        n.Path,
		})
	}

	ac.Success(c, gin.H{
		"list":     vos,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

type resolveAction struct {
	Action         string `json:"action" binding:"required"` // "change-type" / "change-mount" / "mark-resolved"
	TargetType     string `json:"targetType"`                // change-type 时:civil_code/biz_group/virtual_org
	TargetParentID uint   `json:"targetParentId"`            // change-mount 时:目标父节点
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

	if err := ac.applyResolve(c, db, uint(id), body); err != nil {
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
		err := ac.applyResolve(c, db, id, resolveAction{
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
func (ac *AnomalyController) applyResolve(c *gin.Context, db *gorm.DB, id uint, body resolveAction) error {
	return db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var rec gbmodels.GbAnomalyRecord
		res := tx.Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&rec)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if rec.Resolved {
			return nil
		}

		var node gbmodels.GbCatalogNode
		if body.Action != "mark-resolved" {
			nodeRes := tx.Scopes(ownerDeptScope(c)).Where("id = ?", rec.CatalogNodeID).Limit(1).Find(&node)
			if nodeRes.Error != nil {
				return nodeRes.Error
			}
			if nodeRes.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
		}

		switch body.Action {
		case "change-type":
			if body.TargetType == "" {
				return errors.New("targetType 不能为空")
			}
			if err := tx.Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
				Where("id = ?", rec.CatalogNodeID).
				Updates(map[string]any{
					"node_type":      body.TargetType,
					"anomaly":        false,
					"anomaly_reason": "",
				}).Error; err != nil {
				return err
			}
		case "change-mount":
			if body.TargetParentID == 0 {
				return errors.New("targetParentId 不能为空")
			}
			if body.TargetParentID == node.ID {
				return errors.New("不能挂载到自身")
			}

			var target gbmodels.GbCatalogNode
			targetRes := tx.Scopes(ownerDeptScope(c)).Where("id = ?", body.TargetParentID).Limit(1).Find(&target)
			if targetRes.Error != nil {
				return targetRes.Error
			}
			if targetRes.RowsAffected == 0 {
				return errors.New("目标父节点不存在")
			}
			if target.OwnerDeptID != rec.OwnerDeptID || node.OwnerDeptID != rec.OwnerDeptID {
				return errors.New("目标父节点不在当前归属部门")
			}
			if strings.HasPrefix(target.Path, node.Path) {
				return errors.New("不能挂载到自己的子树下")
			}

			oldPath := node.Path
			newPath := gbcatalog.BuildPath(target.Path, node.ID)
			newDepth := gbcatalog.DepthFromPath(newPath)
			if err := tx.Model(&gbmodels.GbCatalogNode{}).Scopes(ownerDeptScope(c)).
				Where("id = ?", node.ID).
				Updates(map[string]any{
					"parent_id":      body.TargetParentID,
					"path":           newPath,
					"depth":          newDepth,
					"anomaly":        false,
					"anomaly_reason": "",
				}).Error; err != nil {
				return err
			}

			var descendants []gbmodels.GbCatalogNode
			if err := tx.Scopes(ownerDeptScope(c)).
				Where("owner_dept_id = ? AND path LIKE ? AND id <> ?", rec.OwnerDeptID, oldPath+"%", node.ID).
				Find(&descendants).Error; err != nil {
				return err
			}
			for _, child := range descendants {
				childPath := strings.Replace(child.Path, oldPath, newPath, 1)
				childDepth := gbcatalog.DepthFromPath(childPath)
				if err := tx.Model(&gbmodels.GbCatalogNode{}).
					Where("id = ? AND owner_dept_id = ?", child.ID, rec.OwnerDeptID).
					Updates(map[string]any{
						"path":  childPath,
						"depth": childDepth,
					}).Error; err != nil {
					return err
				}
			}
		case "mark-resolved":
			// 只标记记录已处理,不改节点结构
		default:
			return errors.New("不支持的 resolve action")
		}

		now := time.Now()
		return tx.Model(&rec).Updates(map[string]any{
			"resolved":        true,
			"resolved_at":     now,
			"resolved_action": body.Action,
		}).Error
	})
}
