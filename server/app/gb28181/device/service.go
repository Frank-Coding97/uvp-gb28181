package device

import (
	"context"
	"fmt"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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

	existing, err := gbmodels.FindByDeviceID(ctx, info.DeviceID)
	if err != nil {
		return false, fmt.Errorf("查询设备失败: %w", err)
	}
	// 首次:不存在或上次为离线 → 视为新注册,需要触发 Catalog
	isFirst := existing == nil || existing.Status != gbmodels.DeviceStatusOnline

	d := &gbmodels.GbDevice{
		DeviceID:          info.DeviceID,
		Transport:         info.Transport,
		IP:                info.IP,
		Port:              info.Port,
		Expires:           info.Expires,
		RegisterTime:      &now,
		RegisterExpireAt:  expireAt,
		KeepaliveTime:     &now, // 注册也视为一次心跳事实
		KeepaliveInterval: keepaliveInterval,
		Status:            gbmodels.DeviceStatusOnline, // 物化缓存,顺手刷
	}
	if err := gbmodels.Upsert(ctx, d); err != nil {
		return false, fmt.Errorf("自动建档失败: %w", err)
	}
	return isFirst, nil
}

// HandleUnregister 处理注销(Expires=0):即时置离线(事实上停止心跳 + 缓存翻转)
func HandleUnregister(ctx context.Context, deviceID string) error {
	return gbmodels.MarkOffline(ctx, deviceID)
}

// Keepalive 处理心跳:更新 keepalive_time 事实 + 刷新 status 缓存
func Keepalive(ctx context.Context, deviceID string) error {
	return gbmodels.TouchKeepalive(ctx, deviceID)
}
