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
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadePTZQuery struct {
	XMLName  xml.Name `xml:"Query"`
	CmdType  string   `xml:"CmdType"`
	SN       int      `xml:"SN"`
	DeviceID string   `xml:"DeviceID"`
	Number   *int     `xml:"Number"`
}

type cascadePTZQueryFunc func(context.Context, cascadeDeviceStatusTarget, ptz.QueryKind, int, string) ([][]byte, error)

func cascadePTZQueryKind(cmd string) (ptz.QueryKind, bool) {
	switch cmd {
	case manscdp.CmdPresetQuery:
		return ptz.QueryPreset, true
	case manscdp.CmdHomePositionQuery:
		return ptz.QueryHomePosition, true
	case manscdp.CmdCruiseTrackListQuery:
		return ptz.QueryCruiseTrackList, true
	case manscdp.CmdCruiseTrackQuery:
		return ptz.QueryCruiseTrack, true
	case manscdp.CmdPTZPosition, manscdp.CmdPTZPreciseStatusQuery:
		return ptz.QueryPreciseStatus, true
	}
	return "", false
}

func newCascadePTZQueryHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return newCascadePTZQueryHandlerWithQuery(store, clients, queryCascadePTZ)
}

func newCascadePTZQueryHandlerWithQuery(store *repository.GormRepository, clients *cascadePlatformClientFactory, execute cascadePTZQueryFunc) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var head struct {
			XMLName xml.Name
			CmdType string `xml:"CmdType"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &head) != nil || head.XMLName.Local != "Query" {
			return false
		}
		kind, ok := cascadePTZQueryKind(head.CmdType)
		if !ok {
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
			return reject(503, "PTZ query unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled || !platform.PTZEnabled {
			return reject(403, "PTZ query upstream rejected")
		}
		profile := cascadeWireProfile(*platform)
		var query cascadePTZQuery
		if manscdp.DecodeProfiledXML(profile, req.Body(), &query) != nil || query.SN <= 0 || !validCascadeGBID(query.DeviceID) {
			return reject(400, "Invalid PTZ query")
		}
		if head.CmdType != query.CmdType {
			return reject(400, "Invalid PTZ query command")
		}
		if (kind == ptz.QueryCruiseTrack && (query.Number == nil || *query.Number < 0 || *query.Number > 255)) || (kind != ptz.QueryCruiseTrack && query.Number != nil) {
			return reject(400, "Invalid PTZ query track")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, query.DeviceID, toID, req.Recipient.User) {
			return reject(403, "PTZ query target mismatch")
		}
		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.Enabled || !projection.Platform.PTZEnabled || projection.Platform.ConfigRevision != platform.ConfigRevision || projection.Platform.ProjectionRevision != platform.ProjectionRevision {
			return reject(403, "PTZ query authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "PTZ query publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, query.DeviceID)
		if !ok || target.IsPlatform {
			return reject(403, "PTZ query target is not published")
		}
		allowed := false
		for _, channel := range projection.Channels {
			if channel.SourceChannelID == target.SourceChannelID && channel.PublishedChannelID == query.DeviceID && channel.PTZAllowed {
				allowed = true
				break
			}
		}
		if !allowed {
			return reject(403, "PTZ query permission denied")
		}
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			return reject(503, "PTZ query response unavailable")
		}
		if tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)) != nil {
			return true
		}
		trackID := 0
		if query.Number != nil {
			trackID = *query.Number
		}
		cseq := uint32(0)
		if req.CSeq() != nil {
			cseq = req.CSeq().SeqNo
		}
		key := strings.Replace(cascadeDeviceStatusIdempotencyKey(platform.ID, cascadeCallID(req), cseq, query.DeviceID), "cascade-device-status:", "cascade-ptz-query:", 1)
		snapshot := projection.Platform
		go func() {
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer queryCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(queryCtx, store, snapshot) {
				return
			}
			pages, queryErr := execute(queryCtx, target, kind, trackID, key)
			var responses [][]byte
			if queryErr == nil {
				var fits func([]byte) bool
				if strings.EqualFold(snapshot.Transport, "UDP") {
					fits = client.(*cascadePlatformClient).transactions.MessageFitsUDP
				}
				responses, queryErr = planCascadePTZQueryResponses(profile, query, pages, fits)
			}
			if queryErr != nil || len(responses) == 0 {
				app.Log(queryCtx).Warn("级联云台查询失败", zap.String("event", "cascade.ptz.query_failed"), zap.Uint64("platform_id", snapshot.ID), zap.String("published_target_id", query.DeviceID), zap.Error(queryErr))
				body, err := manscdp.MarshalProfiledXML(profile, struct {
					XMLName  xml.Name `xml:"Response"`
					CmdType  string   `xml:"CmdType"`
					SN       int      `xml:"SN"`
					DeviceID string   `xml:"DeviceID"`
					Result   string   `xml:"Result"`
				}{CmdType: query.CmdType, SN: query.SN, DeviceID: query.DeviceID, Result: "ERROR"})
				if err != nil {
					return
				}
				responses = [][]byte{body}
			}
			for _, body := range responses {
				sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
				if !cascadeDeviceStatusAuthorizationCurrent(sendCtx, store, snapshot) {
					sendCancel()
					return
				}
				result := client.(*cascadePlatformClient).transactions.SendMessage(sendCtx, body, fmt.Sprintf("ptz-query-%d-%d", snapshot.ID, time.Now().UnixNano()))
				sendCancel()
				if !result.Success {
					return
				}
			}
		}()
		return true
	}
}

func queryCascadePTZ(ctx context.Context, target cascadeDeviceStatusTarget, kind ptz.QueryKind, trackID int, key string) ([][]byte, error) {
	service, db := ptzService, app.DB()
	if service == nil || db == nil {
		return nil, fmt.Errorf("PTZ query runtime unavailable")
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
	pages, err := service.QueryForCascade(ctx, loaded, kind, trackID, key)
	if err != nil {
		return nil, err
	}
	current, err := load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil || current.DeviceEpoch != loaded.DeviceEpoch || current.DeviceCode != loaded.DeviceCode || current.ChannelCode != loaded.ChannelCode {
		return nil, fmt.Errorf("PTZ query source authorization changed")
	}
	return pages, nil
}
