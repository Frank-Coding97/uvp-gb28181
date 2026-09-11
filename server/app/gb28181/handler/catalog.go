package handler

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/global/app"

	"go.uber.org/zap"
)

const catalogAggregationTimeout = 30 * time.Second

// catalogAggregator 按 deviceID + SN 聚合分包的 Catalog 应答。
type catalogAggregator struct {
	mu    sync.Mutex
	cache map[catalogAggregateKey]*catalogBucket
}

type catalogAggregateKey struct {
	deviceID string
	sn       int
}

type catalogBucket struct {
	sumNum int
	items  map[string]manscdp.CatalogItem
	order  []string
	timer  *time.Timer
}

var catalogAgg = &catalogAggregator{cache: make(map[catalogAggregateKey]*catalogBucket)}

func (a *catalogAggregator) add(resp *manscdp.CatalogResponse) (items []manscdp.CatalogItem, received, total int, done bool) {
	if resp == nil {
		return nil, 0, 0, false
	}
	key := catalogAggregateKey{deviceID: resp.DeviceID, sn: resp.SN}
	a.mu.Lock()
	defer a.mu.Unlock()

	// 标准空结果是一个完整终态，不能留下永不完成的 SumNum=0 bucket。
	if resp.SumNum == 0 && len(resp.DeviceList.Items) == 0 {
		if old := a.cache[key]; old != nil {
			old.timer.Stop()
			delete(a.cache, key)
		}
		return nil, 0, 0, true
	}

	b := a.cache[key]
	if b == nil {
		b = &catalogBucket{
			sumNum: resp.SumNum,
			items:  make(map[string]manscdp.CatalogItem),
		}
		a.cache[key] = b
	}
	if resp.SumNum > b.sumNum {
		b.sumNum = resp.SumNum
	}
	for _, item := range resp.DeviceList.Items {
		if item.DeviceID == "" {
			continue
		}
		if _, exists := b.items[item.DeviceID]; !exists {
			b.order = append(b.order, item.DeviceID)
		}
		// 相同目录编码的重复包不增加进度；较新的字段覆盖旧值。
		b.items[item.DeviceID] = item
	}
	if b.sumNum <= 0 {
		// 兼容少量老设备漏填 SumNum 的单包应答。
		b.sumNum = len(b.items)
	}
	received, total = len(b.items), b.sumNum
	done = total > 0 && received >= total
	if done {
		items = make([]manscdp.CatalogItem, 0, len(b.order))
		for _, id := range b.order {
			items = append(items, b.items[id])
		}
		if b.timer != nil {
			b.timer.Stop()
		}
		delete(a.cache, key)
		return items, received, total, true
	}

	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(catalogAggregationTimeout, func() {
		a.mu.Lock()
		if current := a.cache[key]; current == b {
			delete(a.cache, key)
		}
		a.mu.Unlock()
	})
	return nil, received, total, false
}

func (a *catalogAggregator) reset() {
	a.mu.Lock()
	for _, bucket := range a.cache {
		if bucket.timer != nil {
			bucket.timer.Stop()
		}
	}
	a.cache = make(map[catalogAggregateKey]*catalogBucket)
	a.mu.Unlock()
}

func (a *catalogAggregator) active() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.cache)
}

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
	db := app.GormDbMysql
	if app.ConfigYml != nil {
		db = app.DB()
	}
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

// HandleCatalogResponse 处理一条 Catalog 应答:按设备与 SN 聚合，收齐后一次性入库。
//
// A4 改造:
//   - 旧路径:gbmodels.UpsertChannel(只写 gb_channel)
//   - 新路径:catalog.Pipeline.Ingest(写 gb_channel + gb_catalog_node + gb_channel_mount
//   - classify/anomaly 兜底)
//
// pipeline 不可用时(db nil)回退到旧路径,保证生产兼容
func HandleCatalogResponse(ctx context.Context, body []byte) {
	resp, err := manscdp.ParseCatalogResponse(body)
	if err != nil {
		app.ZapLog.Warn("Catalog 应答解析失败", zap.Error(err))
		return
	}

	aggregated, received, sumNum, done := catalogAgg.add(resp)
	if done {
		pipeline := getCatalogPipeline()
		if pipeline != nil {
			items := make([]catalog.CatalogItem, 0, len(aggregated))
			for _, it := range aggregated {
				if it.DeviceID == "" {
					continue
				}
				items = append(items, manscdpToCatalogItem(it))
			}
			if e := pipeline.Ingest(ctx, catalog.Sender{SourceDeviceID: resp.DeviceID}, items); e != nil {
				app.ZapLog.Error("Catalog Pipeline.Ingest 失败(部分通道未入库)",
					zap.String("deviceId", resp.DeviceID), zap.Error(e))
			}
		} else {
			app.ZapLog.Debug("CatalogPipeline 不可用,跳过 catalog 入库", zap.String("deviceId", resp.DeviceID))
		}
	}

	app.ZapLog.Info("Catalog 应答处理",
		zap.String("deviceId", resp.DeviceID),
		zap.Int("sn", resp.SN),
		zap.Int("本条", len(resp.DeviceList.Items)),
		zap.Int("累计", received), zap.Int("总数", sumNum), zap.Bool("收齐", done))
}

// manscdpToCatalogItem 把 manscdp DTO 转 catalog DTO(无依赖,易测)
func manscdpToCatalogItem(it manscdp.CatalogItem) catalog.CatalogItem {
	return catalog.CatalogItem{
		DeviceID:        it.DeviceID,
		Name:            it.Name,
		Manufacturer:    it.Manufacturer,
		Model:           it.Model,
		Owner:           it.Owner,
		CivilCode:       it.CivilCode,
		ParentID:        it.ParentID,
		BusinessGroupID: it.BusinessGroupID,
		Parental:        it.Parental,
		PTZType:         it.PTZType,
		Longitude:       it.Longitude,
		Latitude:        it.Latitude,
		StatusOn:        it.IsOnline(),
		Address:         it.Address,
		Secrecy:         int8(it.Secrecy),
		RegisterWay:     int8(it.RegisterWay),
	}
}
