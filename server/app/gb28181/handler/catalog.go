package handler

import (
	"context"
	"sync"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

// catalogAggregator 按 deviceID 聚合分包的 Catalog 应答
// 国标 Catalog 应答可能分多条 MESSAGE 到达,需按 SumNum 累积齐后落库
type catalogAggregator struct {
	mu    sync.Mutex
	cache map[string]*catalogBucket // key: deviceID
}

type catalogBucket struct {
	sumNum   int
	received int
}

var catalogAgg = &catalogAggregator{cache: make(map[string]*catalogBucket)}

// HandleCatalogResponse 处理一条 Catalog 应答:逐项入库 + 分包计数
func HandleCatalogResponse(ctx context.Context, body []byte) {
	resp, err := manscdp.ParseCatalogResponse(body)
	if err != nil {
		app.ZapLog.Warn("Catalog 应答解析失败", zap.Error(err))
		return
	}

	for _, item := range resp.DeviceList.Items {
		if item.DeviceID == "" {
			continue
		}
		status := gbmodels.ChannelStatusOffline
		if item.IsOnline() {
			status = gbmodels.ChannelStatusOnline
		}
		ch := &gbmodels.GbChannel{
			DeviceID:     resp.DeviceID,
			ChannelID:    item.DeviceID, // Catalog Item 的 DeviceID 即通道编码
			Name:         item.Name,
			Manufacturer: item.Manufacturer,
			Model:        item.Model,
			Owner:        item.Owner,
			CivilCode:    item.CivilCode,
			ParentID:     item.ParentID,
			PTZType:      int8(item.PTZType),
			Longitude:    item.Longitude,
			Latitude:     item.Latitude,
			Status:       status,
		}
		if err := gbmodels.UpsertChannel(ctx, ch); err != nil {
			app.ZapLog.Error("通道入库失败", zap.String("channelId", item.DeviceID), zap.Error(err))
		}
	}

	// 分包聚合计数(便于日志/判断是否收齐)
	catalogAgg.mu.Lock()
	b := catalogAgg.cache[resp.DeviceID]
	if b == nil {
		b = &catalogBucket{sumNum: resp.SumNum}
		catalogAgg.cache[resp.DeviceID] = b
	}
	b.received += len(resp.DeviceList.Items)
	done := b.received >= b.sumNum && b.sumNum > 0
	received, sumNum := b.received, b.sumNum
	if done {
		delete(catalogAgg.cache, resp.DeviceID)
	}
	catalogAgg.mu.Unlock()

	app.ZapLog.Info("Catalog 应答处理",
		zap.String("deviceId", resp.DeviceID),
		zap.Int("本条", len(resp.DeviceList.Items)),
		zap.Int("累计", received), zap.Int("总数", sumNum), zap.Bool("收齐", done))
}
