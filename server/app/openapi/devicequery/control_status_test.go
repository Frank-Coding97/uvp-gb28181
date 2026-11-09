package devicequery

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestControlStatusPreservesMissingReportsAndDeviceBoundary(t *testing.T) {
	db, scope, deviceID, channelID, childID := newDeviceQueryDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}))
	zero := 0
	now := time.Now().UTC()
	old := now.Add(-2 * time.Minute)
	rows := []gbmodels.GbDeviceControlState{
		{DeviceID: deviceID, ChannelID: channelID, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: queryRootChannel, RecordState: gbmodels.ControlStateOn, GuardState: gbmodels.ControlStateUnknown, Freshness: gbmodels.ControlStateFresh, ObservedAt: old, AlarmInputCount: &zero},
		{DeviceID: deviceID, TargetScope: gbmodels.ControlTargetScopeAlarm, TargetCode: "alarm-input", GuardState: gbmodels.ControlStateOff, RecordState: gbmodels.ControlStateUnknown, Freshness: gbmodels.ControlStateFresh, ObservedAt: now},
		{DeviceID: childID, TargetScope: gbmodels.ControlTargetScopeAlarm, TargetCode: "private-input", ObservedAt: now},
	}
	require.NoError(t, db.Create(&rows).Error)
	result, err := New(db).GetChannelControlStatusInScope(context.Background(), scope, queryRootDevice, queryRootChannel)
	require.NoError(t, err)
	require.NotNil(t, result.Record)
	require.Equal(t, "stale", result.Record.Freshness)
	require.Equal(t, 0, *result.Record.AlarmInputCount)
	require.Nil(t, result.Record.OnlineState)
	require.Len(t, result.AlarmInputs, 1)
	require.Equal(t, "alarm-input", result.AlarmInputs[0].TargetCode)
}
