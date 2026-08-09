package handler

import (
	"context"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// CatalogTrigger 设备首次注册或从离线恢复后触发 Catalog 查询的能力(便于注入与测试)
// transport 需匹配设备注册时的传输协议(UDP/TCP),空值兜底 UDP
type CatalogTrigger interface {
	Trigger(ctx context.Context, deviceID, dest, transport string)
}

// uacCatalogTrigger 默认实现:用 UAC 发 MESSAGE(承载 Catalog Query XML)
type uacCatalogTrigger struct {
	uac *uac.UAC
	sn  atomic.Int64
}

// NewUACCatalogTrigger 包装 UAC 为 CatalogTrigger
func NewUACCatalogTrigger(u *uac.UAC) CatalogTrigger {
	return &uacCatalogTrigger{uac: u}
}

// Trigger 异步向设备发 Catalog 查询(失败仅记日志,不阻塞注册响应)
func (t *uacCatalogTrigger) Trigger(_ context.Context, deviceID, dest, transport string) {
	if t.uac == nil {
		return
	}
	go func() {
		sn := int(t.sn.Add(1))
		body, err := manscdp.BuildCatalogQuery(deviceID, sn)
		if err != nil {
			app.ZapLog.Warn("Catalog 查询 XML 构造失败", zap.String("deviceId", deviceID), zap.Error(err))
			return
		}
		if err := t.uac.SendMessage(context.Background(), deviceID, dest, transport, body); err != nil {
			app.ZapLog.Warn("Catalog 查询发送失败",
				zap.String("deviceId", deviceID), zap.String("dest", dest),
				zap.String("transport", transport), zap.Error(err))
			return
		}
		app.ZapLog.Info("Catalog 查询已发出",
			zap.String("deviceId", deviceID), zap.String("transport", transport), zap.Int("sn", sn))
	}()
}
