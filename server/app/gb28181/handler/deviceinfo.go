package handler

import (
	"context"
	"strings"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// HandleDeviceInfoResponse 处理一条 DeviceInfo 应答
//
// 语义:设备本体元数据回写 gb_device.name / manufacturer / model / firmware
// 只覆盖"非空且新值不同"的字段,避免用空串清掉已有数据;
// 若 DB 未就绪(单测/早启动)直接 no-op,不 panic。
func HandleDeviceInfoResponse(ctx context.Context, body []byte) {
	resp, err := manscdp.ParseDeviceInfoResponse(body)
	if err != nil {
		app.ZapLog.Warn("DeviceInfo 应答解析失败", zap.Error(err))
		return
	}
	if resp.DeviceID == "" {
		app.ZapLog.Warn("DeviceInfo 应答缺少 DeviceID,忽略")
		return
	}

	db := app.GormDbMysql
	if db == nil {
		app.ZapLog.Debug("DB 未初始化,跳过 DeviceInfo 回写", zap.String("deviceId", resp.DeviceID))
		return
	}

	// 查设备(可能还没在库里,极端场景 DeviceInfo 早于 REGISTER 落地——不建档,直接跳过)
	var dev gbmodels.GbDevice
	res := db.WithContext(ctx).Where("device_id = ?", resp.DeviceID).Limit(1).Find(&dev)
	if res.Error != nil {
		app.ZapLog.Error("DeviceInfo 回写查设备失败",
			zap.String("deviceId", resp.DeviceID), zap.Error(res.Error))
		return
	}
	if res.RowsAffected == 0 {
		app.ZapLog.Warn("DeviceInfo 应答对应设备不在库,忽略",
			zap.String("deviceId", resp.DeviceID))
		return
	}

	// 只覆盖"非空且新值不同"的字段
	updates := map[string]any{}
	if name := strings.TrimSpace(resp.DeviceName); name != "" && name != dev.Name {
		updates["name"] = name
	}
	if m := strings.TrimSpace(resp.Manufacturer); m != "" && m != dev.Manufacturer {
		updates["manufacturer"] = m
	}
	if m := strings.TrimSpace(resp.Model); m != "" && m != dev.Model {
		updates["model"] = m
	}
	if fw := strings.TrimSpace(resp.Firmware); fw != "" && fw != dev.Firmware {
		updates["firmware"] = fw
	}
	if len(updates) == 0 {
		app.ZapLog.Debug("DeviceInfo 应答无字段变更",
			zap.String("deviceId", resp.DeviceID))
		return
	}
	if err := db.WithContext(ctx).Model(&gbmodels.GbDevice{}).
		Where("id = ?", dev.ID).
		Updates(updates).Error; err != nil {
		app.ZapLog.Error("DeviceInfo 回写失败",
			zap.String("deviceId", resp.DeviceID), zap.Error(err))
		return
	}
	app.ZapLog.Info("DeviceInfo 回写成功",
		zap.String("deviceId", resp.DeviceID),
		zap.Any("updates", updates))
}
