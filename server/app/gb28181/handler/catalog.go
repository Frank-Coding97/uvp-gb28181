package handler

import (
	"context"
	"sync"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
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

// catalogPipeline 全局入库管道(A4 改造:不再直接 UpsertChannel,投递到 catalog.Pipeline)
// 包内可见单例,首次使用 lazy 装配;测试可调 SetCatalogPipeline 注入替身
var (
	catalogPipelineMu sync.RWMutex
	catalogPipeline   *catalog.Pipeline
)

// SetCatalogPipeline 注入 Pipeline(给单测 / bootstrap 用)
func SetCatalogPipeline(p *catalog.Pipeline) {
	catalogPipelineMu.Lock()
	catalogPipeline = p
	catalogPipelineMu.Unlock()
}

// getCatalogPipeline lazy init,首次返回基于 app.DB() 的 Pipeline
func getCatalogPipeline() *catalog.Pipeline {
	catalogPipelineMu.RLock()
	p := catalogPipeline
	catalogPipelineMu.RUnlock()
	if p != nil {
		return p
	}
	// 直接读底层 *gorm.DB,避开 app.DB() 在 ConfigYml=nil 时的 panic
	db := app.GormDbMysql
	if db == nil {
		return nil
	}
	catalogPipelineMu.Lock()
	defer catalogPipelineMu.Unlock()
	if catalogPipeline == nil {
		catalogPipeline = catalog.New(db.Session(&gorm.Session{NewDB: true}))
	}
	return catalogPipeline
}

// HandleCatalogResponse 处理一条 Catalog 应答:逐项入库 + 分包计数
//
// A4 改造:
//   - 旧路径:gbmodels.UpsertChannel(只写 gb_channel)
//   - 新路径:catalog.Pipeline.Ingest(写 gb_channel + gb_catalog_node + gb_channel_mount
//             + classify/anomaly 兜底)
//
// pipeline 不可用时(db nil)回退到旧路径,保证生产兼容
func HandleCatalogResponse(ctx context.Context, body []byte) {
	resp, err := manscdp.ParseCatalogResponse(body)
	if err != nil {
		app.ZapLog.Warn("Catalog 应答解析失败", zap.Error(err))
		return
	}

	pipeline := getCatalogPipeline()

	if pipeline != nil {
		items := make([]catalog.CatalogItem, 0, len(resp.DeviceList.Items))
		for _, it := range resp.DeviceList.Items {
			if it.DeviceID == "" {
				continue
			}
			items = append(items, manscdpToCatalogItem(it))
		}
		if e := pipeline.Ingest(ctx, catalog.Sender{
			TenantID:       0, // 兼容:未严格落多租户,pipeline 内部默认 tenant 1
			SourceDeviceID: resp.DeviceID,
		}, items); e != nil {
			app.ZapLog.Error("Catalog Pipeline.Ingest 失败(部分通道未入库)",
				zap.String("deviceId", resp.DeviceID), zap.Error(e))
		}
	} else {
		// 兼容回退:db 未初始化(单测/早启动)
		app.ZapLog.Debug("CatalogPipeline 不可用,跳过 catalog 入库", zap.String("deviceId", resp.DeviceID))
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

// manscdpToCatalogItem 把 manscdp DTO 转 catalog DTO(无依赖,易测)
func manscdpToCatalogItem(it manscdp.CatalogItem) catalog.CatalogItem {
	return catalog.CatalogItem{
		DeviceID:     it.DeviceID,
		Name:         it.Name,
		Manufacturer: it.Manufacturer,
		Model:        it.Model,
		Owner:        it.Owner,
		CivilCode:    it.CivilCode,
		ParentID:     it.ParentID,
		PTZType:      it.PTZType,
		Longitude:    it.Longitude,
		Latitude:     it.Latitude,
		StatusOn:     it.IsOnline(),
	}
}
