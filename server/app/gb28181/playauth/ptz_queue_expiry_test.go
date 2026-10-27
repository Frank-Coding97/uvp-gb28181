package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 排队超时必须落成**专属错误码**。
//
// ⛔ 历史上这里写的是 HOME_POSITION_UNAVAILABLE，和"PTZ 服务/设备授权不可用"混用：
// 现场看到这个码会去查服务状态和设备授权，而真相是"指令压根没发出去，设备侧什么都没收到"。
func TestPTZQueueExpiryUsesDedicatedCode(t *testing.T) {
	f, store := newIntentFixture(t)
	require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))

	id := intentIdentity(301)
	id.Kind = "ptz"
	now := time.Now().UTC().Truncate(time.Millisecond)
	deadline := now.Add(5 * time.Second)
	operation, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
		OperationID: "queue-expiry", IdempotencyKey: "queue-expiry", DeviceID: 1, DeviceCode: id.DeviceCode,
		ChannelID: 11, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
		Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 3, QueueDeadlineAt: &deadline,
	})
	require.NoError(t, err)

	require.NoError(t, store.ExpirePTZReservation(context.Background(), id, operation.ID, deadline))

	var stored gbmodels.GbPTZOperation
	require.NoError(t, f.db.First(&stored, operation.ID).Error)
	require.Equal(t, gbmodels.PTZOperationRejected, stored.Status)
	require.Equal(t, PTZQueueTimeout, stored.ErrorCode)
	require.NotEqual(t, "HOME_POSITION_UNAVAILABLE", stored.ErrorCode)

	// 同一行再过期一次必须是无操作（不能覆盖已终结的终态）。
	require.NoError(t, store.ExpirePTZReservation(context.Background(), id, operation.ID, deadline.Add(time.Minute)))
	require.NoError(t, f.db.First(&stored, operation.ID).Error)
	require.Equal(t, PTZQueueTimeout, stored.ErrorCode)
}
