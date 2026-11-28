package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 能力去重集合的判据：queued（还没轮到）/ sent（已发等应答）/ accepted（设备已应答）
// 都算"已经问过"，不再重复排；排队超时 / 传输不确定的通道必须留在待读集合里
// —— 那些报文压根没到设备，正是要补的。
func TestFreshVideoParamChannelsOnlyCountsDeliveredReads(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	now := time.Now().Truncate(time.Millisecond)

	videoParam := `{"configTypes":["` + manscdp.ConfigTypeVideoParamOpt + `"]}`
	create := func(operationID, channelCode, payload string, status gbmodels.PTZOperationStatus, scope string, createdAt time.Time) {
		t.Helper()
		require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
			OperationID: operationID, IdempotencyKey: operationID, DeviceID: 1, DeviceCode: "D",
			ChannelID: 11, ChannelCode: channelCode, CmdType: manscdp.CmdConfigDownload,
			Action: actionRefreshDeviceConfigs, PayloadJSON: payload, TargetScope: scope, TargetCode: channelCode,
			Status: status, CreatedAt: createdAt, ResponseRequired: true, MaxAttempts: 3,
		}).Error)
	}

	create("accepted", "CH-1", videoParam, gbmodels.PTZOperationAccepted, gbmodels.ControlTargetScopeChannel, now.Add(-time.Minute))
	create("sent", "CH-2", videoParam, gbmodels.PTZOperationSent, gbmodels.ControlTargetScopeChannel, now.Add(-2*time.Minute))
	create("queued", "CH-6", videoParam, gbmodels.PTZOperationQueued, gbmodels.ControlTargetScopeChannel, now.Add(-time.Minute))
	create("queue-timeout", "CH-3", videoParam, gbmodels.PTZOperationRejected, gbmodels.ControlTargetScopeChannel, now.Add(-time.Minute))
	create("transport-unknown", "CH-4", videoParam, gbmodels.PTZOperationUnknown, gbmodels.ControlTargetScopeChannel, now.Add(-time.Minute))
	create("application-timeout", "CH-7", videoParam, gbmodels.PTZOperationTimeout, gbmodels.ControlTargetScopeChannel, now.Add(-time.Minute))
	create("stale", "CH-5", videoParam, gbmodels.PTZOperationAccepted, gbmodels.ControlTargetScopeChannel, now.Add(-2*time.Hour))
	create("basic-param", "D", `{"configTypes":["`+manscdp.ConfigTypeBasicParam+`"]}`, gbmodels.PTZOperationAccepted, gbmodels.ControlTargetScopeDevice, now.Add(-time.Minute))

	fresh, err := service.FreshVideoParamChannels(context.Background(), "D", now.Add(-capabilityFreshWindowForTest))
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{"CH-1": {}, "CH-2": {}, "CH-6": {}}, fresh)
}

// capabilityFreshWindowForTest 与 handler 侧的 capabilityFreshFor 同为 10 分钟；
// 这里只用其值域，避免把常量跨包导出成公共契约。
const capabilityFreshWindowForTest = 10 * time.Minute
