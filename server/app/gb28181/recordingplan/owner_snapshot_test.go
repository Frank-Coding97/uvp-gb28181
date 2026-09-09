package recordingplan

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestChangingModeRetainsOriginalRecorderOwner(t *testing.T) {
	assignment, _ := newAssignmentService(t)
	channel := models.GbChannel{DeviceID: "D", ChannelID: "C", OwnerDeptID: 1, RecordingMode: models.RecordingModeScheduled}
	require.NoError(t, assignment.db.Create(&channel).Error)
	planID := uint64(3)
	state := models.GbRecordingPlanChannelState{ChannelID: channel.ID, PlanID: &planID, DesiredState: models.RecordingDesiredRecording, ActualState: models.RecordingStateRecording, ReconcileAt: time.Now(), RecorderOwnerKind: "plan", RecorderOwnerID: "plan-run-1", RecorderClaimVersion: 7}
	require.NoError(t, assignment.db.Create(&state).Error)
	for _, mode := range []string{models.RecordingModeOff, models.RecordingModeContinuous} {
		require.NoError(t, assignment.SetMode(context.Background(), 1, channel.ID, mode))
		var restored models.GbRecordingPlanChannelState
		require.NoError(t, assignment.db.First(&restored, "channel_id = ?", channel.ID).Error)
		require.Nil(t, restored.PlanID)
		require.Equal(t, state.RecorderOwnerKind, restored.RecorderOwnerKind)
		require.Equal(t, state.RecorderOwnerID, restored.RecorderOwnerID)
		require.Equal(t, state.RecorderClaimVersion, restored.RecorderClaimVersion)
	}
}
