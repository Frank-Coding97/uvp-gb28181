package device

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// RegisterInfo 注册时采集的信息
type RegisterInfo struct {
	DeviceID  string
	Transport string
	IP        string
	Port      int
	Expires   int
}

// HandleRegister 处理注册成功:自动建档(upsert),记录心跳时间事实
// keepaliveInterval = 该设备期望心跳周期(秒),用于后续在线判定
// 返回 isFirst:true 表示首次建档或从离线/未知状态重新注册(用于触发 Catalog 等首次动作)
func HandleRegister(ctx context.Context, info RegisterInfo, keepaliveInterval int) (bool, error) {
	now := time.Now()
	var expireAt *time.Time
	if info.Expires > 0 {
		t := now.Add(time.Duration(info.Expires) * time.Second)
		expireAt = &t
	}

	var isFirst bool
	err := app.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the current row so a renewal, recovery, and timeout cannot all
		// classify the same REGISTER from the same stale status.
		existing, err := gbmodels.FindByDeviceIDWithDB(ctx, tx.Set("gorm:query_option", "FOR UPDATE"), info.DeviceID)
		if err != nil {
			return fmt.Errorf("查询设备失败: %w", err)
		}
		isFirst = existing == nil || existing.Status != gbmodels.DeviceStatusOnline

		d := &gbmodels.GbDevice{
			DeviceID:          info.DeviceID,
			Transport:         info.Transport,
			IP:                info.IP,
			Port:              info.Port,
			Expires:           info.Expires,
			RegisterTime:      &now,
			RegisterExpireAt:  expireAt,
			KeepaliveTime:     &now,
			KeepaliveInterval: keepaliveInterval,
			Status:            gbmodels.DeviceStatusOnline,
		}
		var fromStatus *int8
		if existing != nil {
			d.OwnerDeptID = existing.OwnerDeptID
			from := existing.Status
			fromStatus = &from
		}
		if d.OwnerDeptID == 0 {
			d.OwnerDeptID, err = defaultOwnerDeptIDWithDB(ctx, tx)
			if err != nil {
				return err
			}
		}
		if err := gbmodels.UpsertWithDB(ctx, tx, d); err != nil {
			return fmt.Errorf("自动建档失败: %w", err)
		}

		eventType := gbmodels.DeviceEventRegisterRenewed
		if isFirst {
			eventType = gbmodels.DeviceEventRegisterOnline
		}
		expires := info.Expires
		interval := keepaliveInterval
		return gbmodels.RecordStatusEvent(tx, d, eventType, gbmodels.DeviceEventSourceRegister, fromStatus, gbmodels.DeviceStatusOnline, now, gbmodels.StatusEventMetadata{
			RegisterExpires:   &expires,
			KeepaliveInterval: &interval,
			IP:                info.IP,
			Port:              info.Port,
			Transport:         info.Transport,
		})
	})
	return isFirst, err
}

func defaultOwnerDeptID(ctx context.Context) (uint, error) {
	return defaultOwnerDeptIDWithDB(ctx, app.DB())
}

func defaultOwnerDeptIDWithDB(ctx context.Context, db *gorm.DB) (uint, error) {
	if app.ConfigYml == nil {
		return 0, fmt.Errorf("自动建档失败: 缺少配置,未设置 gb28181.device.default_owner_dept_id")
	}
	deptID := uint(app.ConfigYml.GetInt("gb28181.device.default_owner_dept_id"))
	if deptID == 0 {
		return 0, fmt.Errorf("自动建档失败: 未配置 gb28181.device.default_owner_dept_id")
	}
	var count int64
	if err := db.WithContext(ctx).Table("sys_department").Where("id = ?", deptID).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("自动建档失败: 校验默认部门失败: %w", err)
	}
	if count == 0 {
		return 0, fmt.Errorf("自动建档失败: 默认部门 %d 不存在", deptID)
	}
	return deptID, nil
}

// HandleUnregister 处理注销(Expires=0):即时置离线(事实上停止心跳 + 缓存翻转)
func HandleUnregister(ctx context.Context, deviceID string) error {
	return gbmodels.MarkOfflineWithReason(ctx, deviceID, gbmodels.DeviceEventUnregisterOffline, gbmodels.DeviceEventSourceUnregister)
}

// Keepalive 处理心跳:更新 keepalive_time 事实 + 刷新 status 缓存。
// 返回 true 表示设备从离线恢复,调用方应重新查询 Catalog 恢复通道状态。
func Keepalive(ctx context.Context, deviceID string) (bool, error) {
	return gbmodels.TouchKeepalive(ctx, deviceID)
}
