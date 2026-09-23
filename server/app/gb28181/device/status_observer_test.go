package device

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestStatusObserverPublishesOnlyAfterSuccessfulStateTransactions(t *testing.T) {
	observer := &capturedStatusObserver{}
	SetStatusObserver(observer)
	t.Cleanup(func() { SetStatusObserver(nil) })
	db := newStatusEventTestDB(t)
	ctx := context.Background()
	deviceID := "34020000002000000041"

	_, err := HandleRegister(ctx, RegisterInfo{DeviceID: deviceID, Expires: 3600}, 60)
	require.NoError(t, err)
	require.Equal(t, []string{"REGISTER_ONLINE"}, observer.reasons)
	require.NoError(t, HandleUnregister(ctx, deviceID))
	_, err = Keepalive(ctx, deviceID)
	require.NoError(t, err)
	require.Equal(t, []string{"REGISTER_ONLINE", "UNREGISTER_OFFLINE", "HEARTBEAT_RECOVERED"}, observer.reasons)

	app.ConfigYml = preallocationTestConfig{enabled: true}
	_, err = HandleRegister(ctx, RegisterInfo{DeviceID: "34020000002000000999", Expires: 3600}, 60)
	require.ErrorIs(t, err, ErrDeviceNotPreallocated)
	require.Len(t, observer.reasons, 3, "rolled back register must not publish")

	var stored gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&stored).Error)
}

type capturedStatusObserver struct{ reasons []string }

func (o *capturedStatusObserver) DeviceStatusChanged(_ context.Context, _ string, _ bool, reason string) {
	o.reasons = append(o.reasons, reason)
}
