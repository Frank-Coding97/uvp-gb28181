package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newUpgradeSQLiteBaselineDB(t *testing.T) (*gorm.DB, *gbmodels.GbDevice) {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "upgrade.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)

	device := &gbmodels.GbDevice{
		DeviceID: "34020000001320009012", Name: "T12 upgrade device", IP: "192.0.2.30", Port: 5060,
		Transport: "UDP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	return db, device
}

func t12UpgradeTarget(device *gbmodels.GbDevice) Target {
	return Target{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2022),
	}
}

func t12UpgradeRequest(key string) Request {
	return Request{
		Confirmed: true, IdempotencyKey: key, Firmware: "v2.3.4",
		FileURL: "https://fixture.example/v2.3.4.bin", Manufacturer: "UVP", ActorID: 7, ActorDeptID: 8,
	}
}

func t12DeviceControlResponse(deviceCode string, sn int) []byte {
	return []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result></Response>`, sn, deviceCode))
}

func t12UpgradeResult(deviceCode string, sn int, sessionID, firmware string) []byte {
	return []byte(fmt.Sprintf(`<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><SessionID>%s</SessionID><UpgradeResult>OK</UpgradeResult><Firmware>%s</Firmware></Notify>`, sn, deviceCode, sessionID, firmware))
}

func TestT12UpgradeSQLiteBaselinePersistsProtocolMilestonesAndRejectsInvalid(t *testing.T) {
	db, device := newUpgradeSQLiteBaselineDB(t)
	sender := &fakeSender{}
	now := time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)
	service, err := NewService(db, sender, &fakeSN{next: 100}, func() time.Time { return now })
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	operation, deduplicated, err := service.Execute(ctx, t12UpgradeTarget(device), t12UpgradeRequest("t12-upgrade"))
	require.NoError(t, err)
	require.False(t, deduplicated)
	require.Equal(t, gbmodels.FirmwareUpgradeSent, operation.Status)
	require.Equal(t, http.StatusOK, operation.SIPStatus)
	require.Len(t, sender.bodies, 1)
	require.Contains(t, string(sender.bodies[0]), "<DeviceUpgrade>")

	consumed, err := service.OnUpgradeMessage(ctx, device.DeviceID, "t12-business", "2", t12DeviceControlResponse(device.DeviceID, operation.SN))
	require.NoError(t, err)
	require.True(t, consumed)
	accepted, err := service.Get(ctx, operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeAccepted, accepted.Status)

	consumed, statusCode, err := service.OnUpgradeResultMessage(ctx, device.DeviceID, "t12-final", "3", t12UpgradeResult(device.DeviceID, operation.SN, operation.SessionID, operation.Firmware))
	require.NoError(t, err)
	require.True(t, consumed)
	require.Equal(t, http.StatusOK, statusCode)
	completed, err := service.Get(ctx, operation.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, completed.Status)

	var storedDevice gbmodels.GbDevice
	require.NoError(t, db.Where("id = ?", device.ID).First(&storedDevice).Error)
	require.Equal(t, operation.Firmware, storedDevice.Firmware)

	invalid := t12UpgradeRequest("t12-invalid")
	invalid.Confirmed = false
	_, _, err = service.Execute(ctx, t12UpgradeTarget(device), invalid)
	require.ErrorIs(t, err, ErrInvalidArgument)
	require.Len(t, sender.bodies, 1, "未确认请求不能发送升级协议")

	consumed, statusCode, err = service.OnUpgradeResultMessage(ctx, device.DeviceID, "bad-final", "4", []byte(`<Notify>`))
	require.ErrorIs(t, err, ErrProtocolResponse)
	require.True(t, consumed)
	require.Equal(t, http.StatusBadRequest, statusCode)
}
