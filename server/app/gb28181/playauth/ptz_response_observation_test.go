package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZResponseScopeCannotLeaveOrphanOnReuse(t *testing.T) {
	f, store := newIntentFixture(t)
	require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	id := intentIdentity(901)
	id.Kind = "ptz"
	op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
		OperationID: "scope-source", IdempotencyKey: "scope-source", DeviceID: 1, DeviceCode: id.DeviceCode,
		ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
		CmdType: "DeviceControl", Action: "home_position", Status: gbmodels.PTZOperationQueued, MaxAttempts: 1})
	require.NoError(t, err)
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		scope, err := store.BeginPTZResponseObservation(tx, op)
		require.NoError(t, err)
		require.NoError(t, tx.Model(&op).Update("status", gbmodels.PTZOperationAccepted).Error)
		deadline := time.Now().Add(time.Second)
		child := gbmodels.GbPTZOperation{OperationID: "first-child", CmdType: "HomePositionQuery", Action: "refresh_home_position",
			Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, QueueDeadlineAt: &deadline}
		require.NoError(t, scope.ReserveHomePositionQuery(child))
		child.OperationID = "second-child"
		require.Error(t, scope.ReserveHomePositionQuery(child))
		return nil // Deliberately swallow the second error to test API safety.
	}))
	var count int64
	require.NoError(t, f.db.Model(&DeviceOperationIntent{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "source plus exactly one child, no orphan reservation")
}
