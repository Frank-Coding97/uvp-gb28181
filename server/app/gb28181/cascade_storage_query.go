package gb28181

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeStorageQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
}

type cascadeStorageQueryFunc func(context.Context, cascadeDeviceStatusTarget, string) ([]byte, error)

func newCascadeStorageQueryHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return newCascadeStorageQueryHandlerWithQuery(store, clients, queryCascadeStorage)
}

func newCascadeStorageQueryHandlerWithQuery(store *repository.GormRepository, clients *cascadePlatformClientFactory, execute cascadeStorageQueryFunc) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var query cascadeStorageQuery
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &query) != nil || query.CmdType != manscdp.CmdSDCardStatus {
			return false
		}
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(503, "Storage query unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled {
			return reject(403, "Storage query upstream rejected")
		}
		if query.SN <= 0 || !validCascadeGBID(query.DeviceID) {
			return reject(400, "Invalid storage query")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, query.DeviceID, toID, req.Recipient.User) {
			return reject(403, "Storage query target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.Enabled || projection.Platform.ConfigRevision != platform.ConfigRevision || projection.Platform.ProjectionRevision != platform.ProjectionRevision {
			return reject(403, "Storage query authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "Storage query publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, query.DeviceID)
		if !ok || target.IsPlatform {
			return reject(403, "Storage query target is not published")
		}
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			return reject(503, "Storage query response unavailable")
		}
		if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
			return true
		}
		cseq := uint32(0)
		if req.CSeq() != nil {
			cseq = req.CSeq().SeqNo
		}
		key := strings.Replace(cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cseq, query.DeviceID), "cascade-device-status:", "cascade-storage-query:", 1)
		snapshot := projection.Platform
		profile := cascadeWireProfile(snapshot)
		go func() {
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer queryCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(queryCtx, store, snapshot) {
				return
			}
			source, queryErr := execute(queryCtx, target, key)
			var body []byte
			if queryErr == nil {
				status, err := manscdp.ParseSDCardStatusResponse(source)
				queryErr = err
				if queryErr == nil {
					body, queryErr = manscdp.BuildSDCardStatusResponseWithProfile(profile, query.DeviceID, query.SN, status)
				}
			}
			// This response is a complete list, not a paginated catalog. Never
			// truncate it to fit UDP and report a partial result as success.
			if queryErr == nil && strings.EqualFold(snapshot.Transport, "UDP") && !client.(*cascadePlatformClient).transactions.MessageFitsUDP(body) {
				queryErr = fmt.Errorf("complete storage response exceeds UDP budget")
			}
			if queryErr != nil {
				app.Log(queryCtx).Warn("级联存储卡查询失败", zap.String("event", "cascade.storage.query_failed"), zap.Uint64("platform_id", snapshot.ID), zap.String("published_target_id", query.DeviceID), zap.Error(queryErr))
				var err error
				body, err = manscdp.MarshalProfiledXML(profile, struct {
					XMLName  xml.Name `xml:"Response"`
					CmdType  string   `xml:"CmdType"`
					SN       int      `xml:"SN"`
					DeviceID string   `xml:"DeviceID"`
					Result   string   `xml:"Result"`
				}{CmdType: query.CmdType, SN: query.SN, DeviceID: query.DeviceID, Result: "ERROR"})
				if err != nil {
					return
				}
			}
			sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer sendCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(sendCtx, store, snapshot) {
				return
			}
			result := client.(*cascadePlatformClient).transactions.SendMessage(sendCtx, body, fmt.Sprintf("storage-query-%d-%d", snapshot.ID, time.Now().UnixNano()))
			if !result.Success {
				app.Log(sendCtx).Warn("级联存储卡应答发送失败", zap.String("event", "cascade.storage.response_failed"), zap.Uint64("platform_id", snapshot.ID), zap.Int("status", result.StatusCode))
			}
		}()
		return true
	}
}

func queryCascadeStorage(ctx context.Context, target cascadeDeviceStatusTarget, key string) ([]byte, error) {
	service, db := ptzService, app.DB()
	if service == nil || db == nil {
		return nil, fmt.Errorf("storage query runtime unavailable")
	}
	loader := control.NewGormTargetLoader(db)
	load := loader.Load
	if service.RequiresDeviceAuthorization() {
		load = loader.LoadAuthorized
	}
	loaded, err := load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil {
		return nil, err
	}
	body, err := service.StorageCardsForCascade(ctx, loaded, key)
	if err != nil {
		return nil, err
	}
	current, err := load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil || current.DeviceEpoch != loaded.DeviceEpoch || current.DeviceCode != loaded.DeviceCode || current.ChannelCode != loaded.ChannelCode {
		return nil, fmt.Errorf("storage query source authorization changed")
	}
	return body, nil
}
