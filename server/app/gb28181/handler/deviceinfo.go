package handler

import (
	"context"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"

	"go.uber.org/zap"
)

// HandleDeviceInfoResponse 处理一条 DeviceInfo 应答
//
// 语义:设备本体元数据回写 gb_device.name / manufacturer / model / firmware
// 只覆盖"非空且新值不同"的字段,避免用空串清掉已有数据;
// 若 DB 未就绪(单测/早启动)直接 no-op,不 panic。
// deviceID / callID / cseq 是显式参数，不是 `traceFields ...zap.Field` ——
// 理由同 HandleCatalogResponse：`.With(zap.String(...))` 让字段名在日志语句里读不到。
func HandleDeviceInfoResponse(ctx context.Context, body []byte, deviceID, callID, cseq string) {
	logger := app.Log(ctx).Named("gb28181.deviceinfo")
	resp, err := manscdp.ParseDeviceInfoResponse(body)
	// INFO（两条同一判据）：应答体坏了 / 应答体缺 DeviceID —— 都是**对方的错**，
	// 我们丢弃该应答并结束，对外行为与"没有这次应答"等价。下面 `device_missing`
	// （设备不在库）**保持 WARN**：那是本地数据与设备行为不一致，要人去看。
	if err != nil {
		logger.Info("DeviceInfo 应答解析失败",
			zap.String("event", "gb28181.deviceinfo.response_parse_failed"),
			zap.String("device_id", deviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
			zap.String("reason_code", "device_info_response_invalid"), logging.Error(err))
		return
	}
	if resp.DeviceID == "" {
		logger.Info("DeviceInfo 应答缺少 DeviceID,忽略",
			zap.String("event", "gb28181.deviceinfo.missing_device_id"),
			// 应答体里没有 DeviceID，但信封上有 —— 否则这条 INFO 就成了"某台设备发了
			// 一个坏应答"里那个说不出是谁的坏应答。
			zap.String("device_id", deviceID), zap.String("call_id", callID), zap.String("cseq", cseq))
		return
	}

	db := app.GormDbMysql
	if db == nil {
		logger.Debug("DB 未初始化,跳过 DeviceInfo 回写",
			zap.String("event", "gb28181.deviceinfo.store_unavailable"),
			zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq))
		return
	}

	// 查设备(可能还没在库里,极端场景 DeviceInfo 早于 REGISTER 落地——不建档,直接跳过)
	var dev gbmodels.GbDevice
	res := db.WithContext(ctx).Where("device_id = ?", resp.DeviceID).Limit(1).Find(&dev)
	if res.Error != nil {
		logger.Warn("DeviceInfo 回写查设备失败",
			zap.String("event", "gb28181.deviceinfo.lookup_failed"),
			zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
			logging.Error(res.Error))
		return
	}
	if res.RowsAffected == 0 {
		logger.Warn("DeviceInfo 应答对应设备不在库,忽略",
			zap.String("event", "gb28181.deviceinfo.device_missing"),
			zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq))
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
		logger.Debug("DeviceInfo 应答无字段变更",
			zap.String("event", "gb28181.deviceinfo.unchanged"),
			zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq))
		return
	}
	if err := db.WithContext(ctx).Model(&gbmodels.GbDevice{}).
		Where("id = ?", dev.ID).
		Updates(updates).Error; err != nil {
		logger.Warn("DeviceInfo 回写失败",
			zap.String("event", "gb28181.deviceinfo.update_failed"),
			zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
			logging.Error(err))
		return
	}
	logger.Info("DeviceInfo 回写成功",
		zap.String("event", "gb28181.deviceinfo.updated"),
		zap.String("device_id", resp.DeviceID), zap.String("call_id", callID), zap.String("cseq", cseq),
		zap.Int("updated_field_count", len(updates)),
		zap.Bool("name_updated", updates["name"] != nil),
		zap.Bool("manufacturer_updated", updates["manufacturer"] != nil),
		zap.Bool("model_updated", updates["model"] != nil),
		zap.Bool("firmware_updated", updates["firmware"] != nil))
}
