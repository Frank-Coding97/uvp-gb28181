package gb28181

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	gbplayback "uvplatform.com/uvp-gb28181/app/gb28181/playback"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/recordquery"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeRecordQuery struct {
	XMLName    xml.Name                    `xml:"Query"`
	CmdType    string                      `xml:"CmdType"`
	SN         int                         `xml:"SN"`
	DeviceID   string                      `xml:"DeviceID"`
	StartTime  string                      `xml:"StartTime"`
	EndTime    string                      `xml:"EndTime"`
	Type       manscdp.RecordInfoQueryType `xml:"Type"`
	Secrecy    int                         `xml:"Secrecy"`
	RecorderID string                      `xml:"RecorderID"`
}

type cascadeRecordingTarget struct {
	device        gbmodels.GbDevice
	channel       gbmodels.GbChannel
	authorization gbplayback.AuthorizationSnapshot
}

func loadCascadeRecordingTarget(ctx context.Context, target catalog.PublishedChannelTarget) (cascadeRecordingTarget, error) {
	var result cascadeRecordingTarget
	if err := app.DB().WithContext(ctx).First(&result.channel, target.SourceChannelID).Error; err != nil {
		return result, err
	}
	lookup := app.DB().WithContext(ctx).Where("id = ? AND deleted_at IS NULL", target.SourceDeviceID).Limit(1).Find(&result.device)
	if lookup.Error != nil {
		return result, lookup.Error
	}
	if lookup.RowsAffected != 1 || result.channel.DeletedAt != nil || result.device.DeviceID != result.channel.DeviceID {
		return result, fmt.Errorf("shared recording source missing")
	}
	var epoch struct{ AccessEpoch, CleanupCompletedEpoch int64 }
	if err := app.DB().WithContext(ctx).Table("gb_device").Select("access_epoch, cleanup_completed_epoch").Where("id = ? AND deleted_at IS NULL", target.SourceDeviceID).Take(&epoch).Error; err != nil {
		return result, err
	}
	if epoch.AccessEpoch <= 0 || epoch.CleanupCompletedEpoch != epoch.AccessEpoch {
		return result, fmt.Errorf("shared recording source authorization not ready")
	}
	result.authorization = gbplayback.AuthorizationSnapshot{DevicePK: int64(target.SourceDeviceID), ChannelPK: int64(target.SourceChannelID),
		DeviceCode: result.device.DeviceID, ChannelCode: result.channel.ChannelID, DeviceEpoch: epoch.AccessEpoch, CleanupCompletedEpoch: epoch.CleanupCompletedEpoch}
	return result, nil
}

func cascadeRecordingProjection(ctx context.Context, store *repository.GormRepository, platform model.GbCascadePlatform, published string) (catalog.PublishedChannelTarget, error) {
	projection, err := store.ProjectionSnapshot(ctx, platform.ID)
	if err != nil {
		return catalog.PublishedChannelTarget{}, err
	}
	snapshot, err := catalog.Build(*projection, catalog.SourceFacts{})
	if err != nil {
		return catalog.PublishedChannelTarget{}, err
	}
	target, ok := snapshot.LookupPublishedChannel(published)
	if !ok {
		return catalog.PublishedChannelTarget{}, fmt.Errorf("recording channel is not shared")
	}
	return target, nil
}

func newCascadeRecordInfoHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		if req == nil {
			return false
		}
		var query cascadeRecordQuery
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &query) != nil || query.XMLName.Local != "Query" || query.CmdType != manscdp.CmdRecordInfo {
			return false
		}
		fail := func(code int) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, "Record query rejected", nil))
			return true
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return fail(500)
		}
		peer, ok := cascadePeerFromRequest(req)
		if !ok {
			return fail(400)
		}
		var platform *model.GbCascadePlatform
		for i := range platforms {
			if peer.match(platforms[i]) {
				if platform != nil {
					return fail(403)
				}
				platform = &platforms[i]
			}
		}
		if platform == nil || !platform.Enabled || !platform.RecordingEnabled {
			return fail(403)
		}
		profile := cascadeWireProfile(*platform)
		if manscdp.DecodeProfiledXML(profile, req.Body(), &query) != nil || query.SN <= 0 {
			return fail(400)
		}
		target, err := cascadeRecordingProjection(ctx, store, *platform, strings.TrimSpace(query.DeviceID))
		if err != nil {
			return fail(404)
		}
		if req.To() == nil || (req.To().Address.User != platform.LocalDeviceID && req.To().Address.User != query.DeviceID) ||
			(req.Recipient.User != platform.LocalDeviceID && req.Recipient.User != query.DeviceID) {
			return fail(403)
		}
		source, err := loadCascadeRecordingTarget(ctx, target)
		if err != nil {
			return fail(404)
		}
		if source.device.Status != gbmodels.DeviceStatusOnline {
			return fail(480)
		}
		config := gbconfig.Load().RecordQuery
		start, startErr := recordquery.ParseWallClock(query.StartTime, config.Location)
		end, endErr := recordquery.ParseWallClock(query.EndTime, config.Location)
		if startErr != nil || endErr != nil || !end.After(start) || end.Sub(start) > time.Duration(config.MaxRangeHours)*time.Hour ||
			(query.Type != "" && query.Type != manscdp.RecordInfoQueryTypeAll && query.Type != manscdp.RecordInfoQueryTypeManual && query.Type != manscdp.RecordInfoQueryTypeAlarm) ||
			(query.Secrecy != 0 && query.Secrecy != 1) || (query.RecorderID != "" && query.RecorderID != platform.LocalDeviceID) {
			return fail(400)
		}
		if recordQueryService == nil || clients == nil {
			return fail(503)
		}
		client, err := clients.NewClient(*platform)
		if err != nil {
			return fail(503)
		}
		outbound := client.(*cascadePlatformClient)
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}
		result, err := recordQueryService.QueryForCascade(ctx, recordquery.CascadeQueryRequest{ChannelID: uint(target.SourceChannelID), DeviceCode: source.device.DeviceID,
			ChannelCode: source.channel.ChannelID, Destination: net.JoinHostPort(source.device.IP, strconv.Itoa(source.device.Port)), Transport: source.device.Transport,
			ProtocolVersion: protocol.Version(source.device.EffectiveVersion), StartTime: start, EndTime: end, Type: query.Type, Secrecy: query.Secrecy})
		if err != nil || result.Incomplete {
			logCascadeRecordQueryFailed(ctx, platform.ID, cascadeCallID(req), errors.Join(err, fmt.Errorf("incomplete recording query")))
			return true
		}
		var fits func([]byte) bool
		if strings.EqualFold(platform.Transport, "UDP") {
			fits = outbound.transactions.MessageFitsUDP
		}
		bodies, err := planCascadeRecordResponses(profile, query, result.Records, platform.LocalDeviceID, platform.CatalogBatchSize, fits)
		if err != nil {
			logCascadeRecordQueryFailed(ctx, platform.ID, cascadeCallID(req), err)
			return true
		}
		for _, body := range bodies {
			latest, err := store.FindPlatform(ctx, platform.ID)
			if err != nil || !latest.Enabled || !latest.RecordingEnabled || latest.ConfigRevision != platform.ConfigRevision || latest.ProjectionRevision != platform.ProjectionRevision {
				return true
			}
			currentTarget, err := cascadeRecordingProjection(ctx, store, *latest, strings.TrimSpace(query.DeviceID))
			if err != nil || currentTarget != target {
				return true
			}
			current, err := loadCascadeRecordingTarget(ctx, currentTarget)
			if err != nil || current.authorization != source.authorization {
				return true
			}
			sent := outbound.transactions.SendMessage(ctx, body, fmt.Sprintf("records-%d-%d", platform.ID, time.Now().UnixNano()))
			if !sent.Success {
				logCascadeRecordQueryFailed(ctx, platform.ID, cascadeCallID(req), errors.Join(sent.TransportErr, sent.BuildErr))
				return true
			}
		}
		return true
	}
}

type cascadeRecordResponse struct {
	XMLName  xml.Name           `xml:"Response"`
	CmdType  string             `xml:"CmdType"`
	SN       int                `xml:"SN"`
	DeviceID string             `xml:"DeviceID"`
	Name     string             `xml:"Name"`
	SumNum   int                `xml:"SumNum"`
	List     *cascadeRecordList `xml:"RecordList,omitempty"`
}
type cascadeRecordList struct {
	Num   int                 `xml:"Num,attr"`
	Items []cascadeRecordItem `xml:"Item"`
}
type cascadeRecordItem struct {
	DeviceID   string `xml:"DeviceID"`
	Name       string `xml:"Name"`
	FilePath   string `xml:"FilePath,omitempty"`
	Address    string `xml:"Address,omitempty"`
	StartTime  string `xml:"StartTime"`
	EndTime    string `xml:"EndTime"`
	Secrecy    int    `xml:"Secrecy"`
	Type       string `xml:"Type"`
	RecorderID string `xml:"RecorderID"`
	FileSize   *int64 `xml:"FileSize,omitempty"`
}

func planCascadeRecordResponses(profile protocol.Profile, query cascadeRecordQuery, records []recordquery.Record, localID string, batch int, fits func([]byte) bool) ([][]byte, error) {
	if batch <= 0 {
		return nil, fmt.Errorf("invalid recording batch size")
	}
	marshal := func(items []recordquery.Record) ([]byte, error) {
		response := cascadeRecordResponse{CmdType: manscdp.CmdRecordInfo, SN: query.SN, DeviceID: query.DeviceID, SumNum: len(records)}
		if len(items) > 0 {
			response.Name = items[0].Name
			response.List = &cascadeRecordList{Num: len(items)}
			for _, item := range items {
				filePath := item.FilePath
				if uri, err := sdp.ParseHistoricalURI(filePath); err == nil && uri.FilePath != "" {
					uri.DeviceID = query.DeviceID
					filePath = uri.String()
				}
				response.List.Items = append(response.List.Items, cascadeRecordItem{DeviceID: query.DeviceID, Name: item.Name,
					FilePath: filePath, Address: item.Address, StartTime: item.StartTime, EndTime: item.EndTime, Secrecy: item.Secrecy, Type: string(item.Type), RecorderID: localID, FileSize: item.FileSize})
			}
		}
		return manscdp.MarshalProfiledXML(profile, response)
	}
	if len(records) == 0 {
		body, err := marshal(nil)
		return [][]byte{body}, err
	}
	var bodies [][]byte
	for start := 0; start < len(records); {
		end := min(start+batch, len(records))
		body, err := marshal(records[start:end])
		if err != nil {
			return nil, err
		}
		for fits != nil && !fits(body) {
			if end-start == 1 {
				return nil, fmt.Errorf("recording item exceeds UDP packet limit; use TCP")
			}
			end = start + (end-start)/2
			body, err = marshal(records[start:end])
			if err != nil {
				return nil, err
			}
		}
		bodies = append(bodies, body)
		start = end
	}
	return bodies, nil
}

func logCascadeRecordQueryFailed(ctx context.Context, platformID uint64, callID string, err error) {
	app.Log(ctx).Warn("级联录像查询失败", zap.String("event", "cascade.record.query_failed"), zap.Uint64("platform_id", platformID), zap.String("call_id", callID), zap.Error(err))
}
