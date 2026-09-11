package gb28181

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// cascadeCatalogPusher proactively reports the shared catalog to one upstream
// platform without waiting for its Catalog query. GB/T 28181 defines catalog
// delivery as a query/response exchange, so the push synthesizes our own SN
// and sends the same encoded response batches the responder would produce.
type cascadeCatalogPusher struct {
	store   *repository.GormRepository
	clients *cascadePlatformClientFactory
}

func newCascadeCatalogPusher(store *repository.GormRepository, clients *cascadePlatformClientFactory) *cascadeCatalogPusher {
	return &cascadeCatalogPusher{store: store, clients: clients}
}

func (p *cascadeCatalogPusher) PushCatalog(ctx context.Context, platformID uint64) (items, batches int, err error) {
	platforms, err := p.store.ListPlatforms(ctx)
	if err != nil {
		return 0, 0, err
	}
	var matched *model.GbCascadePlatform
	for i := range platforms {
		if platforms[i].ID == platformID {
			matched = &platforms[i]
			break
		}
	}
	if matched == nil {
		return 0, 0, repository.ErrPlatformNotFound
	}
	if !matched.Enabled {
		return 0, 0, fmt.Errorf("cascade platform %d is disabled", platformID)
	}
	snapshot, err := buildCascadeCatalogSnapshot(ctx, p.store, matched)
	if err != nil {
		return 0, 0, err
	}
	client, err := p.clients.NewClient(*matched)
	if err != nil {
		return 0, 0, err
	}
	outbound, ok := client.(*cascadePlatformClient)
	if !ok {
		return 0, 0, fmt.Errorf("cascade client does not support MESSAGE transport")
	}
	profile := protocol.ProfileFor(effectiveCascadeProfile(*matched))
	// 合成 SN:无入站查询可回带,取纳秒时间戳低位保证 1..1e9 内唯一即可。
	sn := int(time.Now().UnixNano() % 1_000_000_000)
	if sn < 1 {
		sn += 1_000_000_000
	}
	queryBody, err := manscdp.BuildCatalogQueryWithProfile(profile, matched.LocalDeviceID, sn)
	if err != nil {
		return 0, 0, err
	}
	var fits func([]byte) bool
	if strings.EqualFold(matched.Transport, "UDP") {
		fits = outbound.transactions.MessageFitsUDP
	}
	responses, err := catalog.PlanCatalogResponsesWithinLimit(profile, matched.LocalDeviceID, snapshot, matched.CatalogBatchSize, queryBody, fits)
	if err != nil {
		return 0, 0, err
	}
	sent := 0
	for _, response := range responses {
		result := outbound.transactions.SendMessage(ctx, response.Body, fmt.Sprintf("catalog-push-%d-%d", matched.ID, time.Now().UnixNano()))
		if !result.Success {
			if app.ZapLog != nil {
				app.ZapLog.Warn("级联目录手动推送失败", zap.Uint64("platformId", matched.ID), zap.Int("sent", sent), zap.Int("status", result.StatusCode), zap.Error(result.TransportErr), zap.Error(result.BuildErr))
			}
			return len(snapshot.Items), sent, fmt.Errorf("send catalog batch %d/%d failed", sent+1, len(responses))
		}
		sent++
	}
	if app.ZapLog != nil {
		app.ZapLog.Info("级联目录手动推送完成", zap.Uint64("platformId", matched.ID), zap.Int("items", len(snapshot.Items)), zap.Int("batches", sent))
	}
	return len(snapshot.Items), sent, nil
}
