package subscribe

import (
	"context"
	"strings"

	"uvplatform.cn/uvp-gb28181/app/gb28181/catalog"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// CatalogProcessor forwards subscription notifications through the established catalog pipeline.
type CatalogProcessor struct {
	pipeline                  *catalog.Pipeline
	ignoreOfflineStatusNotify func() bool
}

func NewCatalogProcessor(pipeline *catalog.Pipeline) *CatalogProcessor {
	return &CatalogProcessor{
		pipeline:                  pipeline,
		ignoreOfflineStatusNotify: gbconfig.IgnoreChannelOfflineStatusNotify,
	}
}

func (p *CatalogProcessor) Process(ctx context.Context, device *gbmodels.GbDevice, notification Notification) error {
	if p == nil || p.pipeline == nil || device == nil {
		return nil
	}
	notify, err := manscdp.ParseCatalogNotify(notification.Body)
	if err != nil {
		return err
	}
	sender := catalog.Sender{SourceDeviceID: device.DeviceID, OwnerDeptID: device.OwnerDeptID}
	full := make([]catalog.CatalogItem, 0, len(notify.DeviceList.Items))
	for _, item := range notify.DeviceList.Items {
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

func catalogItemFromMANSCDP(item manscdp.CatalogItem) catalog.CatalogItem {
	return catalog.CatalogItem{
		DeviceID: item.DeviceID, Name: item.Name, Manufacturer: item.Manufacturer,
		Model: item.Model, Owner: item.Owner, CivilCode: item.CivilCode,
		ParentID: item.ParentID, PTZType: item.PTZType, Longitude: item.Longitude,
		Latitude: item.Latitude, StatusOn: item.IsOnline(),
	}
}
