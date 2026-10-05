package recordcache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ── fakes ───────────────────────────────────────────────────────────

// fakeLeases 模拟 play.SourceLeaseRegistry：记录登记/释放，并回答 HasLease。
type fakeLeases struct {
	mu       sync.Mutex
	acquired []string
	released []string
	active   map[string]int
}

func newFakeLeases() *fakeLeases {
	return &fakeLeases{active: map[string]int{}}
}

func (f *fakeLeases) Acquire(streamID string, _ uint64, _ string) SourceLease {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquired = append(f.acquired, streamID)
	f.active[streamID]++
	return &fakeLease{registry: f, stream: streamID}
}

// HasLease 让 fakeRecorder 能问「停录这一刻租约还在吗」。
func (f *fakeLeases) HasLease(streamID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[streamID] > 0
}

func (f *fakeLeases) releasedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.released)
}

type fakeLease struct {
	registry *fakeLeases
	stream   string
	once     sync.Once
}

func (l *fakeLease) Release() error {
	l.once.Do(func() {
		l.registry.mu.Lock()
		defer l.registry.mu.Unlock()
		l.registry.released = append(l.registry.released, l.stream)
		if l.registry.active[l.stream] > 0 {
			l.registry.active[l.stream]--
		}
	})
	return nil
}

// fakeFileIndex 模拟 gb_recording_file 的读取（on_record_mp4 hook 写入的真实数据）。
type fakeFileIndex struct {
	mu   sync.Mutex
	rows map[string][]IndexedSegment
}

func newFakeFileIndex() *fakeFileIndex {
	return &fakeFileIndex{rows: map[string][]IndexedSegment{}}
}

func (f *fakeFileIndex) IndexedSegments(_ context.Context, stream string) ([]IndexedSegment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[stream], nil
}

func (f *fakeFileIndex) add(stream string, segment IndexedSegment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[stream] = append(f.rows[stream], segment)
}

// ── 保流租约 ─────────────────────────────────────────────────────────

// ⛔⛔ 这是「缓存失败 / 文件只有一分多钟」的根因护栏。
// ZLM 的 streamNoneReaderDelayMS=20s 会在流没有 reader 时问平台要不要关；
// 录制器在 ZLM 眼里不算 reader，而平台的通道级策略拿 pb- 流名去通道表查不到
// （回放流天然不在通道表）→ 判「关」→ 正在录制的流被拆掉。
func TestStartSegmentAcquiresSourceLease(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateRunning, row.State)
	require.Equal(t, []string{row.Stream}, h.leases.acquired,
		"会话建立后必须立刻登记保流租约，否则 20 秒后流会被判无人观看回收")
}

func TestFinishSegmentKeepsLeaseUntilRecordingStops(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	h.create(t, 0)

	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 100, 10*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	require.True(t, h.recorder.leaseHeldAtStop,
		"必须**先停录、后放租约**：反过来的话流会在文件封口前被 ZLM 回收，最后一片永远不完整")
	require.Equal(t, 1, h.leases.releasedCount())
}

func TestFailReleasesSourceLease(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())
	// 本片一个文件都没落盘 → finishSegment 判失败。
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateFailed, row.State)
	require.Equal(t, 1, h.leases.releasedCount(),
		"失败也必须放租约，否则会留下一条没人认领的保活流，永久占着设备通道")
}

func TestCloseReleasesHeldLeases(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	h.create(t, 0)
	h.service.Tick(context.Background())
	require.Equal(t, 0, h.leases.releasedCount())

	h.service.Close()

	require.Equal(t, 1, h.leases.releasedCount(), "卸载运行时必须放干净")
}

// 缓存任务与「用户在回放页开的会话」必须分属不同槽位，
// 否则同一通道上「边看回放边缓存」必然吃 429。
func TestCacheOwnerDoesNotCollideWithUserPlaybackScope(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	h.create(t, 0)
	h.service.Tick(context.Background())

	require.Len(t, h.playback.created, 1)
	require.Equal(t, cacheOwnerNamespace+"9", h.playback.created[0].OwnerID)
	require.NotEqual(t, "9", h.playback.created[0].OwnerID)
}

// ── 真实时长 ─────────────────────────────────────────────────────────

// ZLM 的 getMp4RecordFile 不回时长/大小（DurationMS/FileSize 恒 nil，既有契约），
// 只有入库记录才有真实值。拿不到真实时长就会退化成「按请求区间推进」——
// 把「实际只录到 84 秒」当成「录满 20 分钟」宣布成功，用户一个文件都拿不到。
func TestCollectSegmentFilesPrefersIndexedMetadata(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())

	// 故意让 ZLM 那条路什么都给不出（size/ms 全 0）。
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 0, 0)
	h.fileIndex.add("pb-stream-1", IndexedSegment{
		Name: "seg-1.mp4", Path: "/opt/media/bin/www/recordings/record/rtp/pb-stream-1/2026-10-03/seg-1.mp4",
		Size: 33718507, DurationMS: 83841, StartedAt: h.segmentStart, Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	segments := row.DecodeSegments()
	require.Len(t, segments, 1)
	require.Equal(t, int64(83841), segments[0].MS, "时长必须取入库的真实值")
	require.Equal(t, int64(33718507), segments[0].Size)
	require.Equal(t, uint64(33718507), row.FileSize)
	require.NotNil(t, row.CursorAt)
	require.Equal(t, h.segmentStart.Add(83841*time.Millisecond), row.CursorAt.UTC(),
		"游标必须按**真实**媒体时长推进，否则会把 84 秒当成 20 分钟")
}

// 入库还没追上时（hook 延迟）退回 ZLM 的文件列表：文件仍然可下载，
// 但没有时长 —— 此时按请求区间保守推进，任务状态不会被误判成"已完成"。
func TestCollectSegmentFilesFallsBackToZLMList(t *testing.T) {
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())

	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 1024, 0)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	segments := row.DecodeSegments()
	require.Len(t, segments, 1)
	require.Equal(t, "seg-1.mp4", segments[0].Name, "兜底路径至少要保住文件名，下载才可用")
	require.Equal(t, int64(1024), segments[0].Size)
	require.Equal(t, int64(0), segments[0].MS)
}
