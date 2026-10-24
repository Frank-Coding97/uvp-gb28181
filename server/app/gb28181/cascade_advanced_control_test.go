package gb28181

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeAdvancedControlForwardsAndMapsBusinessResponse(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 1)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, _ string, body []byte) (uac.TrackedMessageResult, error) {
		if device != "34020000001320000001" || address != "192.0.2.30:5060" {
			return uac.TrackedMessageResult{}, fmt.Errorf("wrong target")
		}
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "advanced-down", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}))
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id=?", 10).Update("effective_version", "2022").Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET device_control_enabled=1,ptz_enabled=0 WHERE id=?", platform.ID).Error)
	scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	body := []byte(`<Control><CmdType>DeviceControl</CmdType><SN>91</SN><DeviceID>34020000001320000012</DeviceID><RecordCmd>Record</RecordCmd><StreamNumber>3</StreamNumber></Control>`)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	executionErrors := make(chan error, 1)
	execute := func(ctx context.Context, target cascadeConfigWriteTarget, command cascadeAdvancedControlCommand, authorization ptz.CascadeDeviceControlAuthorization, key string) (cascadeAdvancedControlResult, error) {
		result, err := executeCascadeAdvancedControl(ctx, target, command, authorization, key)
		if err != nil {
			executionErrors <- err
		}
		return result, err
	}
	handler, wait := newCascadeAdvancedControlHandlerWithExecutor(store, newCascadePlatformClientFactory(upstream, nil, time.Second), execute)
	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	var sent []byte
	select {
	case sent = <-downstream:
	case <-time.After(3 * time.Second):
		select {
		case err := <-executionErrors:
			t.Fatalf("no downstream command: %v", err)
		default:
			t.Fatal("no downstream command")
		}
	}
	var wire struct {
		SN                  int
		DeviceID, RecordCmd string
		StreamNumber        *int
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), sent, &wire))
	require.Equal(t, "34020000001320000002", wire.DeviceID)
	require.Equal(t, "Record", wire.RecordCmd)
	require.NotEqual(t, 91, wire.SN)
	require.NotNil(t, wire.StreamNumber)
	require.Equal(t, 3, *wire.StreamNumber)
	response := []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result></Response>`, wire.SN, wire.DeviceID))
	require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "advanced-down", "2", response))
	wait()
	var forwarded *sip.Request
	select {
	case forwarded = <-upstream.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("no upstream business result")
	}
	ack, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), forwarded.Body())
	require.NoError(t, err)
	require.Equal(t, 91, ack.SN)
	require.Equal(t, "34020000001320000012", ack.DeviceID)
	require.Equal(t, manscdp.DeviceControlResultOK, ack.Result)
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("sn=?", wire.SN).Take(&operation).Error)
	require.Equal(t, gbmodels.PTZOperationAccepted, operation.Status)
	var state gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id=? AND target_scope=? AND target_code=?", 10, gbmodels.ControlTargetScopeChannel, "34020000001320000002").Take(&state).Error)
	require.Equal(t, gbmodels.ControlStateOn, state.RecordState)
}

func TestCascadeAdvancedControlParsesGuardAndAlarmCommands(t *testing.T) {
	for _, test := range []struct {
		name, body, action, target string
	}{
		{"guard channel", `<Control><CmdType>DeviceControl</CmdType><SN>5</SN><DeviceID>34020000001320000012</DeviceID><GuardCmd>ResetGuard</GuardCmd></Control>`, "guard_reset", "34020000001320000002"},
		{"alarm fallback device", `<Control><CmdType>DeviceControl</CmdType><SN>6</SN><DeviceID>34020000001320000012</DeviceID><AlarmCmd>ResetAlarm</AlarmCmd><Info><AlarmMethod>2/5</AlarmMethod><AlarmType>1</AlarmType></Info></Control>`, "alarm_reset", "34020000001320000001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, err := parseCascadeAdvancedControl(protocol.ProfileFor(protocol.Version2022), []byte(test.body))
			require.NoError(t, err)
			require.Equal(t, test.action, command.Action)
			require.NoError(t, manscdp.ValidateAlarmResetOptions(command.Alarm))
			var wire []byte
			switch command.Action {
			case "guard_set":
				wire, err = manscdp.BuildGuardControlWithProfile(protocol.ProfileFor(protocol.Version2022), test.target, 12, manscdp.GuardSet)
			case "guard_reset":
				wire, err = manscdp.BuildGuardControlWithProfile(protocol.ProfileFor(protocol.Version2022), test.target, 12, manscdp.GuardReset)
			case "alarm_reset":
				wire, err = manscdp.BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2022), test.target, 12, command.Alarm)
			}
			require.NoError(t, err)
			require.Contains(t, string(wire), test.target)
			require.Contains(t, string(wire), map[string]string{"guard_set": "SetGuard", "guard_reset": "ResetGuard", "alarm_reset": "ResetAlarm"}[test.action])
		})
	}
}

func TestCascadeAdvancedControlRejects2022FieldsFor2016Upstream(t *testing.T) {
	for _, body := range []string{
		`<Control><CmdType>DeviceControl</CmdType><SN>7</SN><DeviceID>34020000001320000012</DeviceID><RecordCmd>Record</RecordCmd><StreamNumber>1</StreamNumber></Control>`,
		`<Control><CmdType>DeviceControl</CmdType><SN>8</SN><DeviceID>34020000001320000012</DeviceID><AlarmCmd>ResetAlarm</AlarmCmd><Info><AlarmMethod>2</AlarmMethod></Info></Control>`,
	} {
		_, err := parseCascadeAdvancedControl(protocol.ProfileFor(protocol.Version2016), []byte(body))
		require.Error(t, err)
	}
}

func TestCascadeAdvancedControlReportsUnsupportedDownstreamVersion(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		t.Error("unsupported stream selection must not reach the device")
		return uac.TrackedMessageResult{}, nil
	}))
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET device_control_enabled=1 WHERE id=?", platform.ID).Error)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	body := []byte(`<Control><CmdType>DeviceControl</CmdType><SN>39</SN><DeviceID>34020000001320000012</DeviceID><RecordCmd>Record</RecordCmd><StreamNumber>3</StreamNumber></Control>`)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	handler := newCascadeAdvancedControlHandler(store, newCascadePlatformClientFactory(upstream, nil, time.Second))
	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	var response *sip.Request
	select {
	case response = <-upstream.requests:
	case <-time.After(3 * time.Second):
		t.Fatal("no upstream error result")
	}
	ack, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), response.Body())
	require.NoError(t, err)
	require.Equal(t, 39, ack.SN)
	require.Equal(t, "34020000001320000012", ack.DeviceID)
	require.Equal(t, manscdp.DeviceControlResultError, ack.Result)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCascadeAdvancedControlGuardAndAlarmEndToEnd(t *testing.T) {
	for _, test := range []struct {
		name, body, expectedWire, result string
		upstreamSN                       int
	}{
		{"guard reset", `<Control><CmdType>DeviceControl</CmdType><SN>31</SN><DeviceID>34020000001320000012</DeviceID><GuardCmd>ResetGuard</GuardCmd></Control>`, "ResetGuard", "OK", 31},
		{"alarm reset rejected", `<Control><CmdType>DeviceControl</CmdType><SN>32</SN><DeviceID>34020000001320000012</DeviceID><AlarmCmd>ResetAlarm</AlarmCmd></Control>`, "ResetAlarm", "ERROR", 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 1)
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "advanced-guard-down", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}, &gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{}))
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET device_control_enabled=1,ptz_enabled=0 WHERE id=?", platform.ID).Error)
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(test.body))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			executionErrors := make(chan error, 1)
			execute := func(ctx context.Context, target cascadeConfigWriteTarget, command cascadeAdvancedControlCommand, authorization ptz.CascadeDeviceControlAuthorization, key string) (cascadeAdvancedControlResult, error) {
				result, err := executeCascadeAdvancedControl(ctx, target, command, authorization, key)
				if err != nil {
					executionErrors <- err
				}
				return result, err
			}
			handler, wait := newCascadeAdvancedControlHandlerWithExecutor(store, newCascadePlatformClientFactory(upstream, nil, time.Second), execute)
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode, tx.Result()[0].Reason)
			var sent []byte
			select {
			case sent = <-downstream:
			case <-time.After(3 * time.Second):
				var operation gbmodels.GbPTZOperation
				_ = db.Order("id DESC").Take(&operation).Error
				select {
				case err := <-executionErrors:
					t.Fatalf("no downstream command: %v", err)
				default:
					t.Fatalf("no downstream command: status=%s error=%s", operation.Status, operation.ErrorMessage)
				}
			}
			require.Contains(t, string(sent), test.expectedWire)
			require.Contains(t, string(sent), "34020000001320000001", "alarm target falls back to the registered device code when no alarm input is published")
			var wire struct {
				SN       int    `xml:"SN"`
				DeviceID string `xml:"DeviceID"`
			}
			require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), sent, &wire))
			response := []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>%s</Result></Response>`, wire.SN, wire.DeviceID, test.result))
			require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "advanced-guard-down", "2", response))
			wait()
			var forwarded *sip.Request
			select {
			case forwarded = <-upstream.requests:
			case <-time.After(2 * time.Second):
				t.Fatal("no upstream business result")
			}
			ack, err := manscdp.ParseDeviceControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), forwarded.Body())
			require.NoError(t, err)
			require.Equal(t, test.upstreamSN, ack.SN)
			require.Equal(t, "34020000001320000012", ack.DeviceID)
			require.Equal(t, manscdp.DeviceControlResult(test.result), ack.Result)
			var operation gbmodels.GbPTZOperation
			require.NoError(t, db.Where("sn=?", wire.SN).Take(&operation).Error)
			want := gbmodels.PTZOperationAccepted
			if test.result == "ERROR" {
				want = gbmodels.PTZOperationRejected
			}
			require.Equal(t, want, operation.Status)
			if test.result == "OK" {
				var state gbmodels.GbDeviceControlState
				require.NoError(t, db.Where("device_id=? AND target_scope=? AND target_code=?", 10, gbmodels.ControlTargetScopeAlarm, "34020000001320000001").Take(&state).Error)
				require.Equal(t, gbmodels.ControlStateOff, state.GuardState)
			}
		})
	}
}

func TestCascadeAdvancedControlRequiresIndependentGrant(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		t.Error("unexpected send")
		return uac.TrackedMessageResult{}, nil
	}))
	require.NoError(t, db.Model(&platform).Update("device_control_enabled", false).Error)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Control><CmdType>DeviceControl</CmdType><SN>4</SN><DeviceID>34020000001320000012</DeviceID><GuardCmd>SetGuard</GuardCmd></Control>`))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))
	require.True(t, handler(req, tx))
	require.Equal(t, 403, tx.Result()[0].StatusCode, tx.Result()[0].Reason)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCascadeAdvancedControlDoesNotRespondAfterAuthorizationRevocation(t *testing.T) {
	for _, test := range []struct {
		name       string
		statements []string
	}{
		{"platform permission", []string{"UPDATE gb_cascade_platform SET device_control_enabled=0 WHERE id=?"}},
		{"source device epoch", []string{"UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=1 WHERE id=10"}},
		{"shared projection", []string{
			"UPDATE gb_cascade_channel_projection SET active=0 WHERE platform_id=?",
			"UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				t.Error("unexpected device send")
				return uac.TrackedMessageResult{}, nil
			}))
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET device_control_enabled=1,ptz_enabled=0 WHERE id=?", platform.ID).Error)
			entered := make(chan cascadeConfigWriteTarget, 1)
			release := make(chan struct{})
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			ack := []byte(`<Response><CmdType>DeviceControl</CmdType><SN>11</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result></Response>`)
			execute := func(ctx context.Context, target cascadeConfigWriteTarget, _ cascadeAdvancedControlCommand, _ ptz.CascadeDeviceControlAuthorization, _ string) (cascadeAdvancedControlResult, error) {
				entered <- target
				select {
				case <-release:
					return cascadeAdvancedControlResult{body: ack, authorize: func(checkCtx context.Context) error {
						if !cascadeAdvancedSourceAuthorizationCurrent(checkCtx, target) {
							return fmt.Errorf("device source authorization changed")
						}
						return nil
					}}, nil
				case <-ctx.Done():
					return cascadeAdvancedControlResult{}, ctx.Err()
				}
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Control><CmdType>DeviceControl</CmdType><SN>10</SN><DeviceID>34020000001320000012</DeviceID><RecordCmd>Record</RecordCmd></Control>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			handler, wait := newCascadeAdvancedControlHandlerWithExecutor(store, newCascadePlatformClientFactory(upstream, nil, time.Second), execute)
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("advanced control did not enter pending execution")
			}
			for _, statement := range test.statements {
				require.NoError(t, db.Exec(statement, platform.ID).Error)
			}
			close(release)
			wait()
			select {
			case response := <-upstream.requests:
				t.Fatalf("unexpected upstream response after revocation: %s", response.Body())
			default:
			}
		})
	}
}
