package handler

import (
	"context"
	"sync/atomic"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// DeviceInfoTrigger 注册成功后触发 DeviceInfo 查询的能力
// 跟 CatalogTrigger 独立并行:Catalog 拉通道,DeviceInfo 拉设备本体元数据(name/manufacturer/model/firmware)
// transport 需匹配设备注册时的传输协议(UDP/TCP),空值兜底 UDP
type DeviceInfoTrigger interface {
	Trigger(ctx context.Context, deviceID, dest, transport string)
}

// uacDeviceInfoTrigger 默认实现:用 UAC 发 MESSAGE(承载 DeviceInfo Query XML)
type uacDeviceInfoTrigger struct {
	uac    *uac.UAC
	sn     atomic.Int64
	cmdTTL time.Duration
}

// NewUACDeviceInfoTrigger 包装 UAC 为 DeviceInfoTrigger
func NewUACDeviceInfoTrigger(u *uac.UAC) DeviceInfoTrigger {
	return &uacDeviceInfoTrigger{uac: u, cmdTTL: 5 * time.Second}
}

// Trigger 异步向设备发 DeviceInfo 查询(失败仅记日志,不阻塞注册响应)
func (t *uacDeviceInfoTrigger) Trigger(_ context.Context, deviceID, dest, transport string) {
	if t.uac == nil {
		return
	}
	go func() {
		sn := int(t.sn.Add(1))
		body, err := manscdp.BuildDeviceInfoQuery(deviceID, sn)
		if err != nil {
			app.ZapLog.Warn("DeviceInfo 查询 XML 构造失败", zap.String("deviceId", deviceID), zap.Error(err))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), t.cmdTTL)
		defer cancel()
		if err := t.uac.SendMessage(ctx, deviceID, dest, transport, body); err != nil {
			app.ZapLog.Warn("DeviceInfo 查询发送失败",
				zap.String("deviceId", deviceID), zap.String("dest", dest),
				zap.String("transport", transport), zap.Error(err))
			return
		}
		app.ZapLog.Info("DeviceInfo 查询已发出",
			zap.String("deviceId", deviceID), zap.String("transport", transport), zap.Int("sn", sn))
	}()
}
