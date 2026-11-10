package ptz

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestExpireApplicationLocksByPrimaryKeyAndPreservesResponses(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	now := fixture.clock.Now()
	var operations []gbmodels.GbPTZOperation
	for i := 0; i < 5; i++ {
		deadline := now.Add(-time.Minute)
		status := gbmodels.PTZOperationSent
		if i == 3 {
			deadline = now.Add(time.Minute)
		}
		if i == 4 {
			status = gbmodels.PTZOperationAccepted
		}
		operations = append(operations, gbmodels.GbPTZOperation{
			OperationID: fmt.Sprintf("application-%d", i), IdempotencyKey: fmt.Sprintf("application-key-%d", i),
			DeviceID: 1, DeviceCode: "D", ChannelID: 1, ChannelCode: "C",
			CmdType: "ConfigDownload", Action: actionRefreshDeviceConfigs, PayloadJSON: "{}", SN: i + 1,
			Status: status, ResponseRequired: true, MaxAttempts: 1, DeadlineAt: &deadline, CreatedAt: now,
		})
	}
	require.NoError(t, fixture.db.Create(&operations).Error)
	responseArrived := false
	require.NoError(t, fixture.db.Callback().Query().After("gorm:query").Register("accept-after-expiry-scan", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]uint); !ok || responseArrived {
			return
		}
		responseArrived = true
		tx.AddError(fixture.db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operations[0].ID).
			Update("status", gbmodels.PTZOperationAccepted).Error)
	}))
	var updates []string
	require.NoError(t, fixture.db.Callback().Update().After("gorm:update").Register("capture-expiry-update", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_ptz_operation" {
			updates = append(updates, tx.Statement.SQL.String())
		}
	}))

	require.NoError(t, fixture.scheduler.expireApplication(t.Context(), now))
	require.True(t, responseArrived)
	require.NotEmpty(t, updates)
	for _, statement := range updates {
		require.Contains(t, strings.ToLower(statement), "id = ?", "expiry updates must start from the primary key")
	}
	for i, operation := range operations {
		stored := loadSchedulerOperation(t, fixture.db, operation.ID)
		if i == 0 {
			require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status)
		} else if i < 3 {
			require.Equal(t, gbmodels.PTZOperationTimeout, stored.Status)
		} else {
			require.Equal(t, operation.Status, stored.Status)
		}
	}
}

func TestExpireTransportLocksByPrimaryKey(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 1)
	now := fixture.clock.Now()
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", operation.ID).
		Update("transport_deadline_at", now.Add(-time.Minute)).Error)
	var updates []string
	require.NoError(t, fixture.db.Callback().Update().After("gorm:update").Register("capture-transport-expiry", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_ptz_operation" {
			updates = append(updates, tx.Statement.SQL.String())
		}
	}))

	require.NoError(t, fixture.scheduler.expireTransport(t.Context(), now))
	require.NotEmpty(t, updates)
	for _, statement := range updates {
		require.Contains(t, strings.ToLower(statement), "id = ?", "transport expiry must use the primary key")
	}
	stored := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationUnknown, stored.Status)
	require.Equal(t, schedulerErrorTransportUnknown, stored.ErrorCode)
}

// 排队超时判死必须"分批 + 按主键"，不能退回一次性范围 UPDATE。
//
// 线上事故（2026-11-09 万级压测）：范围 UPDATE 会先锁
// (response_required,status,queue_deadline_at) 二级索引再回表锁主键，而单条结果回写
// 是"先主键再改索引"，两条路径加锁顺序相反 ⇒ 6311 次 1213 死锁。
// 本用例同时锁住两件事：
//  1. 单轮 tick 的判死量有上限（chunk×rounds），不会把一批 2 万条塞进一个长事务；
//  2. 多轮之后积压能被收干净，不会有"永远判不完"的残留。
func TestExpireQueuedIsChunkedAndEventuallyDrains(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	now := fixture.clock.Now()
	expired := now.Add(-time.Minute)
	total := schedulerExpireChunk*schedulerExpireRounds + 10

	batch := make([]gbmodels.GbPTZOperation, 0, total)
	for i := 0; i < total; i++ {
		deadline := expired
		batch = append(batch, gbmodels.GbPTZOperation{
			OperationID: fmt.Sprintf("expired-%d", i), IdempotencyKey: fmt.Sprintf("expired-key-%d", i),
			DeviceID: 1, DeviceCode: "D", ChannelID: 1, ChannelCode: "C",
			CmdType: "ConfigDownload", Action: actionRefreshDeviceConfigs, PayloadJSON: "{}", SN: i + 1,
			Status: gbmodels.PTZOperationQueued, ResponseRequired: true, MaxAttempts: 1,
			QueueDeadlineAt: &deadline, CreatedAt: expired,
		})
	}
	require.NoError(t, fixture.db.CreateInBatches(&batch, 500).Error)
	// GORM 会跳过带 default 的零值列（attempt 的默认值是 1），而入队态必须是 attempt=0 ——
	// 判死条件只认 `attempt = 0`，这里显式回写，别让用例测了个空集合。
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Where("1 = 1").Update("attempt", 0).Error)

	countBy := func(status gbmodels.PTZOperationStatus) int64 {
		var n int64
		require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Where("status = ?", status).Count(&n).Error)
		return n
	}

	require.NoError(t, fixture.scheduler.RunDue(now))
	require.Equal(t, int64(schedulerExpireChunk*schedulerExpireRounds), countBy(gbmodels.PTZOperationRejected),
		"单轮 tick 最多判死 chunk×rounds 条，其余留给下一轮，避免长事务放大锁冲突")
	require.Equal(t, int64(10), countBy(gbmodels.PTZOperationQueued))

	require.NoError(t, fixture.scheduler.RunDue(now))
	require.Zero(t, countBy(gbmodels.PTZOperationQueued), "积压必须能在后续 tick 收干净")

	var codes []string
	require.NoError(t, fixture.db.Model(&gbmodels.GbPTZOperation{}).Distinct().Pluck("error_code", &codes).Error)
	require.Equal(t, []string{"PTZ_QUEUE_TIMEOUT"}, codes)
}

// 候选查询必须限量：积压两万条时每 250ms 全量捞一遍既浪费也放大锁冲突。
func TestClaimCandidateLimitIsBounded(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	require.GreaterOrEqual(t, fixture.scheduler.claimCandidateLimit(), 64)

	small := NewScheduler(fixture.scheduler.service, WithSchedulerCapacity(2))
	require.Equal(t, 64, small.claimCandidateLimit(), "小容量也要留足余量，否则取不到可派发的候选")

	large := NewScheduler(fixture.scheduler.service, WithSchedulerCapacity(4096))
	require.Equal(t, 1024, large.claimCandidateLimit(), "上限兜住，避免把整张表捞进内存")
}
