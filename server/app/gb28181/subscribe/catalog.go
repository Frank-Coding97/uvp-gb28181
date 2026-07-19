package subscribe

import (
	"context"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// CatalogProcessor forwards subscription notifications through the established catalog pipeline.
type CatalogProcessor struct {
	pipeline *catalog.Pipeline
}

func NewCatalogProcessor(pipeline *catalog.Pipeline) *CatalogProcessor {
	return &CatalogProcessor{pipeline: pipeline}
}

func (p *CatalogProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.pipeline == nil || device == nil {
		return nil
	}
	notify, err := manscdp.ParseCatalogNotify(notification.Body)
	if err != nil {
		return err
	}
	items := make([]catalog.CatalogItem, 0, len(notify.DeviceList.Items))
	for _, item := range notify.DeviceList.Items {
		if item.DeviceID != "" {
			items = append(items, catalogItemFromMANSCDP(item))
		}
	}
	sender := catalog.Sender{SourceDeviceID: device.DeviceID, OwnerDeptID: device.OwnerDeptID}
	for i, item := range notify.DeviceList.Items {
		if item.Event == "" {
			continue
		}
		if err := p.pipeline.IngestDelta(ctx, sender, item.Event, catalogItemFromMANSCDP(item)); err != nil {
			return err
		}
		items[i] = catalog.CatalogItem{}
	}
	full := items[:0]
	for _, item := range items {
		if item.DeviceID != "" {
			full = append(full, item)
		}
	}
	return p.pipeline.Ingest(ctx, sender, full)
}

func catalogItemFromMANSCDP(item manscdp.CatalogItem) catalog.CatalogItem {
	return catalog.CatalogItem{
		DeviceID: item.DeviceID, Name: item.Name, Manufacturer: item.Manufacturer,
		Model: item.Model, Owner: item.Owner, CivilCode: item.CivilCode,
		ParentID: item.ParentID, PTZType: item.PTZType, Longitude: item.Longitude,
		Latitude: item.Latitude, StatusOn: item.IsOnline(),
	}
}
