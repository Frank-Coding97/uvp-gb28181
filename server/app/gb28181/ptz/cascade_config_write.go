package ptz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const actionCascadeConfigWrite = "cascade_apply_config"

// CascadeConfigAuthorization is captured with the durable operation, so queued
// writes cannot outlive the publication or its explicit configuration grant.
type CascadeConfigAuthorization struct {
	PlatformID         uint64 `json:"platformId"`
	ConfigRevision     uint64 `json:"configRevision"`
	ProjectionRevision uint64 `json:"projectionRevision"`
	PublishedID        string `json:"publishedId"`
}

type cascadeConfigWritePayload struct {
	Configuration string                     `json:"configuration"`
	Authorization CascadeConfigAuthorization `json:"authorization"`
}

func (s *Service) ConfigWriteForCascade(ctx context.Context, target Target, command *manscdp.CascadeConfigWrite, authorization CascadeConfigAuthorization, key string) ([]byte, error) {
	if !s.RequiresDeviceAuthorization() || target.DeviceEpoch <= 0 || command == nil {
		return nil, fmt.Errorf("authorized configuration service unavailable")
	}
	code, scope := target.ChannelCode, gbmodels.ControlTargetScopeChannel
	if command.Scoped(true) != nil {
		if command.Scoped(false) != nil {
			return nil, fmt.Errorf("device and channel configurations require separate operations")
		}
		code, scope = target.DeviceCode, gbmodels.ControlTargetScopeDevice
	}
	canonical, err := command.Build(protocol.ProfileFor(protocol.Version2022), code, 1)
	if err != nil {
		return nil, err
	}
	payload := cascadeConfigWritePayload{string(canonical), authorization}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	expected := gbmodels.GbPTZOperation{DeviceID: target.DeviceID, DeviceCode: target.DeviceCode, ChannelID: target.ChannelID, ChannelCode: target.ChannelCode, PayloadJSON: string(raw)}
	if err := s.validateCascadeConfigWriteAuthorization(ctx, expected); err != nil {
		return nil, err
	}
	identity := fmt.Sprintf("config-write:%x", sha256.Sum256(raw))
	pages, err := s.queryForCascade(ctx, target, identity, key, func() (gbmodels.GbPTZOperation, error) {
		return s.Execute(ctx, target, Command{
			CmdType: manscdp.CmdDeviceConfig, Action: actionCascadeConfigWrite, IdempotencyKey: key,
			Payload:          map[string]interface{}{"configuration": payload.Configuration, "authorization": authorization},
			ResponseRequired: true, MaxAttempts: 1, TargetScope: scope, TargetCode: code, Profile: target.Profile,
			Build: func(sn int) ([]byte, error) { return command.Build(target.Profile, code, sn) },
		})
	})
	if err != nil {
		return nil, err
	}
	if len(pages) != 1 {
		return nil, fmt.Errorf("configuration write requires one application result")
	}
	return pages[0], nil
}

func (s *Service) validateCascadeConfigWriteAuthorization(ctx context.Context, operation gbmodels.GbPTZOperation) error {

	if operation.Action == actionCascadeConfigWrite {
		epoch := int64(0)
		if operation.DeviceEpoch != nil {
			epoch = *operation.DeviceEpoch
		}
		key := cascadeQueryWaiterKey(operation.DeviceID, operation.ChannelID, operation.DeviceCode, operation.ChannelCode, epoch, operation.IdempotencyKey)
		s.cascadeQueryMu.Lock()
		waiter := s.cascadeQueries[key]
		pending := waiter != nil && waiter.refs > 0 && !waiter.complete && waiter.err == nil
		s.cascadeQueryMu.Unlock()
		if !pending {
			return fmt.Errorf("configuration request no longer pending")
		}
	}
	var payload cascadeConfigWritePayload
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return err
	}
	a := payload.Authorization
	if a.PlatformID == 0 || a.ConfigRevision == 0 || a.ProjectionRevision == 0 || a.PublishedID == "" {
		return fmt.Errorf("configuration publication authorization missing")
	}
	var count int64
	err := ptzWriter(s.db).WithContext(ctx).Table("gb_cascade_platform AS platform").
		Joins("JOIN gb_cascade_channel_projection AS channel ON channel.platform_id=platform.id").
		Joins("JOIN gb_cascade_device_projection AS device ON device.id=channel.device_projection_id AND device.platform_id=platform.id").
		Joins("JOIN gb_device AS source_device ON source_device.id=device.source_device_id").
		Joins("JOIN gb_channel AS source_channel ON source_channel.id=channel.source_channel_id AND source_channel.device_id=source_device.device_id AND source_channel.owner_dept_id=source_device.owner_dept_id").
		Where("platform.id=? AND platform.enabled=? AND platform.config_enabled=? AND platform.deleted_at IS NULL AND platform.config_revision=? AND platform.projection_revision=?", a.PlatformID, true, true, a.ConfigRevision, a.ProjectionRevision).
		Where("channel.active=? AND device.active=? AND channel.published_channel_id=? AND device.source_device_id=? AND channel.source_channel_id=?", true, true, a.PublishedID, operation.DeviceID, operation.ChannelID).
		Where("source_device.deleted_at IS NULL AND source_channel.deleted_at IS NULL AND source_device.device_id=? AND source_channel.channel_id=?", operation.DeviceCode, operation.ChannelCode).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("check configuration publication: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("configuration publication authorization revoked")
	}
	return nil
}

func buildCascadeConfigWriteOperation(operation gbmodels.GbPTZOperation) ([]byte, error) {
	var payload cascadeConfigWritePayload
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return nil, err
	}
	command, err := manscdp.ParseCascadeConfigWrite([]byte(payload.Configuration))
	if err != nil {
		return nil, err
	}
	return command.Build(operationProtocolProfile(operation), operationTargetCode(operation), operation.SN)
}

func (s *Service) applyCascadeConfigWriteResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	ack, err := manscdp.ParseCascadeConfigWriteResponse(body, manscdp.ConfigDownloadExpectation{SN: operation.SN, DeviceID: operationTargetCode(operation)})
	if err != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, err.Error())
	}
	if !ack.Accepted() {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorDeviceRejected, "设备拒绝级联配置")
	}
	applied := false
	err = schedulerTransaction(ctx, s.db, func(tx *gorm.DB) error {
		var err error
		applied, err = applyPTZResponseTransition(tx, operation, callID, cseq, ptzResponseTransition{Status: gbmodels.PTZOperationAccepted, DeviceResult: "OK"}, s.now())
		return err
	})
	if err == nil && applied {
		s.observeCascadeQuery(operation, body, true)
	}
	return err
}
