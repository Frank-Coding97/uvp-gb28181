package subscribe

import (
	"context"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// CatalogProcessor forwards subscription notifications through the established catalog pipeline.
type CatalogProcessor struct {
	pipeline                  *catalog.Pipeline
	ignoreOfflineStatusNotify func() bool
	mu                        sync.Mutex
	buckets                   map[catalogNotifyKey]*catalogNotifyBucket
}

type catalogNotifyKey struct {
	deviceID string
	callID   string
	sn       int
}

type catalogNotifyBucket struct {
	sumNum int
	items  map[string]manscdp.CatalogItem
	order  []string
	timer  *time.Timer
}

func NewCatalogProcessor(pipeline *catalog.Pipeline) *CatalogProcessor {
	return &CatalogProcessor{
		pipeline:                  pipeline,
		ignoreOfflineStatusNotify: gbconfig.IgnoreChannelOfflineStatusNotify,
		buckets:                   make(map[catalogNotifyKey]*catalogNotifyBucket),
	}
}

func (p *CatalogProcessor) aggregate(notify *manscdp.CatalogNotify, callID string) ([]manscdp.CatalogItem, bool) {
	if notify == nil {
		return nil, false
	}
	// 兼容没有 SumNum 的旧设备：按单包通知处理。
	if notify.SumNum <= 0 && len(notify.DeviceList.Items) > 0 {
		return notify.DeviceList.Items, true
	}
	if notify.SumNum == 0 {
		return nil, true
	}

	key := catalogNotifyKey{deviceID: notify.DeviceID, callID: strings.TrimSpace(callID), sn: notify.SN}
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.buckets[key]
	if b == nil {
		b = &catalogNotifyBucket{
			sumNum: notify.SumNum,
			items:  make(map[string]manscdp.CatalogItem),
		}
		p.buckets[key] = b
	}
	if notify.SumNum > b.sumNum {
		b.sumNum = notify.SumNum
	}
	for _, item := range notify.DeviceList.Items {
		if item.DeviceID == "" {
			continue
		}
		itemKey := item.DeviceID + "\x00" + strings.ToUpper(strings.TrimSpace(item.Event))
		if _, exists := b.items[itemKey]; !exists {
			b.order = append(b.order, itemKey)
		}
		b.items[itemKey] = item
	}
	if len(b.items) >= b.sumNum {
		items := make([]manscdp.CatalogItem, 0, len(b.order))
		for _, id := range b.order {
			items = append(items, b.items[id])
		}
		if b.timer != nil {
			b.timer.Stop()
		}
		delete(p.buckets, key)
		return items, true
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(30*time.Second, func() {
		p.mu.Lock()
		if current := p.buckets[key]; current == b {
			delete(p.buckets, key)
		}
		p.mu.Unlock()
	})
	return nil, false
}

func (p *CatalogProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.pipeline == nil || device == nil {
		return nil
	}
	notify, err := manscdp.ParseCatalogNotify(notification.Body)
	if err != nil {
		return err
	}
	items, complete := p.aggregate(notify, notification.CallID)
	if !complete {
		return nil
	}
	sender := catalog.Sender{SourceDeviceID: device.DeviceID, OwnerDeptID: device.OwnerDeptID}
	full := make([]catalog.CatalogItem, 0, len(items))
	for _, item := range items {
		if item.DeviceID == "" {
			continue
		}
		event := strings.ToUpper(strings.TrimSpace(item.Event))
		if event == "" {
			full = append(full, catalogItemFromMANSCDP(item))
			continue
		}
		if p.ignoreOfflineStatusNotify != nil && p.ignoreOfflineStatusNotify() && isNegativeChannelStatusEvent(event) {
			continue
		}
		if err := p.pipeline.IngestDelta(ctx, sender, event, catalogItemFromMANSCDP(item)); err != nil {
			return err
		}
	}
	return p.pipeline.Ingest(ctx, sender, full)
}

func isNegativeChannelStatusEvent(event string) bool {
	return event == "OFF" || event == "VLOST" || event == "DEFECT"
}

// catalogItemFromMANSCDP 把 manscdp DTO 转 catalog DTO。
//
// ⛔ 必须与 handler/catalog.go 的 manscdpToCatalogItem **保持完全一致**:
// 两者分别是「订阅 NOTIFY 路径」与「查询应答路径」的适配器,字段漏映射会让同一条目录项
// 走两条路径落出不同结果 —— 此前这里漏了 BusinessGroupID,导致订阅来的通道
// 在目录树里挂不到业务组织下(只有查询应答路径能挂)。
//
// ⛔ 通道属性都在 `<Info>` 容器内,经 InfoOrEmpty() 取。
// ⛔ 版本独有属性(PositionType/UseType vs PhotoelectricImagingType/CapturePositionType)
// 两版各自解析、并存落库,不做版本分支 —— 理由见 handler 侧同函数的注释。
func catalogItemFromMANSCDP(item manscdp.CatalogItem) catalog.CatalogItem {
	info := item.InfoOrEmpty()
	return catalog.CatalogItem{
		DeviceID: item.DeviceID, Name: item.Name, Manufacturer: item.Manufacturer,
		Model: item.Model, Owner: item.Owner, CivilCode: item.CivilCode,
		ParentID: item.ParentID, BusinessGroupID: item.BusinessGroupID,
		Parental: item.Parental, PTZType: item.PTZType, Longitude: item.Longitude,
		Latitude: item.Latitude, StatusOn: item.IsOnline(),
		Address: item.Address, Secrecy: int8(item.Secrecy), RegisterWay: int8(item.RegisterWay),

		IPAddress:       item.IPAddress,
		Port:            item.Port,
		RoomType:        manscdp.ParseAttrInt(info.RoomType),
		SupplyLightType: manscdp.ParseAttrInt(info.SupplyLightType),
		DirectionType:   manscdp.ParseAttrInt(info.DirectionType),
		Resolution:      info.Resolution,

		PositionType:             manscdp.ParseAttrInt(info.PositionType),
		UseType:                  manscdp.ParseAttrInt(info.UseType),
		PhotoelectricImagingType: info.PhotoelectricImagingType,
		CapturePositionType:      info.CapturePositionType,
		StreamNumberList:         info.StreamNumberList,
	}
}
