package ptz

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func newPTZHandlerTestService(t *testing.T, capabilities *string) (*Service, *gorm.DB, *fakeTrackedSender, *time.Time) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ptz-handler.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbPTZHomePosition{}, &gbmodels.GbPTZState{},
		&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{},
		&gbmodels.GbDeviceControlState{},
		&gbmodels.GbChannel{},
	))
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 1, DeviceID: "D", ChannelID: "C", Capabilities: capabilities}).Error)
	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	sender := &fakeTrackedSender{}
	service, err := NewService(db, sender, func() time.Time { return now })
	require.NoError(t, err)
	return service, db, sender, &now
}

func createHandlerHomeControl(t *testing.T, service *Service, key string, enabled bool, resetTime, presetID *int) gbmodels.GbPTZOperation {
	t.Helper()
	operation, err := service.Execute(context.Background(), testTarget(), Command{
		CmdType: manscdp.CmdDeviceControl, Action: "home_position", IdempotencyKey: key,
		Payload:          map[string]interface{}{"enabled": enabled, "resetTime": resetTime, "presetId": presetID},
		ResponseRequired: true, MaxAttempts: 1, ActorID: 17, ActorDeptID: 23,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildHomePositionControl("C", sn, manscdp.HomePositionControl{Enabled: enabled, ResetTime: resetTime, PresetIndex: presetID})
		},
	})
	require.NoError(t, err)
	return operation
}

func createHandlerHomeQuery(t *testing.T, service *Service, key string, trigger *string) gbmodels.GbPTZOperation {
	t.Helper()
	command := Command{
		CmdType: manscdp.CmdHomePositionQuery, Action: "refresh_home_position", IdempotencyKey: key,
		Payload: map[string]interface{}{}, ResponseRequired: true, MaxAttempts: 3,
		Build: func(sn int) ([]byte, error) { return manscdp.BuildHomePositionQuery("C", sn) },
	}
	if trigger != nil {
		command.TriggerOperationID = *trigger
		command.Payload = map[string]interface{}{"triggerOperationId": *trigger}
	}
	operation, err := service.Execute(context.Background(), testTarget(), command)
	require.NoError(t, err)
	return operation
}

func deviceControlResponse(sn int, result string) []byte {
	return deviceControlResponseWithHead(manscdp.CmdDeviceControl, sn, "C", result)
}

func TestHandlerPreciseDeviceControl2022Response(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	target := testTarget()
	target.Profile = protocol.ProfileFor(protocol.Version2022)
	pan, tilt, zoom := 12.5, -3.25, 4.0
	operation, err := service.Execute(context.Background(), target, Command{
		CmdType: manscdp.CmdDeviceControl, Action: "precise", IdempotencyKey: "precise-2022-handler",
		Profile:          protocol.ProfileFor(protocol.Version2022),
		Payload:          map[string]interface{}{"pan": pan, "tilt": tilt, "zoom": zoom},
		ResponseRequired: true, MaxAttempts: 1,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildPTZPreciseDeviceControlWithProfile(protocol.ProfileFor(protocol.Version2022), "C", sn, manscdp.PTZPreciseControl{
				Pan: &pan, Tilt: &tilt, Zoom: &zoom,
			})
		},
	})
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
	body := deviceControlResponse(operation.SN, "OK")
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "precise-2022-response", "1", body))
	stored := storedOperation(t, db, operation.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
}

func TestHandlerDeviceControlResponseDoesNotAcceptOneWayOperation(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	operation, err := service.Execute(context.Background(), testTarget(), Command{
		CmdType: manscdp.CmdDeviceControl, Action: "iframe", IdempotencyKey: "one-way-iframe",
		Payload: map[string]interface{}{"action": "iframe"}, ResponseRequired: false,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildIFrameControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", sn)
		},
	})
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, operation.Status)

	body := deviceControlResponse(operation.SN, "OK")
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "unexpected-response", "1", body))

	stored := storedOperation(t, db, operation.OperationID)
	require.Equal(t, gbmodels.PTZOperationSent, stored.Status)
	require.Empty(t, stored.DeviceResult)
}

func TestHandlerDeviceControlAckPersistsConfirmedFacts(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	target := testTarget()

	record, err := service.Execute(context.Background(), target, Command{
		CmdType: manscdp.CmdDeviceControl, Action: "record_start", IdempotencyKey: "record-ack-fact",
		ResponseRequired: true, MaxAttempts: 1,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildRecordControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", sn, manscdp.RecordStart)
		},
	})
	require.NoError(t, err)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "record-ack", "1", deviceControlResponse(record.SN, "OK")))
	var recordFact gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", target.DeviceID, gbmodels.ControlTargetScopeChannel, target.ChannelCode).First(&recordFact).Error)
	require.Equal(t, gbmodels.ControlStateOn, recordFact.RecordState)
	require.Equal(t, "control_ack", recordFact.Source)
	require.Equal(t, record.OperationID, *recordFact.SourceOperationID)
	require.Equal(t, record.ID, recordFact.SourceOperationSeq)

	guard, err := service.Execute(context.Background(), target, Command{
		CmdType: manscdp.CmdDeviceControl, Action: "guard_set", IdempotencyKey: "guard-ack-fact",
		TargetScope: gbmodels.ControlTargetScopeAlarm, TargetCode: "A",
		ResponseRequired: true, MaxAttempts: 1,
		Build: func(sn int) ([]byte, error) {
			return manscdp.BuildGuardControlWithProfile(protocol.ProfileFor(protocol.Version2016), "A", sn, manscdp.GuardSet)
		},
	})
	require.NoError(t, err)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "guard-ack", "2", deviceControlResponseWithHead(manscdp.CmdDeviceControl, guard.SN, "A", "OK")))
	var guardFact gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", target.DeviceID, gbmodels.ControlTargetScopeAlarm, "A").First(&guardFact).Error)
	require.Equal(t, gbmodels.ControlStateOn, guardFact.GuardState)
	require.Equal(t, "control_ack", guardFact.Source)
	require.Equal(t, guard.ID, guardFact.SourceOperationSeq)
}

func homePositionResponse(sn int, home string) []byte {
	return homePositionResponseWithHead(manscdp.CmdHomePositionQuery, sn, "C", home)
}

func deviceControlResponseWithHead(cmdType string, sn int, deviceID, result string) []byte {
	return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>%s</Result></Response>`, cmdType, sn, deviceID, result))
}

func homePositionResponseWithHead(cmdType string, sn int, deviceID, home string) []byte {
	return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>%s</DeviceID>%s</Response>`, cmdType, sn, deviceID, home))
}

func createHandlerAttempt(t *testing.T, db *gorm.DB, operation gbmodels.GbPTZOperation, now time.Time, callID, cseq string) {
	t.Helper()
	leaseUntil := now.Add(15 * time.Second)
	require.NoError(t, db.Model(&operation).Updates(map[string]interface{}{
		"attempt": 1, "dispatch_started_at": now, "transport_deadline_at": leaseUntil,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperationAttempt{
		OperationID: operation.ID, AttemptNo: 1, SN: operation.SN,
		Status: gbmodels.PTZOperationAttemptSent, CallID: callID, CSeq: cseq,
		StartedAt: now, LeaseUntil: leaseUntil, CreatedAt: now,
	}).Error)
}

func storedOperation(t *testing.T, db *gorm.DB, operationID string) gbmodels.GbPTZOperation {
	t.Helper()
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operationID).First(&operation).Error)
	return operation
}

func TestHandlerHomePositionUniqueAssociationSeparatesInboundMetadata(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	operation := createHandlerHomeQuery(t, service, "unique-query", nil)
	deadline := now.Add(15 * time.Second)
	require.NoError(t, db.Model(&operation).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationSent, "call_id": "outbound-call", "cseq": "1", "sip_status": 202,
		"sent_at": *now, "deadline_at": deadline,
	}).Error)

	body := homePositionResponse(operation.SN, `<HomePosition><Enabled>0</Enabled><ResetTime>0</ResetTime><PresetIndex>0</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "inbound-call", "9", body))
	stored := storedOperation(t, db, operation.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.Equal(t, "outbound-call", stored.CallID)
	require.Equal(t, "1", stored.CSeq)
	require.Equal(t, 202, stored.SIPStatus)
	require.NotNil(t, stored.ResponseCallID)
	require.Equal(t, "inbound-call", *stored.ResponseCallID)
	require.NotNil(t, stored.ResponseCSeq)
	require.Equal(t, "9", *stored.ResponseCSeq)
	require.NotNil(t, stored.ResponseHasData)
	require.True(t, *stored.ResponseHasData)

	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.False(t, home.Enabled)
	require.NotNil(t, home.ResetTime)
	require.Zero(t, *home.ResetTime)
	require.NotNil(t, home.PresetID)
	require.Zero(t, *home.PresetID)
	confirmedAt := home.ConfirmedAt
	*now = now.Add(time.Second)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "duplicate-call", "10", body))
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.Equal(t, confirmedAt, home.ConfirmedAt, "duplicate response must not renew freshness")
}

func TestHandlerHomePositionAttemptMetadataWinsOverAmbiguousProtocolKey(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	target := createHandlerHomeQuery(t, service, "attempt-target", nil)
	transportDeadline := now.Add(15 * time.Second)
	require.NoError(t, db.Model(&target).Updates(map[string]interface{}{
		"attempt": 1, "dispatch_started_at": *now, "transport_deadline_at": transportDeadline,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperationAttempt{
		OperationID: target.ID, AttemptNo: 1, SN: target.SN,
		Status: gbmodels.PTZOperationAttemptSent, CallID: "reused-call", CSeq: "7",
		StartedAt: *now, LeaseUntil: transportDeadline, CreatedAt: *now,
	}).Error)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": "same-protocol-key", "idempotency_key": "same-protocol-key",
		"device_id": 2, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": manscdp.CmdHomePositionQuery, "action": "refresh_home_position", "sn": target.SN,
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true, "max_attempts": 3,
		"queue_deadline_at": transportDeadline, "created_at": *now,
	}).Error)

	body := homePositionResponse(target.SN, `<HomePosition><Enabled>1</Enabled><ResetTime>20</ResetTime><PresetIndex>0</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "reused-call", "7", body))
	require.Equal(t, gbmodels.PTZOperationAccepted, storedOperation(t, db, target.OperationID).Status)
	require.Equal(t, gbmodels.PTZOperationQueued, storedOperation(t, db, "same-protocol-key").Status)
	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.Equal(t, target.ID, home.SourceOperationSeq)
}

func TestHandlerHomePositionProtocolFallbackOnlyMatchesActiveOperation(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	terminal := createHandlerHomeQuery(t, service, "terminal-fallback", nil)
	active := createHandlerHomeQuery(t, service, "active-fallback", nil)
	require.NoError(t, db.Model(&terminal).Updates(map[string]interface{}{
		"sn": 4242, "status": gbmodels.PTZOperationAccepted,
	}).Error)
	require.NoError(t, db.Model(&active).Update("sn", 4242).Error)
	head, err := manscdp.ParseHead(homePositionResponse(4242, ""))
	require.NoError(t, err)

	matched, found, candidates, reason, err := service.findPTZMessageOperation(context.Background(), "D", "", "", *head)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, active.OperationID, matched.OperationID)
	require.Equal(t, []string{active.OperationID}, candidates)
	require.Equal(t, "protocol_key", reason)
}

func TestHandlerHomePositionProtocolFallbackDoesNotMatchTerminalOperation(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	terminal := createHandlerHomeQuery(t, service, "terminal-only-fallback", nil)
	require.NoError(t, db.Model(&terminal).Update("status", gbmodels.PTZOperationAccepted).Error)
	head, err := manscdp.ParseHead(homePositionResponse(terminal.SN, ""))
	require.NoError(t, err)

	_, found, candidates, reason, err := service.findPTZMessageOperation(context.Background(), "D", "", "", *head)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, candidates)
	require.Equal(t, "no_candidate", reason)
}

func TestHandlerQueryProtocolFallbackOnlyMatchesNonTerminalOperation(t *testing.T) {
	tests := []struct {
		name    string
		kind    QueryKind
		profile protocol.Profile
		body    func(sn int) []byte
	}{
		{
			name: "preset",
			kind: QueryPreset,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdPresetQuery, sn))
			},
		},
		{
			name: "cruise list",
			kind: QueryCruiseTrackList,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdCruiseTrackListQuery, sn))
			},
		},
		{
			name: "cruise detail",
			kind: QueryCruiseTrack,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdCruiseTrackQuery, sn))
			},
		},
		{
			name:    "ptz position",
			kind:    QueryPreciseStatus,
			profile: protocol.ProfileFor(protocol.Version2022),
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdPTZPosition, sn))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, db, _, _ := newPTZHandlerTestService(t, nil)
			target := testTarget()
			target.Profile = tt.profile
			terminal, err := service.Refresh(context.Background(), target, tt.kind, 0, "fallback-terminal-"+tt.name)
			require.NoError(t, err)
			active, err := service.Refresh(context.Background(), target, tt.kind, 0, "fallback-active-"+tt.name)
			require.NoError(t, err)
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", terminal.ID).Updates(map[string]interface{}{
				"sn": 777, "status": gbmodels.PTZOperationAccepted,
			}).Error)
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", active.ID).Update("sn", 777).Error)

			head, err := manscdp.ParseHead(tt.body(777))
			require.NoError(t, err)
			matched, found, candidates, reason, err := service.findPTZMessageOperation(context.Background(), "D", "", "", *head)
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, active.OperationID, matched.OperationID)
			require.Equal(t, []string{active.OperationID}, candidates)
			require.Equal(t, "protocol_key", reason)
		})
	}
}

func TestHandlerQueryAttemptCorrelationSkipsTerminalOperation(t *testing.T) {
	tests := []struct {
		name    string
		kind    QueryKind
		profile protocol.Profile
		body    func(sn int) []byte
	}{
		{
			name: "preset",
			kind: QueryPreset,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdPresetQuery, sn))
			},
		},
		{
			name: "cruise list",
			kind: QueryCruiseTrackList,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdCruiseTrackListQuery, sn))
			},
		},
		{
			name: "cruise detail",
			kind: QueryCruiseTrack,
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdCruiseTrackQuery, sn))
			},
		},
		{
			name:    "ptz position",
			kind:    QueryPreciseStatus,
			profile: protocol.ProfileFor(protocol.Version2022),
			body: func(sn int) []byte {
				return []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%d</SN><DeviceID>C</DeviceID></Response>`, manscdp.CmdPTZPosition, sn))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, db, _, now := newPTZHandlerTestService(t, nil)
			target := testTarget()
			target.Profile = tt.profile
			operation, err := service.Refresh(context.Background(), target, tt.kind, 0, "attempt-terminal-"+tt.name)
			require.NoError(t, err)
			createHandlerAttempt(t, db, operation, *now, "late-attempt", "42")
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operation.ID).
				Update("status", gbmodels.PTZOperationTimeout).Error)

			head, err := manscdp.ParseHead(tt.body(operation.SN))
			require.NoError(t, err)
			_, found, candidates, reason, err := service.findPTZMessageOperation(context.Background(), "D", "late-attempt", "42", *head)
			require.NoError(t, err)
			require.False(t, found)
			require.Empty(t, candidates)
			require.Equal(t, "attempt_terminal", reason)
		})
	}
}

func TestHandlerAttemptCorrelationDoesNotFallbackAfterTerminalMatch(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	terminal, err := service.Refresh(context.Background(), testTarget(), QueryPreset, 0, "authoritative-terminal")
	require.NoError(t, err)
	active, err := service.Refresh(context.Background(), testTarget(), QueryPreset, 0, "authoritative-active")
	require.NoError(t, err)
	createHandlerAttempt(t, db, terminal, *now, "terminal-call", "42")
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", terminal.ID).
		Updates(map[string]interface{}{"sn": 777, "status": gbmodels.PTZOperationTimeout}).Error)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", active.ID).Update("sn", 777).Error)
	head, err := manscdp.ParseHead([]byte(`<Response><CmdType>PresetQuery</CmdType><SN>777</SN><DeviceID>C</DeviceID></Response>`))
	require.NoError(t, err)

	_, found, candidates, reason, err := service.findPTZMessageOperation(context.Background(), "D", "terminal-call", "42", *head)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, candidates)
	require.Equal(t, "attempt_terminal", reason)
}

func TestHandlerAttemptMatchedHomeControlRejectsMismatchedBodyMetadata(t *testing.T) {
	tests := []struct {
		name string
		body func(gbmodels.GbPTZOperation) []byte
	}{
		{name: "SN", body: func(operation gbmodels.GbPTZOperation) []byte {
			return deviceControlResponseWithHead(manscdp.CmdDeviceControl, operation.SN+1, "C", "OK")
		}},
		{name: "DeviceID", body: func(operation gbmodels.GbPTZOperation) []byte {
			return deviceControlResponseWithHead(manscdp.CmdDeviceControl, operation.SN, "OTHER", "OK")
		}},
		{name: "CmdType", body: func(operation gbmodels.GbPTZOperation) []byte {
			return deviceControlResponseWithHead(manscdp.CmdHomePositionQuery, operation.SN, "C", "OK")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, db, _, now := newPTZHandlerTestService(t, nil)
			control := createHandlerHomeControl(t, service, "attempt-control-"+test.name, true, intValue(30), intValue(4))
			createHandlerAttempt(t, db, control, *now, "control-attempt", "11")
			seed := homePositionUpdate(0, *now)
			seed.SourceOperationID = "seed-control"
			_, applied, err := service.ApplyHomePosition(context.Background(), seed)
			require.NoError(t, err)
			require.True(t, applied)

			require.NoError(t, service.OnPTZMessage(context.Background(), "D", "control-attempt", "11", test.body(control)))
			stored := storedOperation(t, db, control.OperationID)
			require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
			require.Equal(t, ptzErrorProtocolInvalid, stored.ErrorCode)
			require.NotNil(t, stored.ResponseCallID)
			require.Equal(t, "control-attempt", *stored.ResponseCallID)
			var home gbmodels.GbPTZHomePosition
			require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
			require.NotNil(t, home.SourceOperationID)
			require.Equal(t, "seed-control", *home.SourceOperationID)
			var children int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("trigger_operation_id = ?", control.OperationID).Count(&children).Error)
			require.Zero(t, children)
		})
	}
}

func TestHandlerAttemptMatchedHomeQueryRejectsMismatchedBodyMetadata(t *testing.T) {
	home := `<HomePosition><Enabled>1</Enabled><ResetTime>30</ResetTime><PresetIndex>4</PresetIndex></HomePosition>`
	tests := []struct {
		name string
		body func(gbmodels.GbPTZOperation) []byte
	}{
		{name: "SN", body: func(operation gbmodels.GbPTZOperation) []byte {
			return homePositionResponseWithHead(manscdp.CmdHomePositionQuery, operation.SN+1, "C", home)
		}},
		{name: "DeviceID", body: func(operation gbmodels.GbPTZOperation) []byte {
			return homePositionResponseWithHead(manscdp.CmdHomePositionQuery, operation.SN, "OTHER", home)
		}},
		{name: "CmdType", body: func(operation gbmodels.GbPTZOperation) []byte {
			return homePositionResponseWithHead(manscdp.CmdDeviceControl, operation.SN, "C", home)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, db, _, now := newPTZHandlerTestService(t, nil)
			query := createHandlerHomeQuery(t, service, "attempt-query-"+test.name, nil)
			createHandlerAttempt(t, db, query, *now, "query-attempt", "12")
			seed := homePositionUpdate(0, *now)
			seed.SourceOperationID = "seed-query"
			_, applied, err := service.ApplyHomePosition(context.Background(), seed)
			require.NoError(t, err)
			require.True(t, applied)

			require.NoError(t, service.OnPTZMessage(context.Background(), "D", "query-attempt", "12", test.body(query)))
			stored := storedOperation(t, db, query.OperationID)
			require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
			require.Equal(t, ptzErrorProtocolInvalid, stored.ErrorCode)
			require.NotNil(t, stored.ResponseCallID)
			require.Equal(t, "query-attempt", *stored.ResponseCallID)
			var cached gbmodels.GbPTZHomePosition
			require.NoError(t, db.Where("channel_id = ?", 1).First(&cached).Error)
			require.NotNil(t, cached.SourceOperationID)
			require.Equal(t, "seed-query", *cached.SourceOperationID)
		})
	}
}

func TestHandlerHomePositionOrphanAndAmbiguousResponsesDoNotWrite(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	orphan := homePositionResponse(99, `<HomePosition><Enabled>1</Enabled></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "orphan", "1", orphan))

	deadline := now.Add(5 * time.Second)
	for index := 1; index <= 2; index++ {
		require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
			"operation_id": fmt.Sprintf("ambiguous-%d", index), "idempotency_key": fmt.Sprintf("ambiguous-%d", index),
			"device_id": 2, "device_code": "D", "channel_id": 1, "channel_code": "C",
			"cmd_type": manscdp.CmdHomePositionQuery, "action": "refresh_home_position", "sn": 77,
			"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true, "max_attempts": 3,
			"queue_deadline_at": deadline, "created_at": *now,
		}).Error)
	}
	ambiguous := homePositionResponse(77, `<HomePosition><Enabled>1</Enabled></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "ambiguous", "2", ambiguous))

	var changed int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("operation_id LIKE ? AND status <> ?", "ambiguous-%", gbmodels.PTZOperationQueued).Count(&changed).Error)
	require.Zero(t, changed)
	var homeCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZHomePosition{}).Count(&homeCount).Error)
	require.Zero(t, homeCount)
}

func TestHandlerHomeControlOKWritesCacheAndLinkedReconcile(t *testing.T) {
	service, db, sender, _ := newPTZHandlerTestService(t, nil)
	resetTime, presetID := 30, 0
	control := createHandlerHomeControl(t, service, "control-ok", true, &resetTime, &presetID)
	// The reconcile operation must inherit the immutable profile/target snapshot
	// used by the parent operation, including the 2022 scheduler path.
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("operation_id = ?", control.OperationID).Updates(map[string]interface{}{
		"profile_version": "2022", "profile_charset": "GB18030",
		"target_scope": gbmodels.ControlTargetScopeChannel, "target_code": "C",
	}).Error)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "control-response", "8", deviceControlResponse(control.SN, "OK")))

	storedControl := storedOperation(t, db, control.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, storedControl.Status)
	require.NotNil(t, storedControl.ReconcileOperationID)
	require.Equal(t, uint(17), storedControl.ActorID)
	require.Equal(t, uint(23), storedControl.ActorDeptID)

	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.True(t, home.Enabled)
	require.Equal(t, 30, *home.ResetTime)
	require.Zero(t, *home.PresetID)
	require.Equal(t, gbmodels.PTZHomePositionSourceControlACK, home.Source)
	require.Equal(t, gbmodels.PTZHomePositionVerificationUnverified, home.Verification)
	require.Equal(t, control.ID, home.SourceOperationSeq)

	child := storedOperation(t, db, *storedControl.ReconcileOperationID)
	require.Equal(t, gbmodels.PTZOperationQueued, child.Status)
	require.True(t, child.ResponseRequired)
	require.Zero(t, child.Attempt)
	require.Equal(t, 3, child.MaxAttempts)
	require.NotNil(t, child.TriggerOperationID)
	require.Equal(t, control.OperationID, *child.TriggerOperationID)
	require.Equal(t, control.ActorID, child.ActorID)
	require.Equal(t, control.ActorDeptID, child.ActorDeptID)
	require.Equal(t, "2022", child.ProfileVersion)
	require.Equal(t, "GB18030", child.ProfileCharset)
	require.Equal(t, gbmodels.ControlTargetScopeChannel, child.TargetScope)
	require.Equal(t, "C", child.TargetCode)
	require.Equal(t, "home-reconcile:"+control.OperationID, child.IdempotencyKey)
	require.Contains(t, child.PayloadJSON, control.OperationID)
	require.Zero(t, sender.calls, "ACK transaction must only queue reconcile")

	confirmedAt := home.ConfirmedAt
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "duplicate-response", "9", deviceControlResponse(control.SN, "OK")))
	var childCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("trigger_operation_id = ?", control.OperationID).Count(&childCount).Error)
	require.EqualValues(t, 1, childCount)
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.Equal(t, confirmedAt, home.ConfirmedAt)
}

func TestHandlerHomeControlExplicitUnsupportedSkipsAutomaticReconcile(t *testing.T) {
	profile := `{"home_position_query":false}`
	service, db, _, _ := newPTZHandlerTestService(t, &profile)
	control := createHandlerHomeControl(t, service, "control-unsupported", false, nil, nil)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "1", deviceControlResponse(control.SN, "OK")))

	stored := storedOperation(t, db, control.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.Nil(t, stored.ReconcileOperationID)
	var children int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("trigger_operation_id = ?", control.OperationID).Count(&children).Error)
	require.Zero(t, children)
}

func TestHandlerHomeControlTransactionRollsBackWhenReconcileCreateFails(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	control := createHandlerHomeControl(t, service, "control-rollback", false, nil, nil)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": "conflicting-reconcile", "idempotency_key": "home-reconcile:" + control.OperationID,
		"device_id": 2, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": manscdp.CmdHomePositionQuery, "action": "refresh_home_position", "sn": control.SN + 100,
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true, "max_attempts": 3,
		"queue_deadline_at": now.Add(5 * time.Second), "created_at": *now,
	}).Error)

	err := service.OnPTZMessage(context.Background(), "D", "response", "1", deviceControlResponse(control.SN, "OK"))
	require.Error(t, err)
	stored := storedOperation(t, db, control.OperationID)
	require.Equal(t, gbmodels.PTZOperationQueued, stored.Status)
	require.Nil(t, stored.ReconcileOperationID)
	var homeCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZHomePosition{}).Count(&homeCount).Error)
	require.Zero(t, homeCount)
}

func TestHandlerHomeControlErrorAndMalformedAreStableRejections(t *testing.T) {
	tests := []struct {
		name      string
		result    string
		errorCode string
	}{
		{name: "device error", result: "ERROR", errorCode: "DEVICE_REJECTED"},
		{name: "substring is invalid", result: "NOT OK", errorCode: "PROTOCOL_INVALID_RESPONSE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, db, _, _ := newPTZHandlerTestService(t, nil)
			control := createHandlerHomeControl(t, service, "reject", true, intValue(30), intValue(4))
			require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "1", deviceControlResponse(control.SN, test.result)))
			stored := storedOperation(t, db, control.OperationID)
			require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
			require.Equal(t, test.errorCode, stored.ErrorCode)
			var homeCount, childCount int64
			require.NoError(t, db.Model(&gbmodels.GbPTZHomePosition{}).Count(&homeCount).Error)
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("trigger_operation_id = ?", control.OperationID).Count(&childCount).Error)
			require.Zero(t, homeCount)
			require.Zero(t, childCount)
		})
	}
}

func TestHandlerHomeQueryNoDataPreservesCacheAndMarksStale(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	seed := homePositionUpdate(0, *now)
	seed.SourceOperationID = "seed"
	_, applied, err := service.ApplyHomePosition(context.Background(), seed)
	require.NoError(t, err)
	require.True(t, applied)
	query := createHandlerHomeQuery(t, service, "no-data", nil)

	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "2", homePositionResponse(query.SN, "")))
	stored := storedOperation(t, db, query.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.NotNil(t, stored.ResponseHasData)
	require.False(t, *stored.ResponseHasData)
	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.Zero(t, home.SourceOperationSeq)
	require.Equal(t, *now, home.ConfirmedAt)
	model, err := service.GetHomePositionReadModel(context.Background(), 1, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZFreshnessStale, model.Freshness)
	require.Equal(t, HomePositionRefreshSucceededNoData, model.Refresh.Status)
}

func TestHandlerMalformedHomeQueryRejectsWithoutChangingCache(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	query := createHandlerHomeQuery(t, service, "malformed-query", nil)
	body := homePositionResponse(query.SN, `<HomePosition><Enabled>2</Enabled><PresetIndex>0</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "2", body))

	stored := storedOperation(t, db, query.OperationID)
	require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
	require.Equal(t, ptzErrorProtocolInvalid, stored.ErrorCode)
	require.NotNil(t, stored.ResponseCallID)
	require.Equal(t, "response", *stored.ResponseCallID)
	var homeCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZHomePosition{}).Count(&homeCount).Error)
	require.Zero(t, homeCount)
}

func TestHandlerOlderHomeQueryCannotOverwriteNewerControlCache(t *testing.T) {
	service, db, _, now := newPTZHandlerTestService(t, nil)
	query := createHandlerHomeQuery(t, service, "old-query", nil)
	newer := homePositionUpdate(query.ID+1, now.Add(time.Second))
	newer.Source = gbmodels.PTZHomePositionSourceControlACK
	newer.Verification = gbmodels.PTZHomePositionVerificationUnverified
	newer.Enabled = true
	newer.SourceOperationID = "new-control"
	_, applied, err := service.ApplyHomePosition(context.Background(), newer)
	require.NoError(t, err)
	require.True(t, applied)

	body := homePositionResponse(query.SN, `<HomePosition><Enabled>0</Enabled><ResetTime>10</ResetTime><PresetIndex>0</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "late", "3", body))
	stored := storedOperation(t, db, query.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.True(t, home.Enabled)
	require.Equal(t, query.ID+1, home.SourceOperationSeq)
	require.Equal(t, newer.ConfirmedAt, home.ConfirmedAt)
}

func TestHandlerHomeReconcileMismatchKeepsAcceptedAndDeviceValue(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	resetTime, requestedPreset := 30, 4
	control := createHandlerHomeControl(t, service, "mismatch-control", true, &resetTime, &requestedPreset)
	query := createHandlerHomeQuery(t, service, "mismatch-query", &control.OperationID)
	require.NoError(t, db.Model(&control).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationAccepted, "completed_at": time.Now(), "reconcile_operation_id": query.OperationID,
	}).Error)

	body := homePositionResponse(query.SN, `<HomePosition><Enabled>1</Enabled><ResetTime>30</ResetTime><PresetIndex>5</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "4", body))
	stored := storedOperation(t, db, query.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.Equal(t, "HOME_POSITION_RECONCILE_MISMATCH", stored.ErrorCode)
	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.True(t, home.Enabled)
	require.Equal(t, 5, *home.PresetID)
	require.Equal(t, gbmodels.PTZHomePositionVerificationVerified, home.Verification)
}

func TestHomePositionReconcileMismatchRequiresExactBidirectionalRelationship(t *testing.T) {
	tests := []struct {
		name          string
		mutateQuery   func(*gbmodels.GbPTZOperation)
		parentUpdates map[string]interface{}
	}{
		{name: "query command", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.CmdType = manscdp.CmdPresetQuery }},
		{name: "query action", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.Action = "refresh_presets" }},
		{name: "channel id", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.ChannelID++ }},
		{name: "channel code", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.ChannelCode = "OTHER" }},
		{name: "device id", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.DeviceID++ }},
		{name: "device code", mutateQuery: func(query *gbmodels.GbPTZOperation) { query.DeviceCode = "OTHER" }},
		{name: "parent command", parentUpdates: map[string]interface{}{"cmd_type": manscdp.CmdPresetQuery}},
		{name: "parent action", parentUpdates: map[string]interface{}{"action": "preset_set"}},
		{name: "parent not accepted", parentUpdates: map[string]interface{}{"status": gbmodels.PTZOperationSent}},
		{name: "missing reverse relationship", parentUpdates: map[string]interface{}{"reconcile_operation_id": nil}},
		{name: "wrong reverse relationship", parentUpdates: map[string]interface{}{"reconcile_operation_id": "another-query"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, db, _, _ := newPTZHandlerTestService(t, nil)
			resetTime, presetID := 30, 4
			control := createHandlerHomeControl(t, service, "relationship-control", true, &resetTime, &presetID)
			query := createHandlerHomeQuery(t, service, "relationship-query", &control.OperationID)
			require.NoError(t, db.Model(&control).Updates(map[string]interface{}{
				"status": gbmodels.PTZOperationAccepted, "reconcile_operation_id": query.OperationID,
			}).Error)
			if test.mutateQuery != nil {
				test.mutateQuery(&query)
			}
			if test.parentUpdates != nil {
				require.NoError(t, db.Model(&control).Updates(test.parentUpdates).Error)
			}
			actualPreset := 5
			mismatch, err := homePositionReconcileMismatch(db, query, &manscdp.HomePositionConfig{
				Enabled: true, ResetTime: &resetTime, PresetIndex: &actualPreset,
			})
			require.NoError(t, err)
			require.False(t, mismatch)
		})
	}
}

func TestHandlerUnrelatedMalformedParentDoesNotRollbackHomeQuery(t *testing.T) {
	service, db, _, _ := newPTZHandlerTestService(t, nil)
	resetTime, presetID := 30, 4
	control := createHandlerHomeControl(t, service, "malformed-parent", true, &resetTime, &presetID)
	query := createHandlerHomeQuery(t, service, "query-with-unrelated-parent", &control.OperationID)
	require.NoError(t, db.Model(&control).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationAccepted, "payload_json": "{",
	}).Error)

	body := homePositionResponse(query.SN, `<HomePosition><Enabled>1</Enabled><ResetTime>30</ResetTime><PresetIndex>5</PresetIndex></HomePosition>`)
	require.NoError(t, service.OnPTZMessage(context.Background(), "D", "response", "15", body))
	stored := storedOperation(t, db, query.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
	require.Empty(t, stored.ErrorCode)
	var home gbmodels.GbPTZHomePosition
	require.NoError(t, db.Where("channel_id = ?", 1).First(&home).Error)
	require.Equal(t, 5, *home.PresetID)
	require.NotNil(t, home.SourceOperationID)
	require.Equal(t, query.OperationID, *home.SourceOperationID)
}
