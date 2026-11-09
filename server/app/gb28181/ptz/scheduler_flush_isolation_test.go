package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
)

// 2026-11-09 第三轮万级压测事故的防回归：
//
//	一条结果写回在 5 秒预算内失败 ⇒ 原 flushPendingResults "队首失败即 return" ⇒
//	调度器每个 tick 都死在第一步（claimDue 永不执行，42 条指令躺了 40 分钟），
//	而 Execute 每次下发前也要先 flush（同一把 pendingMu）⇒ 99.6% 的能力刷新静默卡死。
//
// 这里锁死三条契约：
//  1. 失败**要上报**（写回没落库就不许领新任务，这是既有安全契约）；
//  2. 但坏项**不阻断**同批其它项落库，且不持有全局锁做 IO；
//  3. 坏项连续失败到阈值后**放弃**——这才是永久停摆的解药。
func TestFlushRuntimeKeepsOtherItemsMovingDespiteFailure(t *testing.T) {
	f := newSchedulerFixture(t, 4)

	healthyAttempt := seedDispatchingAttempt(t, f.db, f.clock.Now())
	f.scheduler.pendingResults[healthyAttempt.ID] = schedulerPendingResult{
		attempt:    healthyAttempt,
		result:     uac.TrackedMessageResult{StatusCode: 200, Attempted: true, CallID: "c", CSeq: "1"},
		observedAt: f.clock.Now(),
	}
	// 指向不存在的 attempt 行：persistAttemptOutcome 必然报错，等价于现场那条
	// "写回预算内落不进去"的坏结果。
	const brokenID = uint(999999)
	f.scheduler.pendingResults[brokenID] = schedulerPendingResult{
		attempt:    gbmodels.GbPTZOperationAttempt{ID: brokenID, OperationID: 999999},
		observedAt: f.clock.Now(),
	}

	require.Error(t, f.scheduler.flushPendingResults(context.Background()),
		"写回失败必须上报：调用方据此才不会被允许领新任务")

	_, healthyLeft := f.scheduler.pendingResults[healthyAttempt.ID]
	require.False(t, healthyLeft, "同批的正常结果必须照常落库，不能被坏项拦住")

	broken, brokenLeft := f.scheduler.pendingResults[brokenID]
	require.True(t, brokenLeft, "未到阈值前坏项要留在队列里重试")
	require.Equal(t, 1, broken.writebackAttempts)
	require.False(t, broken.writebackFailedAt.IsZero(), "首次失败时间必须记下来，放弃判定靠它")
}

func TestFlushAbandonsWritebackThatKeepsFailing(t *testing.T) {
	f := newSchedulerFixture(t, 4)

	const brokenID = uint(888888)
	f.scheduler.pendingResults[brokenID] = schedulerPendingResult{
		attempt:    gbmodels.GbPTZOperationAttempt{ID: brokenID, OperationID: 888888},
		observedAt: f.clock.Now(),
	}

	for i := 1; i <= schedulerWritebackMaxAttempts; i++ {
		require.Error(t, f.scheduler.flushPendingResults(context.Background()), "第 %d 次仍该上报失败", i)
	}
	// 达到阈值的那一轮已经把坏项丢弃，所以下一次 flush 应该干净通过。
	require.NoError(t, f.scheduler.flushPendingResults(context.Background()),
		"坏项被放弃后，flush 必须立刻恢复正常——这就是『不再永久停摆』的判据")

	_, left := f.scheduler.pendingResults[brokenID]
	require.False(t, left,
		"连续失败达到 %d 次后必须放弃：不放弃就等于把调度器永久钉死在这一条上（收敛交给 expireTransport）",
		schedulerWritebackMaxAttempts)
}

// 严格模式的语义不能被"运行期尽力"污染：停机路径要靠它判断租约是否真的排空。
func TestFlushStrictStillReportsFailure(t *testing.T) {
	f := newSchedulerFixture(t, 4)

	const brokenID = uint(777777)
	f.scheduler.pendingResults[brokenID] = schedulerPendingResult{
		attempt:    gbmodels.GbPTZOperationAttempt{ID: brokenID, OperationID: 777777},
		observedAt: f.clock.Now(),
	}

	require.Error(t, f.scheduler.FlushResults(context.Background()))

	kept, left := f.scheduler.pendingResults[brokenID]
	require.True(t, left, "严格模式不许丢项：停机时宁可留下待重试")
	require.Zero(t, kept.writebackAttempts, "严格模式不该推进放弃计数，否则停机重试会自己把项丢掉")
}

// 单步失败不能让整轮"后面全部跳过"——这是 42 条指令卡死的直接机制。
//
// ⛔ 但"失败不许领新任务"是既有安全契约（写回没落库就发新指令，会让
// 「已发出但没记账」和重试叠在一起），所以这里断言的是**后续收敛步骤仍执行**，
// 而不是 claim 仍然发生。
func TestRunDueKeepsConvergingAfterAnEarlierStepFails(t *testing.T) {
	f := newSchedulerFixture(t, 4)
	now := f.clock.Now()

	// 让 expireQueued 必然失败：带 epoch 的过期指令需要 intent 对账，
	// 而库里没有对应的 intent 行（真实压测里就是"状态已被别处推进"这一类）。
	expired := f.createOperation(t, 1)
	require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", expired.ID).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true,
		"device_epoch": 1, "device_intent_id": "0123456789abcdef0123456789abcdef",
		"queue_deadline_at": now.Add(-time.Minute),
	}).Error)

	// 一条传输已过期、应由**下一步** expireTransport 收敛的指令。
	// ⛔ 不能复用 createOperation：它用固定 clock 的纳秒值当业务键，同一测试里连调会撞唯一键。
	require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": "transport-expired-op", "idempotency_key": "transport-expired-key",
		"device_id": 1, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": "HomePositionQuery", "action": "refresh_home_position", "payload_json": "{}", "sn": 42,
		"status": gbmodels.PTZOperationQueued, "attempt": 1, "response_required": true, "max_attempts": 3,
		"queue_deadline_at":      now.Add(time.Minute),
		"transport_deadline_at":  now.Add(-time.Minute),
		"dispatch_started_at":    now.Add(-2 * time.Minute),
		"created_at":             now,
	}).Error)

	require.Error(t, f.scheduler.runDue(context.Background(), now),
		"收敛失败必须上报（否则调用方会误以为可以领新任务）")

	var converged gbmodels.GbPTZOperation
	require.NoError(t, f.db.Where("operation_id = ?", "transport-expired-op").First(&converged).Error)
	require.Equal(t, gbmodels.PTZOperationUnknown, converged.Status,
		"第 1 步失败不能把后面的 expire_transport 一起跳过，否则过期指令永远悬在 queued")
}

// 收敛没干净就不许领新任务：这是"已发出但没记账"和重试叠加的唯一防线。
func TestRunDueDoesNotClaimWhenConvergenceFails(t *testing.T) {
	f := newSchedulerFixture(t, 4)
	now := f.clock.Now()

	expired := f.createOperation(t, 1)
	require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", expired.ID).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true,
		"device_epoch": 1, "device_intent_id": "fedcba9876543210fedcba9876543210",
		"queue_deadline_at": now.Add(-time.Minute),
	}).Error)

	require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": "must-not-claim", "idempotency_key": "must-not-claim-key",
		"device_id": 1, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": "HomePositionQuery", "action": "refresh_home_position", "payload_json": "{}", "sn": 43,
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true, "max_attempts": 3,
		"queue_deadline_at": now.Add(5 * time.Second), "created_at": now,
	}).Error)

	require.Error(t, f.scheduler.runDue(context.Background(), now))

	var untouched gbmodels.GbPTZOperation
	require.NoError(t, f.db.Where("operation_id = ?", "must-not-claim").First(&untouched).Error)
	require.Nil(t, untouched.DispatchStartedAt,
		"收敛失败时不得领新任务：否则外部已发出的报文没有落库证据，重试会与它叠加")
}

// seedDispatchingAttempt 造一条"已派发、等结果写回"的 attempt 及其 operation。
func seedDispatchingAttempt(t *testing.T, db *gorm.DB, now time.Time) gbmodels.GbPTZOperationAttempt {
	t.Helper()
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": "flush-healthy-op", "idempotency_key": "flush-healthy-key",
		"device_id": 1, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": "DeviceControl", "action": "test", "payload_json": "{}", "sn": 1,
		"status": gbmodels.PTZOperationSent, "attempt": 1, "response_required": true, "max_attempts": 3,
		"created_at": now,
	}).Error)
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "flush-healthy-op").First(&operation).Error)

	require.NoError(t, db.Model(&gbmodels.GbPTZOperationAttempt{}).Create(map[string]interface{}{
		"operation_id": operation.ID, "attempt_no": 1, "sn": 1,
		"status":      gbmodels.PTZOperationAttemptDispatching,
		"started_at":  now, "lease_until": now.Add(time.Minute), "created_at": now,
	}).Error)
	var attempt gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.Where("operation_id = ? AND attempt_no = ?", operation.ID, 1).First(&attempt).Error)
	return attempt
}
