package recordcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// ── 片内实时进度 ──────────────────────────────────────────────────────
//
// ⛔⛔ 这一组是「进度条一直 0%、看着像卡死」的根因护栏。
//
// 症状：界面上「1.8 MB/s」明明在跑，进度 0%、已缓存 0 秒 / 0 B 纹丝不动。
//
// 根因不在前端（前端 2 秒轮询一次，没问题），也不在录制（ZLM 上文件确实在长大），
// 而在这里：分片清单（`segments` 列）只在**整片收尾**的那一刻才落库，
// 而单片媒体上限最长可到 SegmentMediaCap —— 常见 4× 设备就是
// `25min × 4 × 0.9 = 90 分钟媒体 ≈ 22.5 分钟墙钟`。
// 这 22.5 分钟里 `task.CachedMediaDuration()` 恒为 0，
// 于是详情接口返回的 progress 从头到尾都是 0，然后在收尾瞬间直接跳到 100%。
//
// 修法：详情接口（live=true）额外问一次 ZLM 的 getMediaInfo —— 它对每条 track
// 都给出已推流的 duration（毫秒），实时增长，正是"本片已经拉了多长媒体"。
// 把它加在已完成分片之上，进度就能按轮询粒度刷新。

// runningSegmentHarness 造一个「已建会话、正在录第一片」的任务。
func runningSegmentHarness(t *testing.T, window time.Duration) (*harness, gbmodels.GbRecordCacheTask) {
	t.Helper()
	h := newHarness(t, 4, window)
	task := h.create(t, 0)
	h.service.Tick(context.Background()) // queued → running（建会话 + 开始录制）
	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateRunning, row.State, "Tick 一次后必须进入 running")
	require.NotEmpty(t, row.Stream, "running 的任务必须有正在录的流")
	return h, *row
}

func TestDetailReportsLiveProgressInsideRunningSegment(t *testing.T) {
	h, task := runningSegmentHarness(t, 30*time.Minute)

	// ZLM 上这条流已经推了 10 分钟媒体（音 600000ms / 视 599500ms），
	// 瞬时码率 2 MB/s，流累计 300 MB。
	h.recorder.setMediaInfo(2<<20, 300<<20, 600_000, 599_500)

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateRunning, view.State)

	require.InDelta(t, 600, view.CachedSeconds, 0.001,
		"正在录的这一片必须实时计入已缓存时长 —— 否则进度在一整片（最长 90 分钟媒体）里恒为 0")
	require.InDelta(t, 600.0/1800.0, view.Progress, 0.001)
	require.Equal(t, uint64(300<<20), view.CachedBytes,
		"字节展示也要跟上，否则界面上就是自相矛盾的「1.8 MB/s · 0 B」")
	require.Equal(t, uint64(2<<20), view.SpeedBytesPerSec)
}

func TestDetailAddsLiveProgressOnTopOfFinishedSegments(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background()) // 建第一片会话

	h.fileIndex.add("pb-stream-1", IndexedSegment{
		Name: "seg-1.mp4", Path: segmentPath(1), Size: 100 << 20,
		DurationMS: (25 * time.Minute).Milliseconds(), StartedAt: h.segmentStart, Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background()) // 收尾第一片 → 排下一片
	h.service.Tick(context.Background()) // 建第二片会话

	// 第二片正在录，已经推了 5 分钟媒体。
	h.recorder.setMediaInfo(1<<20, 80<<20, 300_000)

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateRunning, view.State)
	require.InDelta(t, 25*60+5*60, view.CachedSeconds, 0.001,
		"总进度 = 已完成分片时长之和 + 本片实时时长（不是二选一）")
	require.Equal(t, uint64(100<<20+80<<20), view.CachedBytes,
		"字节同理：已落盘分片 + 本片流累计")
}

func TestDetailCapsLiveProgressAtRequestedWindow(t *testing.T) {
	h, task := runningSegmentHarness(t, 30*time.Minute)

	// ZLM 报的时长超过整个请求区间（异常时长 / 重试复用了同一个会话）。
	h.recorder.setMediaInfo(1<<20, 10<<20, float64(90*time.Minute/time.Millisecond))

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, 1.0, view.Progress, "进度不能超过 100%")
	require.InDelta(t, 1800, view.CachedSeconds, 0.001, "已缓存时长封顶在请求区间")
}

func TestListDoesNotQueryMediaServer(t *testing.T) {
	h, _ := runningSegmentHarness(t, 30*time.Minute)
	h.recorder.setMediaInfo(2<<20, 300<<20, 600_000)

	page, err := h.service.List(context.Background(), ListQuery{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, page.List, 1)

	require.Empty(t, h.recorder.mediaInfoQueries(),
		"列表接口不许打 ZLM：N 条任务 = N 次外部调用，会把列表延迟绑在媒体节点上")
	require.Zero(t, page.List[0].Progress, "实时部分只在详情里补；未收尾的分片在列表里就还是 0")
}

func TestDetailSurvivesMediaServerFailure(t *testing.T) {
	h, task := runningSegmentHarness(t, 30*time.Minute)
	h.recorder.mediaErr = errors.New("zlm unreachable")

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err, "速率/进度是展示字段，ZLM 查不到不能让整个详情接口失败")
	require.Zero(t, view.SpeedBytesPerSec)
	require.Zero(t, view.Progress)
	require.Zero(t, view.CachedBytes)
}

// ── 进度只增不减 ──────────────────────────────────────────────────────
//
// ⛔⛔ 这一组是「每次查询进度条先归零、再弹回当前位置」的根因护栏。
//
// 症状：进度明明已经到 45%，下一次轮询却先画 0%（或退回一个更小的值），
// 几百毫秒后再弹回 45% —— 用户原话："应该是递增的才对"。
//
// 根因：实时部分来自 ZLM 的 getMediaInfo，它在流刚建立 / 刚停录、节点抖动、
// 分片交接的空档里会查不到 ⇒ 那一轮实时值归零，于是进度掉回
// 「已收尾分片」那一点 —— 单分片任务那一点就是 0%。
//
// 修法：详情接口给实时进度加一条**水位线**（raiseProgressFloor），只抬不降。
// 前端那层页面内快照只能兜住"同一个页面不刷新"的场景，换了页面 / 重开弹窗
// 就没了，所以真正的判据必须落在服务端。

func TestDetailProgressNeverGoesBackOnMediaServerBlip(t *testing.T) {
	h, task := runningSegmentHarness(t, 30*time.Minute)
	h.recorder.setMediaInfo(2<<20, 300<<20, 600_000)

	first, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.InDelta(t, 600, first.CachedSeconds, 0.001)

	// 下一轮 ZLM 查不到这条流（流刚交接 / 节点抖动）：实时值归零。
	h.recorder.mediaErr = errors.New("zlm blip")
	second, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.InDelta(t, 600, second.CachedSeconds, 0.001,
		"ZLM 那一轮查不到时，进度不能掉回「已收尾分片」那一点（单分片任务就是 0%）")
	require.InDelta(t, first.Progress, second.Progress, 0.0001, "进度只增不减")
	require.Equal(t, first.CachedBytes, second.CachedBytes,
		"已缓存字节同理：界面上不能出现 300 MB → 0 B 再弹回")
}

func TestProgressFloorIsPerTask(t *testing.T) {
	h, first := runningSegmentHarness(t, 30*time.Minute)
	h.recorder.setMediaInfo(1<<20, 100<<20, 600_000)

	viewA, err := h.service.Detail(context.Background(), first.TaskID, nil)
	require.NoError(t, err)
	require.InDelta(t, 600, viewA.CachedSeconds, 0.001)

	// 另一条任务（别的通道，刚起步、一片都还没收尾）：ZLM 同样查不到它。
	other := gbmodels.GbRecordCacheTask{
		TaskID: "task-b", ChannelID: 12, State: gbmodels.RecordCacheStateRunning,
		Stream: "pb-stream-2", StartTime: h.segmentStart, EndTime: h.segmentEnd,
	}
	require.NoError(t, other.EncodeSegments(nil))
	require.NoError(t, h.repo.Create(context.Background(), &other))

	h.recorder.mediaErr = errors.New("zlm blip")
	viewB, err := h.service.Detail(context.Background(), other.TaskID, nil)
	require.NoError(t, err)
	require.Zero(t, viewB.CachedSeconds, "水位必须按任务号分开记：A 已经 600 秒，B 不能蹭到 A 的进度")
}

func TestProgressFloorDroppedWhenTaskLeavesActiveState(t *testing.T) {
	h, task := runningSegmentHarness(t, 30*time.Minute)
	h.recorder.setMediaInfo(1<<20, 100<<20, 600_000)
	_, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)

	h.service.progressMu.Lock()
	_, tracked := h.service.progressFloor[task.TaskID]
	h.service.progressMu.Unlock()
	require.True(t, tracked, "活动态任务的实时进度必须记住水位")

	// 任务跑完：终态的进度由落库的分片清单说了算，水位不该再留着。
	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	row.State = gbmodels.RecordCacheStateSucceeded
	require.NoError(t, h.repo.Update(context.Background(), row))

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, 1.0, view.Progress)

	h.service.progressMu.Lock()
	_, tracked = h.service.progressFloor[task.TaskID]
	h.service.progressMu.Unlock()
	require.False(t, tracked, "终态任务不该再占着水位（也防同一行复用时画出旧进度）")
}

func TestDetailSkipsLiveProbeForFinishedTask(t *testing.T) {
	h := newHarness(t, 4, 30*time.Minute)
	task := h.create(t, 0)
	task.State = gbmodels.RecordCacheStateSucceeded
	task.Stream = "pb-stale" // 故意留一条"看着还在录"的流名
	require.NoError(t, h.repo.Update(context.Background(), &task))

	view, err := h.service.Detail(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, 1.0, view.Progress)
	require.Empty(t, h.recorder.mediaInfoQueries(),
		"终态任务没有在录的片，不必打 ZLM")
}
