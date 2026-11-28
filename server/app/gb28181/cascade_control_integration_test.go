package gb28181

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func cascadeAuthorizedControlFixture(t *testing.T, version string, sender ptz.TrackedSender, barriers ...**playauth.DeviceOperationBarrier) (*gorm.DB, model.GbCascadePlatform, *repository.GormRepository, *ptz.Service) {
	t.Helper()
	db := authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "cascade-control.sqlite"))
	_, platform, store := cascadeDeviceStatusFixture(t, version, db)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &playauth.DeviceOperationIntent{}, &model.GbCascadeSnapshotSession{}))
	for _, statement := range []string{
		"ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1",
		"ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1",
		"ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL",
		"UPDATE gb_device SET device_id='34020000001320000001' WHERE id=10",
		"UPDATE gb_channel SET device_id='34020000001320000001', channel_id='34020000001320000002' WHERE id=20",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.NoError(t, db.Model(&platform).Updates(map[string]interface{}{"ptz_enabled": true, "device_control_enabled": true}).Error)
	require.NoError(t, db.Model(&model.GbCascadeChannelProjection{}).Where("platform_id=?", platform.ID).Update("ptz_allowed", true).Error)
	authority := authoritytest.Register(t, db, "")
	intents, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
	require.NoError(t, err)
	if len(barriers) > 0 {
		*barriers[0] = barrier
	}
	service, err := ptz.NewAuthorizedService(db, sender, time.Now, intents, barrier)
	require.NoError(t, err)
	t.Cleanup(service.Retire)
	oldDB, oldService, oldConfig := app.GormDbMysql, ptzService, app.ConfigYml
	app.GormDbMysql, ptzService, app.ConfigYml = db, service, cascadeInviteTestConfig{}
	t.Cleanup(func() { app.GormDbMysql, ptzService, app.ConfigYml = oldDB, oldService, oldConfig })
	return db, platform, store, service
}

func TestCascadeExtendedControlAuthorizedEndToEnd(t *testing.T) {
	for _, test := range []struct{ version, raw, params string }{
		{"2016", "A50F01820003003A", ""},
		{"2022", "A50F018100030039", "<PTZCmdParams><PresetName>大门</PresetName></PTZCmdParams>"},
		{"2022", "A50F01880200003F", "<PTZCmdParams><CruiseTrackName>巡航一</CruiseTrackName></PTZCmdParams>"},
		{"2022", "A50F018902020042", ""},
	} {
		t.Run(test.version+"/"+test.raw, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 2)
			sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
				if device != "34020000001320000001" || address != "192.0.2.30:5060" {
					return uac.TrackedMessageResult{}, fmt.Errorf("wrong downstream target")
				}
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "control-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, test.version, sender)
			profile := protocol.ProfileFor(protocol.Version(test.version))
			// Marshal with the upstream character set, including optional Chinese names.
			body := []byte(fmt.Sprintf(`<Control><CmdType>DeviceControl</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><PTZCmd>%s</PTZCmd>%s</Control>`, test.raw, test.params))
			decoded, err := manscdp.ParsePTZControlWithProfile(protocol.ProfileFor(protocol.Version2016), body)
			require.NoError(t, err)
			body, err = manscdp.BuildRawPTZControlParamsWithProfile(profile, decoded.DeviceID, decoded.SN, test.raw, decoded.Params)
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode, tx.Result()[0].Reason)
			select {
			case sent := <-downstream:
				control, err := manscdp.ParsePTZControlWithProfile(protocol.ProfileFor(protocol.Version2016), sent)
				require.NoError(t, err)
				require.Equal(t, "34020000001320000002", control.DeviceID)
				require.Equal(t, test.raw, control.Command.Raw)
				require.Equal(t, decoded.Params, control.Params)
				require.NotEqual(t, 97, control.SN)
			case <-time.After(2 * time.Second):
				t.Fatal("no downstream control")
			}
			var operation gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				return db.Where("device_id=10").Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			require.NotNil(t, operation.DeviceEpoch, "cascade control must use the device authorization barrier")
			require.EqualValues(t, 1, *operation.DeviceEpoch)
			require.NotNil(t, operation.DeviceIntentID)
			// An upstream transaction retransmission must not issue another movement.
			replayed, err := control.NewService(store, control.NewGormTargetLoader(db), ptzService).Forward(t.Context(), control.ForwardRequest{PlatformID: platform.ID, CallID: "cascade-status-test", Body: body})
			require.NoError(t, err)
			require.Equal(t, operation.OperationID, replayed.OperationID)
			require.Empty(t, downstream)
		})
	}
}

func TestCascadeExtendedControlRejectsUnauthorizedSource(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"cleanup pending", "UPDATE gb_device SET access_epoch=2 WHERE id=10"},
		{"epoch missing", "UPDATE gb_device SET access_epoch=NULL WHERE id=10"},
		{"owner mismatch", "UPDATE gb_channel SET owner_dept_id=1 WHERE id=20"},
		{"source deleted", "UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=10"},
		{"channel permission", "UPDATE gb_cascade_channel_projection SET ptz_allowed=0"},
		{"platform permission", "UPDATE gb_cascade_platform SET ptz_enabled=0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			calls := make(chan struct{}, 1)
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				calls <- struct{}{}
				return uac.TrackedMessageResult{StatusCode: 200}, nil
			})
			db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.Exec(test.statement).Error)
			body, err := manscdp.BuildRawPTZControlWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97, "A50F01820003003A")
			require.NoError(t, err)
			_, err = control.NewService(store, control.NewGormTargetLoader(db), service).Forward(t.Context(), control.ForwardRequest{PlatformID: platform.ID, CallID: "unauthorized-control", Body: body})
			require.Error(t, err)
			require.Empty(t, calls)
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestCascadeControlRejectsTargetMismatch(t *testing.T) {
	for _, changeTo := range []bool{false, true} {
		t.Run(fmt.Sprint(changeTo), func(t *testing.T) {
			_, platform, store := cascadeDeviceStatusFixture(t, "2022")
			body, err := manscdp.BuildRawPTZControlWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97, "A50F01820003003A")
			require.NoError(t, err)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			if changeTo {
				req.To().Address.User = "34020000001320000099"
			} else {
				req.Recipient.User = "34020000001320000099"
			}
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 403, tx.Result()[0].StatusCode)
		})
	}
}
