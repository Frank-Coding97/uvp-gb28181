package gb28181

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type cascadeDeviceStatusTarget struct {
	PublishedID     string
	IsPlatform      bool
	TargetScope     string
	SourceDeviceID  uint64
	SourceChannelID uint64
}

type cascadeDeviceStatusQueryFunc func(context.Context, cascadeDeviceStatusTarget, string) (catalog.DeviceStatusFact, error)

type cascadeDeviceStatusOperations interface {
	RequiresDeviceAuthorization() bool
	Execute(context.Context, ptz.Target, ptz.Command) (gbmodels.GbPTZOperation, error)
	GetOperation(context.Context, string) (gbmodels.GbPTZOperation, error)
}

func newCascadeDeviceStatusHandler(store *repository.GormRepository, clients *cascadePlatformClientFactory) func(*sip.Request, sip.ServerTransaction) bool {
	return newCascadeDeviceStatusHandlerWithQuery(store, clients, queryCascadeDeviceStatus)
}

func newCascadeDeviceStatusHandlerWithQuery(store *repository.GormRepository, clients *cascadePlatformClientFactory, queryStatus cascadeDeviceStatusQueryFunc) func(*sip.Request, sip.ServerTransaction) bool {
	return func(req *sip.Request, tx sip.ServerTransaction) bool {
		var head struct {
			XMLName xml.Name `xml:"Query"`
			CmdType string   `xml:"CmdType"`
		}
		if manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), req.Body(), &head) != nil || head.XMLName.Local != "Query" || head.CmdType != manscdp.CmdDeviceStatus {
			return false
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		reject := func(code int, reason string) bool {
			_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
			return true
		}
		platforms, err := store.ListPlatforms(ctx)
		if err != nil {
			return reject(500, "DeviceStatus query unavailable")
		}
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		if platform == nil && !ambiguous {
			return false
		}
		if ambiguous || platform == nil || !platform.Enabled {
			return reject(403, "DeviceStatus upstream rejected")
		}

		profile := cascadeWireProfile(*platform)
		query, err := catalog.ParseInfoStatusQuery(profile, manscdp.CmdDeviceStatus, req.Body())
		if err != nil {
			return reject(400, "Invalid DeviceStatus query")
		}
		toID := ""
		if req.To() != nil {
			toID = req.To().Address.User
		}
		if !cascadeDeviceStatusRequestTargetsMatch(platform.LocalDeviceID, query.DeviceID, toID, req.Recipient.User) {
			return reject(403, "DeviceStatus target mismatch")
		}

		projection, err := store.ProjectionSnapshot(ctx, platform.ID)
		if err != nil || projection == nil || !projection.Platform.Enabled ||
			projection.Platform.ConfigRevision != platform.ConfigRevision || projection.Platform.ProjectionRevision != platform.ProjectionRevision {
			return reject(403, "DeviceStatus authorization changed")
		}
		publication, err := buildCascadeCatalogSnapshot(ctx, store, &projection.Platform)
		if err != nil || publication.Revision != projection.Revision {
			return reject(503, "DeviceStatus publication unavailable")
		}
		target, ok := resolveCascadeDeviceStatusTarget(*projection, publication, query.DeviceID)
		if !ok {
			return reject(403, "DeviceStatus target is not published")
		}
		client, err := clients.NewClient(projection.Platform)
		if err != nil {
			return reject(503, "DeviceStatus response unavailable")
		}
		if err := tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)); err != nil {
			return true
		}

		callID, cseq := cascadeCallID(req), uint32(0)
		if req.CSeq() != nil {
			cseq = req.CSeq().SeqNo
		}
		idempotencyKey := cascadeDeviceStatusIdempotencyKey(projection.Platform.ID, callID, cseq, query.DeviceID)
		platformSnapshot := projection.Platform
		queryBody := append([]byte(nil), req.Body()...)
		go func() {
			queryCtx, queryCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer queryCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(queryCtx, store, platformSnapshot) {
				return
			}

			facts := catalog.DeviceStatusFacts{}
			if target.IsPlatform {
				facts[query.DeviceID] = catalog.DeviceStatusFact{Online: true, Working: true}
			} else {
				fact, queryErr := queryStatus(queryCtx, target, idempotencyKey)
				if queryErr != nil {
					app.Log(queryCtx).Warn("级联设备状态查询失败",
						zap.String("event", "cascade.device_status.query_failed"),
						zap.Uint64("platform_id", platformSnapshot.ID),
						zap.String("published_target_id", query.DeviceID),
						zap.Error(queryErr))
				} else {
					facts[query.DeviceID] = fact
				}
			}

			var response []byte
			var responseErr error
			if len(facts) == 0 {
				response, responseErr = cascadeDeviceStatusErrorResponse(profile, query)
			} else {
				planned, planErr := catalog.PlanDeviceStatusResponse(profile, platformSnapshot.LocalDeviceID, publication, facts, queryBody)
				if planErr != nil {
					response, responseErr = cascadeDeviceStatusErrorResponse(profile, query)
				} else {
					response = planned.Body
				}
			}
			if responseErr != nil || len(response) == 0 {
				return
			}

			sendCtx, sendCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer sendCancel()
			if !cascadeDeviceStatusAuthorizationCurrent(sendCtx, store, platformSnapshot) {
				return
			}
			result := client.(*cascadePlatformClient).transactions.SendMessage(sendCtx, response, fmt.Sprintf("device-status-%d-%d", platformSnapshot.ID, time.Now().UnixNano()))
			if !result.Success {
				app.Log(sendCtx).Warn("级联设备状态响应发送失败",
					zap.String("event", "cascade.device_status.response_failed"),
					zap.Uint64("platform_id", platformSnapshot.ID),
					zap.Int("status", result.StatusCode))
			}
		}()
		return true
	}
}

func cascadeDeviceStatusRequestTargetsMatch(platformID, queryID, toID, requestURIID string) bool {
	allowed := func(id string) bool { return id == platformID || id == queryID }
	return allowed(toID) && allowed(requestURIID)
}

func resolveCascadeDeviceStatusTarget(projection repository.ProjectionSnapshot, publication catalog.Snapshot, publishedID string) (cascadeDeviceStatusTarget, bool) {
	if publishedID == projection.Platform.LocalDeviceID {
		return cascadeDeviceStatusTarget{PublishedID: publishedID, IsPlatform: true}, true
	}
	kind := catalog.CatalogItemKind("")
	for _, item := range publication.Items {
		if item.ID == publishedID {
			kind = item.Kind
			break
		}
	}
	if kind != catalog.CatalogItemChannel {
		return cascadeDeviceStatusTarget{}, false
	}
	for _, channel := range projection.Channels {
		if channel.Active && channel.PlatformID == projection.Platform.ID && channel.PublishedChannelID == publishedID {
			sourceDeviceID := cascadeProjectionSourceDevice(projection, channel.DeviceProjectionID)
			if sourceDeviceID == 0 || channel.SourceChannelID == 0 {
				return cascadeDeviceStatusTarget{}, false
			}
			return cascadeDeviceStatusTarget{
				PublishedID: publishedID, TargetScope: gbmodels.ControlTargetScopeChannel,
				SourceDeviceID: sourceDeviceID, SourceChannelID: channel.SourceChannelID,
			}, true
		}
	}
	return cascadeDeviceStatusTarget{}, false
}

func cascadeProjectionSourceDevice(projection repository.ProjectionSnapshot, deviceProjectionID uint64) uint64 {
	for _, device := range projection.Devices {
		if device.ID == deviceProjectionID && device.PlatformID == projection.Platform.ID && device.Active {
			return device.SourceDeviceID
		}
	}
	return 0
}

func cascadeDeviceStatusAuthorizationCurrent(ctx context.Context, store *repository.GormRepository, snapshot model.GbCascadePlatform) bool {
	current, err := store.FindPlatform(ctx, snapshot.ID)
	return err == nil && current.Enabled &&
		current.ConfigRevision == snapshot.ConfigRevision && current.ProjectionRevision == snapshot.ProjectionRevision
}

func cascadeDeviceStatusErrorResponse(profile protocol.Profile, query catalog.InfoStatusQuery) ([]byte, error) {
	return manscdp.MarshalProfiledXML(profile, struct {
		XMLName  xml.Name `xml:"Response"`
		CmdType  string   `xml:"CmdType"`
		SN       int      `xml:"SN"`
		DeviceID string   `xml:"DeviceID"`
		Result   string   `xml:"Result"`
	}{CmdType: manscdp.CmdDeviceStatus, SN: query.SN, DeviceID: query.DeviceID, Result: "ERROR"})
}

func cascadeDeviceStatusIdempotencyKey(platformID uint64, callID string, cseq uint32, publishedID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%d|%s", platformID, callID, cseq, publishedID)))
	return "cascade-device-status:" + hex.EncodeToString(sum[:])
}

func queryCascadeDeviceStatus(ctx context.Context, target cascadeDeviceStatusTarget, idempotencyKey string) (catalog.DeviceStatusFact, error) {
	if ptzService == nil {
		return catalog.DeviceStatusFact{}, fmt.Errorf("PTZ service is unavailable")
	}
	return queryCascadeDeviceStatusWith(ctx, target, idempotencyKey, app.DB(), ptzService)
}

func queryCascadeDeviceStatusWith(ctx context.Context, target cascadeDeviceStatusTarget, idempotencyKey string, db *gorm.DB, service cascadeDeviceStatusOperations) (catalog.DeviceStatusFact, error) {
	if db == nil || service == nil {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus query service is unavailable")
	}
	loaded, err := control.NewGormTargetLoader(db).Load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil {
		return catalog.DeviceStatusFact{}, err
	}
	if service.RequiresDeviceAuthorization() {
		var epoch struct{ AccessEpoch, CleanupCompletedEpoch *int64 }
		err := db.WithContext(ctx).Clauses(dbresolver.Write).Table("gb_device").Select("gb_device.access_epoch, gb_device.cleanup_completed_epoch").
			Joins("JOIN gb_channel ON gb_channel.device_id = gb_device.device_id AND gb_channel.owner_dept_id = gb_device.owner_dept_id").
			Where("gb_device.id = ? AND gb_device.device_id = ? AND gb_channel.id = ? AND gb_channel.channel_id = ? AND gb_device.deleted_at IS NULL AND gb_channel.deleted_at IS NULL",
				loaded.DeviceID, loaded.DeviceCode, loaded.ChannelID, loaded.ChannelCode).Take(&epoch).Error
		if err != nil {
			return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus source authorization unavailable: %w", err)
		}
		if epoch.AccessEpoch == nil || epoch.CleanupCompletedEpoch == nil || *epoch.AccessEpoch <= 0 || *epoch.AccessEpoch != *epoch.CleanupCompletedEpoch {
			return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus source authorization not ready")
		}
		loaded.DeviceEpoch = *epoch.AccessEpoch
	}
	targetCode := loaded.DeviceCode
	if target.TargetScope == gbmodels.ControlTargetScopeChannel {
		targetCode = loaded.ChannelCode
	}
	command := ptz.Command{
		CmdType: manscdp.CmdDeviceStatus, Action: "cascade_device_status", IdempotencyKey: idempotencyKey,
		Payload:          map[string]interface{}{"publishedTargetID": target.PublishedID},
		ResponseRequired: true, MaxAttempts: 3, Profile: loaded.Profile,
		TargetScope: target.TargetScope, TargetCode: targetCode,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildDeviceStatusQueryWithProfile(loaded.Profile, targetCode, sn)
		},
	}
	fact, err := executeCascadeDeviceStatusQuery(ctx, db, service, loaded, command)
	if err != nil {
		return catalog.DeviceStatusFact{}, err
	}
	if loaded.DeviceEpoch > 0 {
		var count int64
		err := db.WithContext(ctx).Clauses(dbresolver.Write).Table("gb_device").
			Joins("JOIN gb_channel ON gb_channel.device_id = gb_device.device_id AND gb_channel.owner_dept_id = gb_device.owner_dept_id").
			Where("gb_device.id = ? AND gb_device.device_id = ? AND gb_channel.id = ? AND gb_channel.channel_id = ? AND gb_device.deleted_at IS NULL AND gb_channel.deleted_at IS NULL AND gb_device.access_epoch = ? AND gb_device.cleanup_completed_epoch = ?",
				loaded.DeviceID, loaded.DeviceCode, loaded.ChannelID, loaded.ChannelCode, loaded.DeviceEpoch, loaded.DeviceEpoch).Count(&count).Error
		if err != nil || count != 1 {
			return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus source authorization changed")
		}
	}
	return fact, nil
}

func executeCascadeDeviceStatusQuery(ctx context.Context, db *gorm.DB, service cascadeDeviceStatusOperations, target ptz.Target, command ptz.Command) (catalog.DeviceStatusFact, error) {
	if db == nil || service == nil {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus query service is unavailable")
	}
	operation, err := service.Execute(ctx, target, command)
	if err != nil {
		return catalog.DeviceStatusFact{}, err
	}
	if operation.OperationID == "" {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus operation identity is missing")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		operation, err = service.GetOperation(ctx, operation.OperationID)
		if err != nil {
			return catalog.DeviceStatusFact{}, err
		}
		switch operation.Status {
		case gbmodels.PTZOperationAccepted:
			if operation.DeviceResult != "OK" {
				return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus device response was not OK")
			}
			return cascadeDeviceStatusFactForOperation(ctx, db, operation)
		case gbmodels.PTZOperationRejected, gbmodels.PTZOperationTimeout, gbmodels.PTZOperationCancelled, gbmodels.PTZOperationUnknown:
			return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus operation ended with %s", operation.Status)
		}
		select {
		case <-ctx.Done():
			return catalog.DeviceStatusFact{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func cascadeDeviceStatusFactForOperation(ctx context.Context, db *gorm.DB, operation gbmodels.GbPTZOperation) (catalog.DeviceStatusFact, error) {
	if operation.ID == 0 || operation.OperationID == "" || operation.SN <= 0 || operation.DeviceID == 0 || operation.TargetCode == "" {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus operation identity is incomplete")
	}
	var state gbmodels.GbDeviceControlState
	result := db.WithContext(ctx).Clauses(dbresolver.Write).Where(
		"device_id = ? AND target_scope = ? AND target_code = ? AND source_operation_id = ? AND source_operation_seq = ? AND source_sn = ?",
		operation.DeviceID, operation.TargetScope, operation.TargetCode, operation.OperationID, operation.ID, operation.SN,
	).Limit(1).Find(&state)
	if result.Error != nil {
		return catalog.DeviceStatusFact{}, result.Error
	}
	if result.RowsAffected != 1 || state.Freshness != gbmodels.ControlStateFresh || state.Source != "device_status" {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus facts for this operation are unavailable")
	}
	if state.OnlineState == nil || state.SelfTestState == nil {
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus required facts are missing")
	}
	fact := catalog.DeviceStatusFact{}
	switch strings.ToLower(strings.TrimSpace(*state.OnlineState)) {
	case gbmodels.DeviceOnlineStateOnline:
		fact.Online = true
	case gbmodels.DeviceOnlineStateOffline:
		fact.Online = false
	default:
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus online fact is unknown")
	}
	switch strings.ToLower(strings.TrimSpace(*state.SelfTestState)) {
	case gbmodels.DeviceSelfTestOK:
		fact.Working = true
	case gbmodels.DeviceSelfTestError:
		fact.Working = false
	default:
		return catalog.DeviceStatusFact{}, fmt.Errorf("DeviceStatus self-test fact is unknown")
	}
	fact.Encode = cascadeDeviceStatusBoolFact(state.EncodeState)
	if state.RecordState == gbmodels.ControlStateOn || state.RecordState == gbmodels.ControlStateOff {
		value := state.RecordState == gbmodels.ControlStateOn
		fact.Record = &value
	}
	if state.DeviceTime != nil {
		fact.DeviceTime = state.DeviceTime.Format("2006-01-02T15:04:05")
	}
	return fact, nil
}

func cascadeDeviceStatusBoolFact(value *string) *bool {
	if value == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(*value)) {
	case gbmodels.ControlStateOn:
		result := true
		return &result
	case gbmodels.ControlStateOff:
		result := false
		return &result
	default:
		return nil
	}
}
