package gb28181

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

const cascadeTrackPublished = "34020000001320000012"
const cascadeTrackPanorama = "34020000001320000013"

func setupCascadeTargetTrack(t *testing.T, db *gorm.DB, platform model.GbCascadePlatform) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTargetTrack{}))
	require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022' WHERE id=10").Error)
	require.NoError(t, db.Table("gb_channel").Create(map[string]interface{}{"id": 21, "device_id": "34020000001320000001", "channel_id": "34020000001320000003", "status": 1, "owner_dept_id": 0}).Error)
	var parent model.GbCascadeDeviceProjection
	require.NoError(t, db.Where("platform_id=?", platform.ID).Take(&parent).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{PlatformID: platform.ID, DeviceProjectionID: parent.ID, SourceChannelID: 21, PublishedChannelID: cascadeTrackPanorama, ParentOverride: platform.LocalDeviceID, Active: true, PTZAllowed: false}).Error)
}

func cascadeTargetTrackBody(t *testing.T, mode manscdp.TargetTrackMode, panorama bool) []byte {
	t.Helper()
	command := manscdp.TargetTrackCommand{Mode: mode}
	if panorama {
		command.DeviceID2 = cascadeTrackPanorama
	}
	if mode == manscdp.TargetTrackManual {
		command.Area = &manscdp.TargetTrackArea{Length: 1920, Width: 1080, MidPointX: 0, MidPointY: 540, LengthX: 100, LengthY: 100}
	}
	body, err := manscdp.BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), cascadeTrackPublished, 97, command)
	require.NoError(t, err)
	return body
}

func TestCascadeTargetTrackAuthorizedEndToEnd(t *testing.T) {
	for _, mode := range []manscdp.TargetTrackMode{manscdp.TargetTrackAuto, manscdp.TargetTrackManual, manscdp.TargetTrackStop} {
		t.Run(string(mode), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 2)
			sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, _ string, body []byte) (uac.TrackedMessageResult, error) {
				require.Equal(t, "34020000001320000001", device)
				require.Equal(t, "192.0.2.30:5060", address)
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "track-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
			setupCascadeTargetTrack(t, db, platform)
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET maintenance_enabled=0,device_control_enabled=0 WHERE id=?", platform.ID).Error)
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))
			body := cascadeTargetTrackBody(t, mode, true)
			invoke := func(body []byte) int {
				req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
				tx := siptest.NewServerTxRecorder(req)
				defer tx.Terminate()
				require.True(t, handler(req, tx))
				return tx.Result()[0].StatusCode
			}
			require.Equal(t, 200, invoke(body))
			var sent []byte
			select {
			case sent = <-downstream:
			case <-time.After(time.Second):
				t.Fatal("target tracking was not forwarded")
			}
			wire, err := manscdp.ParseTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), sent)
			require.NoError(t, err)
			require.Equal(t, "34020000001320000002", wire.DeviceID)
			require.Equal(t, "34020000001320000003", wire.Command.DeviceID2)
			require.NotEqual(t, 97, wire.SN)
			require.Equal(t, mode, wire.Command.Mode)
			if mode == manscdp.TargetTrackManual {
				require.NotNil(t, wire.Command.Area)
				require.Zero(t, wire.Command.Area.MidPointX)
			}
			var op gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				return db.Where("action=?", "target_track").Take(&op).Error == nil && op.Status == gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			require.False(t, op.ResponseRequired)
			require.Equal(t, 1, op.MaxAttempts)
			require.NotNil(t, op.DeviceEpoch)
			require.NotNil(t, op.DeviceIntentID)
			var intent gbmodels.GbDeviceTargetTrack
			require.Eventually(t, func() bool { return db.Where("device_id=10").Take(&intent).Error == nil }, time.Second, 5*time.Millisecond)
			require.Equal(t, string(mode), string(intent.Mode))
			require.Equal(t, "34020000001320000003", intent.DeviceID2)
			require.Equal(t, 200, invoke(body))
			require.Empty(t, downstream)
			changed := strings.Replace(string(body), "<TargetTrack>"+string(mode)+"</TargetTrack>", "<TargetTrack>Auto</TargetTrack>", 1)
			if mode != manscdp.TargetTrackAuto {
				invoke([]byte(changed))
				require.Empty(t, downstream)
			}
		})
	}
}

func TestCascadeTargetTrackRejectsInvalidOrUnsharedTarget(t *testing.T) {
	for _, test := range []struct {
		name, statement string
		bodyChange      func(string) string
		status          int
	}{
		{name: "PTZ disabled", statement: "UPDATE gb_cascade_platform SET ptz_enabled=0", status: 403},
		{name: "channel PTZ disabled", statement: "UPDATE gb_cascade_channel_projection SET ptz_allowed=0 WHERE source_channel_id=20", status: 403},
		{name: "panorama hidden", statement: "UPDATE gb_cascade_channel_projection SET active=0 WHERE source_channel_id=21", status: 403},
		{name: "panorama deleted", statement: "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=21", status: 403},
		{name: "panorama owner mismatch", statement: "UPDATE gb_channel SET owner_dept_id=1 WHERE id=21", status: 403},
		{name: "source downgraded", statement: "UPDATE gb_device SET effective_version='2016'", status: 400},
		{name: "upstream downgraded", statement: "UPDATE gb_cascade_platform SET effective_version='2016'", status: 400},
		{name: "source epoch missing", statement: "UPDATE gb_device SET access_epoch=NULL", status: 403},
		{name: "mixed control", bodyChange: func(s string) string {
			return strings.Replace(s, "</Control>", "<PTZCmd>A50F01820003003A</PTZCmd></Control>", 1)
		}, status: 400},
		{name: "private panorama code", bodyChange: func(s string) string { return strings.Replace(s, cascadeTrackPanorama, "34020000001320000003", 1) }, status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 1)
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				downstream <- body
				return uac.TrackedMessageResult{StatusCode: 200}, nil
			})
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
			setupCascadeTargetTrack(t, db, platform)
			if test.statement != "" {
				require.NoError(t, db.Exec(test.statement).Error)
			}
			body := cascadeTargetTrackBody(t, manscdp.TargetTrackAuto, true)
			if test.bodyChange != nil {
				body = []byte(test.bodyChange(string(body)))
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))(req, tx))
			require.Equal(t, test.status, tx.Result()[0].StatusCode, fmt.Sprint(tx.Result()))
			require.Empty(t, downstream)
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestCascadeTargetTrackDispatchBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, mutation string
		status         int
		panorama, sent bool
	}{
		{name: "optional panorama", status: 200, sent: true},
		{name: "device rejects", status: 500},
		{name: "PTZ revoked after claim", mutation: "UPDATE gb_cascade_platform SET ptz_enabled=0"},
		{name: "projection deleted after claim", mutation: "UPDATE gb_cascade_channel_projection SET deleted_at=CURRENT_TIMESTAMP WHERE source_channel_id=20"},
		{name: "device projection deleted after claim", mutation: "UPDATE gb_cascade_device_projection SET deleted_at=CURRENT_TIMESTAMP"},
		{name: "primary ownership transferred after claim", mutation: "UPDATE gb_device SET owner_dept_id=7; UPDATE gb_channel SET owner_dept_id=7 WHERE id=20"},
		{name: "panorama deleted after claim", panorama: true, mutation: "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=21"},
		{name: "panorama ownership transferred after claim", panorama: true, mutation: "UPDATE gb_device SET owner_dept_id=7; UPDATE gb_channel SET owner_dept_id=7"},
		{name: "source downgraded after claim", mutation: "UPDATE gb_device SET effective_version='2016'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			calls := make(chan []byte, 2)
			sender := cascadeRecordSenderFunc(func(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
				calls <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{StatusCode: test.status, Attempted: true}, nil
			})
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
			setupCascadeTargetTrack(t, db, platform)
			if test.mutation != "" {
				require.NoError(t, db.Exec("CREATE TRIGGER revoke_track AFTER INSERT ON gb_ptz_operation_attempt BEGIN "+test.mutation+"; END").Error)
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, cascadeTargetTrackBody(t, manscdp.TargetTrackAuto, test.panorama))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			var op gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				return db.Where("action=?", "target_track").Take(&op).Error == nil && op.Status != gbmodels.PTZOperationQueued
			}, 2*time.Second, 5*time.Millisecond)
			var intents int64
			if test.sent {
				require.Equal(t, gbmodels.PTZOperationSent, op.Status)
				select {
				case body := <-calls:
					parsed, err := manscdp.ParseTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), body)
					require.NoError(t, err)
					require.Empty(t, parsed.Command.DeviceID2)
				default:
					t.Fatal("no downstream send")
				}
				require.Eventually(t, func() bool {
					return db.Model(&gbmodels.GbDeviceTargetTrack{}).Count(&intents).Error == nil && intents == 1
				}, time.Second, 5*time.Millisecond)
			} else {
				require.NotEqual(t, gbmodels.PTZOperationSent, op.Status)
				if test.mutation != "" {
					require.Empty(t, calls)
				}
				require.NoError(t, db.Model(&gbmodels.GbDeviceTargetTrack{}).Count(&intents).Error)
				require.Zero(t, intents, "rejected or unauthorized send must not update intent")
			}
		})
	}
}
