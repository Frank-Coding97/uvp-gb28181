package ptz

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const actionCascadeConfigRead = "refresh_cascade_config"

// ConfigForCascade performs an authorized, durable read for one A.2.4.7 type.
// The publication layer combines these reads under the upstream SN. Keeping a
// type per operation also correlates an untyped empty device response correctly.
func (s *Service) ConfigForCascade(ctx context.Context, target Target, configType, key string) ([]byte, error) {
	types, err := manscdp.NormalizeCascadeConfigTypes([]string{configType})
	if err != nil {
		return nil, err
	}
	configType = types[0]
	profile := target.Profile
	if profile.Version == "" {
		profile = protocol.ProfileFor(protocol.Version2016)
	}
	code, scope := target.ChannelCode, gbmodels.ControlTargetScopeChannel
	if configType == manscdp.ConfigTypeBasicParam {
		code, scope = target.DeviceCode, gbmodels.ControlTargetScopeDevice
	}
	pages, err := s.queryForCascade(ctx, target, "config:"+configType, key, func() (gbmodels.GbPTZOperation, error) {
		return s.Execute(ctx, target, Command{
			CmdType: manscdp.CmdConfigDownload, Action: actionCascadeConfigRead, IdempotencyKey: key,
			Payload: map[string]interface{}{"configTypes": types}, ResponseRequired: true, MaxAttempts: 3,
			TargetCode: code, TargetScope: scope, Profile: profile,
			Build: func(sn int) ([]byte, error) {
				return manscdp.BuildConfigDownloadQueryWithProfile(profile, code, sn, types)
			},
		})
	})
	if err != nil {
		return nil, err
	}
	if len(pages) != 1 {
		return nil, fmt.Errorf("configuration type requires one complete response")
	}
	return pages[0], nil
}

func (s *Service) applyCascadeConfigResponse(ctx context.Context, operation gbmodels.GbPTZOperation, callID, cseq string, body []byte) error {
	var payload struct {
		ConfigTypes []string `json:"configTypes"`
	}
	if err := json.Unmarshal([]byte(operation.PayloadJSON), &payload); err != nil {
		return err
	}
	if len(payload.ConfigTypes) != 1 {
		return fmt.Errorf("cascade configuration operation requires one type")
	}
	response, err := manscdp.ParseCascadeConfigReadResponse(body, manscdp.ConfigDownloadExpectation{SN: operation.SN, DeviceID: operationTargetCode(operation)}, payload.ConfigTypes[0])
	if err != nil {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorProtocolInvalid, err.Error())
	}
	if !response.Accepted() {
		return s.applyRejectedPTZResponse(ctx, operation, callID, cseq, "ERROR", ptzErrorDeviceRejected, "设备拒绝配置读取")
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
