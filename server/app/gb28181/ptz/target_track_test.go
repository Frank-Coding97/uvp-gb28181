package ptz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

// recordingTrackedSender 既记录发出去的报文，又能按需让发送失败 ——
// "没发出去就不改意图"这条口径靠它才验得出来。
type recordingTrackedSender struct {
	bodies [][]byte
	calls  int
	err    error
}

func (s *recordingTrackedSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.calls++
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	if s.err != nil {
		return uac.TrackedMessageResult{}, s.err
	}
	return uac.TrackedMessageResult{CallID: "call-tt", CSeq: "9", StatusCode: 200}, nil
}

func newTargetTrackTestService(t *testing.T, sender TrackedSender) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceTargetTrack{}))
	service, err := NewService(db, sender, func() time.Time {
		return time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	})
	require.NoError(t, err)
	return service, db
}

func targetTrackTestTarget() Target {
	target := testTarget()
	target.ChannelID = 7
	target.ChannelCode = "34020000001320000001"
	return target
}

func manualArea() manscdp.TargetTrackArea {
	return manscdp.TargetTrackArea{Length: 1920, Width: 1080, MidPointX: 960, MidPointY: 540, LengthX: 200, LengthY: 100}
}

func loadIntent(t *testing.T, db *gorm.DB, deviceID uint, targetCode string) (gbmodels.GbDeviceTargetTrack, bool) {
	t.Helper()
	var row gbmodels.GbDeviceTargetTrack
	result := db.Where("device_id = ? AND target_code = ?", deviceID, targetCode).Limit(1).Find(&row)
	require.NoError(t, result.Error)
	return row, result.RowsAffected == 1
}

// ⛔ 本组最重要的一条：**无应答命令一次调用后即终态 `sent`**，且报文里不带等待应答的语义。
//
// 9.3.1 d) 与表 1 序号 13 两处标准原文都写着目标跟踪「（无）」应答。若这里被写成
// ResponseRequired=true，设备按标准不回执 → operation 排到 deadline 才落 timeout →
// 前端把一次正常下发显示成「结果未知」。
func TestTrackTargetSendsOneWayCommandAndRecordsIntent(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, db := newTargetTrackTestService(t, sender)
	target := targetTrackTestTarget()
	area := manualArea()

	operation, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{
		Mode: manscdp.TargetTrackManual, DeviceID2: "34020000001320000002", Area: &area,
	}, 11, 22, "tt-manual-1")
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, operation.Status)
	require.False(t, operation.ResponseRequired, "目标跟踪是无应答命令（9.3.1 d)）")
	require.Equal(t, 1, operation.MaxAttempts)
	require.Equal(t, actionTargetTrack, operation.Action)
	require.Equal(t, target.ChannelCode, operation.TargetCode)

	// 无应答命令由 Execute 同步发出：一次调用即真的发出去了，不存在"排队中"。
	require.Equal(t, 1, sender.calls)
	require.Len(t, sender.bodies, 1)
	wire := string(sender.bodies[0])
	require.Contains(t, wire, "<TargetTrack>Manual</TargetTrack>")
	require.Contains(t, wire, "<DeviceID2>34020000001320000002</DeviceID2>")
	require.Contains(t, wire, "<TargetArea><Length>1920</Length>")

	intent, found := loadIntent(t, db, target.DeviceID, target.ChannelCode)
	require.True(t, found, "下发成功后必须留下平台意图")
	require.Equal(t, gbmodels.TargetTrackModeManual, intent.Mode)
	require.Equal(t, "34020000001320000002", intent.DeviceID2)
	require.Equal(t, uint(11), intent.CommandedBy)
	require.Equal(t, uint(22), intent.CommandedByDeptID)
	require.Equal(t, operation.ID, intent.SourceOperationSeq)
	require.Equal(t, operation.SN, intent.SourceSN)
	require.NotNil(t, intent.AreaLength)
	require.Equal(t, 1920, *intent.AreaLength)
	require.Equal(t, 100, *intent.AreaLengthY)
	// 意图表里留着实际下发的那一帧报文——排查"框选位置不对"时不用再去 SIP 轨迹表捞。
	require.Contains(t, intent.RawSummary, "<TargetTrack>Manual</TargetTrack>")
}

// ⛔ 发送失败**不许**改意图：设备什么都没收到，界面上的"当前指令"必须保持原样，
// 否则会出现「界面写着已下发自动跟踪、设备其实还在按上一条指令跑」。
func TestTrackTargetDoesNotRecordIntentWhenSendFails(t *testing.T) {
	sender := &recordingTrackedSender{err: context.DeadlineExceeded}
	service, db := newTargetTrackTestService(t, sender)
	target := targetTrackTestTarget()

	_, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackAuto}, 11, 22, "tt-fail-1")
	require.Error(t, err)

	_, found := loadIntent(t, db, target.DeviceID, target.ChannelCode)
	require.False(t, found, "没发出去的指令不能留下意图")
}

// 成功发 Auto 之后再发 Stop：意图必须被**更新**，而且六列框选坐标要被清成 NULL。
// ⛔ 用 struct 更新（GORM 的 Updates(model)）会把 nil 当成"不更新"，
// 于是"停止跟踪"之后界面还显示着上一次的框 —— 这条专门钉住"显式把 nil 写进去"。
func TestTrackTargetStopClearsPreviousArea(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, db := newTargetTrackTestService(t, sender)
	target := targetTrackTestTarget()
	area := manualArea()

	_, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{
		Mode: manscdp.TargetTrackManual, Area: &area,
	}, 11, 22, "tt-m-1")
	require.NoError(t, err)

	_, err = service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackStop}, 11, 22, "tt-s-1")
	require.NoError(t, err)

	intent, found := loadIntent(t, db, target.DeviceID, target.ChannelCode)
	require.True(t, found)
	require.Equal(t, gbmodels.TargetTrackModeStop, intent.Mode)
	require.Nil(t, intent.AreaLength, "Stop 必须把上一次的框清掉，不能留成 0 或旧值")
	require.Nil(t, intent.AreaLengthX)
	require.Nil(t, intent.AreaMidPointX)
	require.Equal(t, "", intent.DeviceID2)

	// 仍然是一行：同一台球机的多次下发是"同一条指令的更新"，不是多条记录。
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceTargetTrack{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

// 同一 idempotency key 的重放会拿到**旧的那条** operation；若不加序号保护，
// 一次迟到的重放就会把更晚一次下发的意图覆盖回去
// （界面上会看到"刚点的 Stop 又变回 Auto"）。
func TestTrackTargetReplayDoesNotOverwriteNewerIntent(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, db := newTargetTrackTestService(t, sender)
	target := targetTrackTestTarget()

	auto, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackAuto}, 11, 22, "tt-replay")
	require.NoError(t, err)

	_, err = service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackStop}, 11, 22, "tt-newer")
	require.NoError(t, err)

	// 重放第一次那条（同 key 同参数）。
	replayed, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackAuto}, 11, 22, "tt-replay")
	require.NoError(t, err)
	require.Equal(t, auto.OperationID, replayed.OperationID, "同 idempotency key 必须复用原 operation")
	// 三次调用只应发出两帧：Auto 一次、Stop 一次，重放不再发。
	require.Equal(t, 2, sender.calls, "重放不应再发一次 SIP")

	intent, found := loadIntent(t, db, target.DeviceID, target.ChannelCode)
	require.True(t, found)
	require.Equal(t, gbmodels.TargetTrackModeStop, intent.Mode, "迟到的重放不能把更新的意图写回去")
}

func TestTrackTargetRejectsManualWithoutArea(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, _ := newTargetTrackTestService(t, sender)

	_, err := service.TrackTarget(context.Background(), targetTrackTestTarget(), TargetTrackRequest{Mode: manscdp.TargetTrackManual}, 1, 1, "tt-no-area")
	require.Error(t, err)
	require.Contains(t, err.Error(), "必须携带 TargetArea")
	require.Zero(t, sender.calls, "参数不合法时不能发出任何报文")
}

func TestTrackTargetRequiresChannelCode(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, _ := newTargetTrackTestService(t, sender)
	target := targetTrackTestTarget()
	target.ChannelCode = "  "

	_, err := service.TrackTarget(context.Background(), target, TargetTrackRequest{Mode: manscdp.TargetTrackStop}, 1, 1, "tt-no-code")
	require.Error(t, err)
	require.Contains(t, err.Error(), "缺少目标通道编码")
}

// Auto/Stop 不带坐标时，报文里不能出现空的 `<TargetArea/>` ——
// 那会让设备以为"平台给了一个 (0,0,0,0,0,0) 的框"，与"平台没给过框"不是一回事。
func TestTrackTargetAutoOmitsTargetAreaFromWire(t *testing.T) {
	sender := &recordingTrackedSender{}
	service, _ := newTargetTrackTestService(t, sender)

	_, err := service.TrackTarget(context.Background(), targetTrackTestTarget(), TargetTrackRequest{Mode: manscdp.TargetTrackAuto}, 1, 1, "tt-auto")
	require.NoError(t, err)
	require.Len(t, sender.bodies, 1)
	wire := string(sender.bodies[0])
	require.Contains(t, wire, "<TargetTrack>Auto</TargetTrack>")
	require.False(t, strings.Contains(wire, "<TargetArea"))
	require.False(t, strings.Contains(wire, "<DeviceID2"))
}
