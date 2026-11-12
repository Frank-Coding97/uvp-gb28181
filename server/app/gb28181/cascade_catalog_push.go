package gb28181

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/global/app"
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
	profile := cascadeWireProfile(*matched)
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
	// ⛔ 目录被切成多包时留一条 warn：部分上级（EasyGBS 3.7.451 实测）对同一设备的
	// 连续 Catalog 做「**每包全量替换**」，只会留下最后一包，前面的通道全丢；
	// 而它们又从不 SUBSCRIBE 目录，丢了没有自愈机会。
	// 我方无法替对端决定累积语义，所以只能把「为什么只剩一条」讲清楚：
	// 传输方式改 TCP（无 UDP MTU 折半）+ 批次 ≥ 目录条数，就能一包发完。
	if len(responses) > 1 {
		app.Log(ctx).Warn("级联目录分包发送",
			zap.String("event", "cascade.catalog.push_split"),
			zap.Uint64("platform_id", matched.ID),
			zap.Int("items", len(snapshot.Items)),
			zap.Int("batches", len(responses)),
			zap.Int("batch_size", matched.CatalogBatchSize),
			zap.String("transport", matched.Transport),
			zap.String("hint", "部分上级按每包全量替换目录；建议 transport=TCP 且每批条数不小于目录总数"))
	}
	for _, response := range responses {
		current, checkErr := p.store.FindPlatform(ctx, matched.ID)
		if checkErr != nil {
			return len(snapshot.Items), sent, checkErr
		}
		if !current.Enabled || current.ConfigRevision != matched.ConfigRevision || current.ProjectionRevision != snapshot.Revision {
			return len(snapshot.Items), sent, fmt.Errorf("cascade catalog authorization changed during publication")
		}
		result := outbound.transactions.SendMessage(ctx, response.Body, fmt.Sprintf("catalog-push-%d-%d", matched.ID, time.Now().UnixNano()))
		if !result.Success {
			app.Log(ctx).Warn("级联目录手动推送失败",
				zap.String("event", "cascade.catalog.push_failed"),
				zap.Uint64("platform_id", matched.ID),
				zap.Int("sent", sent),
				zap.Int("status", result.StatusCode),
				// TransportErr 与 BuildErr 互斥，合成一个 error 字段（否则写出两个同名键）。
				zap.Error(errors.Join(result.TransportErr, result.BuildErr)))
			return len(snapshot.Items), sent, fmt.Errorf("send catalog batch %d/%d failed", sent+1, len(responses))
		}
		sent++
	}
	// 手动推送是 HTTP 管理接口触发的，ctx 里带着请求身份 —— 用 app.Log(ctx)
	// 才能和这次管理请求的访问日志串起来（之前用全局 logger 是串不上的）。
	app.Log(ctx).Info("级联目录手动推送完成",
		zap.String("event", "cascade.catalog.push_completed"),
		zap.Uint64("platform_id", matched.ID),
		zap.Int("items", len(snapshot.Items)),
		zap.Int("batches", sent))
	return len(snapshot.Items), sent, nil
}
