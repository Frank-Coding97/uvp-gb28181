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

// CatalogTrigger 注册成功后触发 Catalog 查询的能力(便于注入与测试)
type CatalogTrigger interface {
	Trigger(ctx context.Context, deviceID, dest string)
}

// uacCatalogTrigger 默认实现:用 UAC 发 MESSAGE(承载 Catalog Query XML)
type uacCatalogTrigger struct {
	uac    *uac.UAC
	sn     atomic.Int64
	cmdTTL time.Duration // 单次发送超时
}

// NewUACCatalogTrigger 包装 UAC 为 CatalogTrigger
func NewUACCatalogTrigger(u *uac.UAC) CatalogTrigger {
	return &uacCatalogTrigger{uac: u, cmdTTL: 5 * time.Second}
}

// Trigger 异步向设备发 Catalog 查询(失败仅记日志,不阻塞注册响应)
func (t *uacCatalogTrigger) Trigger(_ context.Context, deviceID, dest string) {
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
		ctx, cancel := context.WithTimeout(context.Background(), t.cmdTTL)
		defer cancel()
		if err := t.uac.SendMessage(ctx, deviceID, dest, body); err != nil {
			app.ZapLog.Warn("Catalog 查询发送失败",
				zap.String("deviceId", deviceID), zap.String("dest", dest), zap.Error(err))
			return
		}
		app.ZapLog.Info("Catalog 查询已发出", zap.String("deviceId", deviceID), zap.Int("sn", sn))
	}()
}
