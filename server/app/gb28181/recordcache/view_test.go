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
