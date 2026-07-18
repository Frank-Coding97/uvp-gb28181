package controllers

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// DeleteDevice 单个设备硬删除(dept-scoped)
// DELETE /device-mgmt/device/:id
//
// 级联:
//   - gb_channel(device_id 匹配设备国标 20 位编码)
//   - gb_channel_mount(channel_id 属于被删通道)
//   - gb_catalog_node(device_id 或 channel_id 引用被删设备/通道)
//   - gb_anomaly_record(catalog_node_id 引用被删节点)
//
// 语义:用户主动删除设备 = 该设备本身消失,所有 mount/node 悬空引用一起清。
// 跟 catalog notify DEL 的 preserve-multi-mounted 不同(那是"从某目录移除")。
func (dc *DeviceMgmtController) DeleteDevice(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	if err := dc.deleteDeviceByID(c, db, uint(id)); err != nil {
		dc.FailAndAbort(c, "删除失败", err)
		return
	}
	dc.Success(c, gin.H{"id": id, "ok": true})
}

// BatchDeleteDevices 批量删除设备
// POST /device-mgmt/device/batch-delete   body: {ids:[]}
func (dc *DeviceMgmtController) BatchDeleteDevices(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	var body struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "body 解析失败", err)
		return
	}
	if len(body.IDs) == 0 {
		dc.FailAndAbort(c, "ids 不能为空", nil)
		return
	}

	succeeded := make([]uint, 0, len(body.IDs))
	failed := make([]map[string]any, 0)
	for _, id := range body.IDs {
		if id == 0 {
			failed = append(failed, map[string]any{"id": id, "error": "ID 不合法"})
			continue
		}
		if err := dc.deleteDeviceByID(c, db, id); err != nil {
			failed = append(failed, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		succeeded = append(succeeded, id)
	}
	dc.Success(c, gin.H{"succeeded": succeeded, "failed": failed})
}

// DeleteChannel 单个通道硬删除(dept-scoped)
// DELETE /device-mgmt/channel/:id
//
// 级联:
//   - gb_channel_mount(所有引用该通道的挂载)
//   - gb_catalog_node(channel_id 引用该通道)
//   - gb_anomaly_record(catalog_node_id 引用被删节点)
func (dc *DeviceMgmtController) DeleteChannel(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		dc.FailAndAbort(c, "ID 不合法", err)
		return
	}
	if err := dc.deleteChannelByID(c, db, uint(id)); err != nil {
		dc.FailAndAbort(c, "删除失败", err)
		return
	}
	dc.Success(c, gin.H{"id": id, "ok": true})
}

// BatchDeleteChannels 批量删除通道
// POST /device-mgmt/channel/batch-delete   body: {ids:[]}
func (dc *DeviceMgmtController) BatchDeleteChannels(c *gin.Context) {
	db := dc.db()
	if db == nil {
		dc.FailAndAbort(c, "DB 未就绪", nil)
		return
	}
	var body struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dc.FailAndAbort(c, "body 解析失败", err)
		return
	}
	if len(body.IDs) == 0 {
		dc.FailAndAbort(c, "ids 不能为空", nil)
		return
	}

	succeeded := make([]uint, 0, len(body.IDs))
	failed := make([]map[string]any, 0)
	for _, id := range body.IDs {
		if id == 0 {
			failed = append(failed, map[string]any{"id": id, "error": "ID 不合法"})
			continue
		}
		if err := dc.deleteChannelByID(c, db, id); err != nil {
			failed = append(failed, map[string]any{"id": id, "error": err.Error()})
			continue
		}
		succeeded = append(succeeded, id)
	}
	dc.Success(c, gin.H{"succeeded": succeeded, "failed": failed})
}

// deleteDeviceByID 事务内物理删除单个设备及其级联数据
//
// 硬删除:gb_device / gb_channel / gb_catalog_node 都是 gorm.DeletedAt 软删模型,
// 必须用 .Unscoped() 才能真正 DELETE。否则被软删的 device_id 仍占用 uk_device_id
// 唯一索引,设备再次 REGISTER 时 Upsert 会撞索引报 Duplicate entry。
func (dc *DeviceMgmtController) deleteDeviceByID(c *gin.Context, db *gorm.DB, id uint) error {
	return db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// 1. 查设备(dept-scoped)
		var dev gbmodels.GbDevice
		res := tx.Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&dev)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("设备不存在或无权限")
		}

		// 2. 查该设备下所有通道 ID(通过国标 device_id 关联,dept-scoped)
		var channelIDs []uint
		if err := tx.Model(&gbmodels.GbChannel{}).
			Scopes(ownerDeptScope(c)).
			Where("device_id = ?", dev.DeviceID).
			Pluck("id", &channelIDs).Error; err != nil {
			return err
		}

		// 3. 删通道 mount(gb_channel_mount 无软删字段,Delete 就是物理删)
		if len(channelIDs) > 0 {
			if err := tx.Scopes(ownerDeptScope(c)).
				Where("channel_id IN ?", channelIDs).
				Delete(&gbmodels.GbChannelMount{}).Error; err != nil {
				return err
			}
		}

		// 4. 查所有涉及本设备的 catalog_node ID(设备节点 + 该设备名下通道节点)
		var nodeIDs []uint
		nodeQuery := tx.Model(&gbmodels.GbCatalogNode{}).
			Scopes(ownerDeptScope(c)).
			Where("device_id = ?", dev.ID)
		if len(channelIDs) > 0 {
			nodeQuery = nodeQuery.Or("channel_id IN ?", channelIDs)
		}
		if err := nodeQuery.Pluck("id", &nodeIDs).Error; err != nil {
			return err
		}

		// 5. 删 anomaly_record + catalog_node(catalog_node 是软删模型,必须 Unscoped)
		if len(nodeIDs) > 0 {
			if err := tx.Scopes(ownerDeptScope(c)).
				Where("catalog_node_id IN ?", nodeIDs).
				Delete(&gbmodels.GbAnomalyRecord{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Scopes(ownerDeptScope(c)).
				Where("id IN ?", nodeIDs).
				Delete(&gbmodels.GbCatalogNode{}).Error; err != nil {
				return err
			}
		}

		// 6. 删通道(gb_channel 是软删模型,必须 Unscoped)
		if len(channelIDs) > 0 {
			if err := tx.Unscoped().Scopes(ownerDeptScope(c)).
				Where("id IN ?", channelIDs).
				Delete(&gbmodels.GbChannel{}).Error; err != nil {
				return err
			}
		}

		// 7. 删设备本身(gb_device 是软删模型,必须 Unscoped)
		if err := tx.Unscoped().Scopes(ownerDeptScope(c)).
			Where("id = ?", dev.ID).
			Delete(&gbmodels.GbDevice{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// deleteChannelByID 事务内物理删除单个通道及其级联数据
func (dc *DeviceMgmtController) deleteChannelByID(c *gin.Context, db *gorm.DB, id uint) error {
	return db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// 1. 查通道(dept-scoped)
		var ch gbmodels.GbChannel
		res := tx.Scopes(ownerDeptScope(c)).Where("id = ?", id).Limit(1).Find(&ch)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("通道不存在或无权限")
		}

		// 2. 删该通道所有 mount(无软删字段)
		if err := tx.Scopes(ownerDeptScope(c)).
			Where("channel_id = ?", ch.ID).
			Delete(&gbmodels.GbChannelMount{}).Error; err != nil {
			return err
		}

		// 3. 查涉及本通道的 catalog_node
		var nodeIDs []uint
		if err := tx.Model(&gbmodels.GbCatalogNode{}).
			Scopes(ownerDeptScope(c)).
			Where("channel_id = ?", ch.ID).
			Pluck("id", &nodeIDs).Error; err != nil {
			return err
		}

		// 4. 删 anomaly_record + catalog_node(catalog_node 软删模型,Unscoped)
		if len(nodeIDs) > 0 {
			if err := tx.Scopes(ownerDeptScope(c)).
				Where("catalog_node_id IN ?", nodeIDs).
				Delete(&gbmodels.GbAnomalyRecord{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Scopes(ownerDeptScope(c)).
				Where("id IN ?", nodeIDs).
				Delete(&gbmodels.GbCatalogNode{}).Error; err != nil {
				return err
			}
		}

		// 5. 删通道(gb_channel 软删模型,Unscoped)
		if err := tx.Unscoped().Scopes(ownerDeptScope(c)).
			Where("id = ?", ch.ID).
			Delete(&gbmodels.GbChannel{}).Error; err != nil {
			return err
		}
		return nil
	})
}
