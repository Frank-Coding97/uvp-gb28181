package gb28181

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeMaintenanceControlRequiresIndependentGrant(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
		t.Error("maintenance permission is disabled; unexpected device send")
		return uac.TrackedMessageResult{}, nil
	})
	_, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(fmt.Sprintf(`<Control><CmdType>DeviceControl</CmdType><SN>4</SN><DeviceID>%s</DeviceID><FormatSDCard>0</FormatSDCard></Control>`, "34020000001320000012")))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, newCascadeMaintenanceControlHandler(store, nil)(req, tx))
	require.Equal(t, 403, tx.Result()[0].StatusCode)
}

func TestCascadeMaintenanceFormatUsesIndependentOneWayPath(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 1)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		require.Equal(t, "34020000001320000001", device)
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "cascade-format-down", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET maintenance_enabled=1,device_control_enabled=0,ptz_enabled=0 WHERE id=?", platform.ID).Error)
	scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	body := []byte(`<Control><CmdType>DeviceControl</CmdType><SN>19</SN><DeviceID>34020000001320000012</DeviceID><FormatSDCard>0</FormatSDCard></Control>`)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, newCascadeMaintenanceControlHandler(store, nil)(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode, tx.Result()[0].Reason)
	select {
	case sent := <-downstream:
		require.Contains(t, string(sent), "<FormatSDCard>0</FormatSDCard>")
		require.Contains(t, string(sent), "<DeviceID>34020000001320000002</DeviceID>")
	case <-time.After(3 * time.Second):
		t.Fatal("cascade storage format was not sent")
	}
	var operation gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action=?", "format_sd").Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
	}, time.Second, 5*time.Millisecond)
	require.False(t, operation.ResponseRequired)
	require.Equal(t, 1, operation.MaxAttempts)
	require.Contains(t, operation.PayloadJSON, `"publishedId":"34020000001320000012"`)
}

func TestCascadeMaintenanceTeleBootUsesDeviceScopeStateMachine(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 1)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
		require.Equal(t, "34020000001320000001", device)
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "cascade-reboot-down", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET maintenance_enabled=1,device_control_enabled=0,ptz_enabled=0 WHERE id=?", platform.ID).Error)
	body := []byte(`<Control><CmdType>DeviceControl</CmdType><SN>20</SN><DeviceID>34020000001320000012</DeviceID><TeleBoot>Boot</TeleBoot></Control>`)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, newCascadeMaintenanceControlHandler(store, nil)(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode, tx.Result()[0].Reason)
	select {
	case sent := <-downstream:
		require.Contains(t, string(sent), "<TeleBoot>Boot</TeleBoot>")
		require.Contains(t, string(sent), "<DeviceID>34020000001320000001</DeviceID>")
	case <-time.After(3 * time.Second):
		t.Fatal("cascade TeleBoot was not sent")
	}
	var operation gbmodels.GbPTZOperation
	require.Eventually(t, func() bool {
		return db.Where("action=?", "teleboot").Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
	}, time.Second, 5*time.Millisecond)
	require.Zero(t, operation.ChannelID)
	require.Empty(t, operation.ChannelCode)
	require.Equal(t, gbmodels.ControlTargetScopeDevice, operation.TargetScope)
	require.False(t, operation.ResponseRequired)
	require.Contains(t, operation.PayloadJSON, `"publishedId":"34020000001320000012"`)
}
