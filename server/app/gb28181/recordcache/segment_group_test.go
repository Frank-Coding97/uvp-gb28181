package recordcache

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// ── 片 = 内容区间，补拉并进同一片 ─────────────────────────────────────
//
// 背景：设备在 download 模式下按**墙钟**推流（请求 R 秒媒体 @N 倍速 ⇒ 推 R/N 秒
// 就停），真实有效倍速总略低于标称，于是每次都少推 2%~4% 的尾部；平台按实际录到的
// 媒体时长推进游标、再开会话补拉。补拉文件是同一段内容的延续，必须并进同一片。

// TestBackfillFoldsIntoOneSegment 用的是 2026-10-05 的真实数据（老板在界面
// 上看到"三个片段"的那一次）：
//
//	30 分钟录像 @4× → 设备推 1747.69s 就静默 → 平台补 51.04s → 再补 1.04s
//
// 改造后必须只有**一片**（用户只看到一个文件），三个源文件在收尾期合成一个产物。
func TestBackfillFoldsIntoOneSegment(t *testing.T) {
	h := newHarness(t, 4, 30*time.Minute)
	partA, partB, partC := videoFragment(8), videoFragment(3), videoFragment(1)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	h.recorder.serve(segmentPath(3), partC)
	task := h.create(t, 0)

	backfills := []struct {
		stream  string
		name    string
		path    string
		size    int64
		mediaMS int64
	}{
		{"pb-stream-1", "seg-1.mp4", segmentPath(1), int64(len(partA)), 1747690},
		{"pb-stream-2", "seg-2.mp4", segmentPath(2), int64(len(partB)), 51040},
		{"pb-stream-3", "seg-3.mp4", segmentPath(3), int64(len(partC)), 1040},
	}
	for i, backfill := range backfills {
		h.service.Tick(context.Background()) // 建本次会话
		require.Len(t, h.playback.created, i+1, "每一段剩余区间都要真的续播")
		h.fileIndex.add(backfill.stream, IndexedSegment{
			Name: backfill.name, Path: backfill.path, Size: backfill.size,
			DurationMS: backfill.mediaMS, StartedAt: h.segmentStart, Period: "2026-10-03",
		})
		h.playback.dropSessions()
		h.service.Tick(context.Background()) // 收尾本次会话 → 并入当前片
	}

	// 三次补拉之后还差 0.23 秒（1799.77s / 1800s），平台会再开一次极短会话；
	// 设备对这种几十毫秒的区间什么都不推。这时**不能**判失败 ——
	// 内容已经录到 99.99%，报失败等于把拿到的录像说成"没有录像"。
	h.service.Tick(context.Background())
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State,
		"最后那点尾巴推不出来也必须成功收尾")
	segments := row.DecodeSegments()
	require.Len(t, segments, 1,
		"补拉是同一段内容的延续：30 分钟录像必须只有一片（用户只该看到一个文件）")
	require.Len(t, segments[0].Sources(), 3, "三个源文件都要留在同一片里")
	require.Equal(t, int64(1799770), segments[0].MS, "片级时长是三个源文件之和")
	require.Equal(t, int64(1799770), row.CachedMediaDuration().Milliseconds())
	require.Equal(t, h.segmentStart.Format(time.RFC3339), segments[0].Start,
		"片起点锚在第一次会话的起点上，不随补拉漂移")

	path, size, ok := h.service.mergedArtifact(&row)
	require.True(t, ok, "多个源文件必须在收尾期合成为一个产物")
	require.Contains(t, path, task.TaskID)
	require.Greater(t, size, int64(0))

	h.recorder.downloadHits = nil
	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, ""))
	require.Empty(t, h.recorder.downloadHits,
		"产物已就绪，下载不该再回媒体节点拉源文件")
}

// TestLongRecordingStillSplitsAtSegmentLimit 兜住另一半：补拉并进同一片**不等于**
// 永远只有一片。录像比单片上限（4× 设备 = 90 分钟媒体）长时仍要按区间切片，
// 否则单文件会失控（几 GB 一个文件、还可能超出自动合并上限）。
func TestLongRecordingStillSplitsAtSegmentLimit(t *testing.T) {
	h := newHarness(t, 4, 180*time.Minute)
	partA, partB := videoFragment(6), videoFragment(6)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := h.create(t, 0)

	for i, stream := range []string{"pb-stream-1", "pb-stream-2"} {
		h.service.Tick(context.Background())
		require.Len(t, h.playback.created, i+1)
		h.fileIndex.add(stream, IndexedSegment{
			Name: "seg-" + stream[len(stream)-1:] + ".mp4", Path: segmentPath(i + 1),
			Size: int64(len(partA)), DurationMS: (90 * time.Minute).Milliseconds(),
			StartedAt: h.segmentStart, Period: "2026-10-03",
		})
		h.playback.dropSessions()
		h.service.Tick(context.Background())
	}

	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State)
	segments := row.DecodeSegments()
	require.Len(t, segments, 2, "180 分钟录像在 90 分钟上限下必须切成两片")
	require.Len(t, segments[0].Sources(), 1)
	require.Len(t, segments[1].Sources(), 1)
	require.Equal(t, h.segmentStart.Add(90*time.Minute).Format(time.RFC3339), segments[1].Start,
		"第二片的起点就是第一片的终点（无缝隙、无重叠）")
}

// TestSegmentDownloadMergesMultipleSourcesOfOneSegment 钉住按段下载在
// "一片多个源文件"下的行为：没有单一文件可以透传，只能现场把这几个文件合起来。
func TestSegmentDownloadMergesMultipleSourcesOfOneSegment(t *testing.T) {
	h := newHarness(t, 4, 30*time.Minute)
	partA, partB := videoFragment(5), videoFragment(4)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := h.create(t, 0)

	h.service.Tick(context.Background())
	h.fileIndex.add("pb-stream-1", IndexedSegment{
		Name: "seg-1.mp4", Path: segmentPath(1), Size: int64(len(partA)),
		DurationMS: (20 * time.Minute).Milliseconds(), StartedAt: h.segmentStart, Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	h.service.Tick(context.Background())
	h.fileIndex.add("pb-stream-2", IndexedSegment{
		Name: "seg-2.mp4", Path: segmentPath(2), Size: int64(len(partB)),
		DurationMS: (10 * time.Minute).Milliseconds(), StartedAt: h.segmentStart, Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	row := h.waitMerge(t, task.TaskID)
	require.Len(t, row.DecodeSegments(), 1)
	require.Len(t, row.DecodeSegments()[0].Sources(), 2)

	h.recorder.downloadHits = nil
	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, 0, ""))
	require.Equal(t, []string{segmentPath(1), segmentPath(2)}, h.recorder.downloadHits,
		"片内多个源文件时必须按顺序全部拉回来合并")
	require.Equal(t, uint32(9), stszSampleCount(t, rec.Body.Bytes()),
		"发出去的必须是两个源文件合起来的完整文件（5+4 个样本）")
}

// TestBackfillSessionsUseDistinctIdempotencyKeys 钉住补拉会话的幂等键口径。
//
// ⛔ 它必须随**本段起点**变化，不能再用分片序号：补拉并进同一片之后，序号在
// 整个补拉期间根本不变 —— 两段拿到同一个键，回放 registry 会把第二次补拉判成
// "已有会话"、直接返回上一段那个**已经拆掉**的会话，任务当场停摆。
func TestBackfillSessionsUseDistinctIdempotencyKeys(t *testing.T) {
	h := newHarness(t, 4, 30*time.Minute)
	task := h.create(t, 0)

	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 100, 20*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-2", "seg-2.mp4", 100, 5*60*1000)
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	require.Len(t, h.playback.created, 2)
	first := h.playback.created[0].IdempotencyKey
	second := h.playback.created[1].IdempotencyKey
	require.NotEqual(t, first, second, "两段补拉必须用不同的幂等键")
	require.Equal(t, fmt.Sprintf("%s-%d", task.RequestID, h.segmentStart.UnixMilli()), first,
		"幂等键 = 任务号 + 本段起点（同一段重试复用，不同段必须不同）")
	require.Equal(t, fmt.Sprintf("%s-%d", task.RequestID, h.segmentStart.Add(20*time.Minute).UnixMilli()), second)
	require.Equal(t, 1, rowSegmentCount(t, h, task.TaskID),
		"两次会话录的是同一段内容的延续，清单里始终只有一片")
}

func rowSegmentCount(t *testing.T, h *harness, taskID string) int {
	t.Helper()
	row, err := h.repo.FindByTaskID(context.Background(), taskID, nil)
	require.NoError(t, err)
	return len(row.DecodeSegments())
}

// TestLegacySegmentWithoutPartsStillServesSingleFile 钉住向后兼容：
// 改造前落库的分片清单没有 parts，只有片级三元组 —— 必须回退成"单文件一片"，
// 否则历史任务会瞬间变成"暂无可下载的文件"（文件明明还躺在媒体节点上）。
func TestLegacySegmentWithoutPartsStillServesSingleFile(t *testing.T) {
	h := newHarness(t, 4, 40*time.Minute)
	task := h.create(t, 0)
	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	row.Segments = `[{"stream":"pb-1","name":"legacy.mp4","path":"/rec/pb-1/legacy.mp4",` +
		`"size":123,"ms":60000,"start":"2026-10-03T06:00:00Z","end":"2026-10-03T06:01:00Z","period":"2026-10-03"}]`
	row.State = gbmodels.RecordCacheStateSucceeded
	row.NodeID = 1 // 老任务早就记下过落点节点，下载要靠它定位媒体节点
	require.NoError(t, h.repo.Update(context.Background(), row))

	require.Equal(t, 1, row.SourceFileCount(), "老数据必须回退成『单文件一片』")
	require.True(t, downloadableSegment(row.DecodeSegments()[0]))

	require.NoError(t, h.service.Content(context.Background(), nil, task.TaskID, nil, 0, "bytes=0-9"))
	require.Equal(t, "legacy.mp4", h.content.last.FileName)
	require.Equal(t, "/rec/pb-1/legacy.mp4", h.content.last.FilePath)
	require.Equal(t, "bytes=0-9", h.content.last.Range)

	// 单片单文件不触发任何合并动作（否则每次下载都要在后端多写一份等大的临时文件）。
	require.Empty(t, h.recorder.downloadHits)
}

// TestAppendSegmentFilesFoldsUntilLimit 把分片记账的两条判据钉成纯函数用例：
// 未到上限就并入当前片；到上限才另起一片，且新片的起点 = 这次会话的起点。
func TestAppendSegmentFilesFoldsUntilLimit(t *testing.T) {
	limit := 90 * time.Minute
	origin := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	file := func(stream, name string, ms int64) gbmodels.RecordCacheSegment {
		return gbmodels.RecordCacheSegment{Stream: stream, Name: name, Path: "p/" + name, Size: 10, MS: ms}
	}

	segments := appendSegmentFiles(nil, []gbmodels.RecordCacheSegment{
		file("s1", "a.mp4", 29*60*1000),
	}, origin, limit)
	require.Len(t, segments, 1)
	require.Equal(t, origin.Format(time.RFC3339), segments[0].Start)

	// 补拉：未到上限 ⇒ 并进当前片，而不是新开一片。
	segments = appendSegmentFiles(segments, []gbmodels.RecordCacheSegment{
		file("s2", "b.mp4", 60*1000),
	}, origin.Add(29*time.Minute), limit)
	require.Len(t, segments, 1, "未到单片上限时，补拉必须并进当前片")
	require.Len(t, segments[0].Sources(), 2)
	require.Equal(t, int64(30*60*1000), segments[0].MS)
	require.Equal(t, int64(20), segments[0].Size, "片级大小是各源文件之和（不是重复累加）")
	require.Equal(t, "a.mp4", segments[0].Name, "片级文件标识取第一个源文件，保持老读法")
	require.Equal(t, origin.Add(30*time.Minute).Format(time.RFC3339), segments[0].End)

	// 当前片已经装到上限 ⇒ 这一批自成一片。
	segments = appendSegmentFiles(segments, []gbmodels.RecordCacheSegment{
		file("s3", "c.mp4", 60*1000),
	}, origin.Add(limit), limit)
	require.Len(t, segments, 2, "当前片已满时必须另起一片")
	require.Equal(t, origin.Add(limit).Format(time.RFC3339), segments[1].Start,
		"新片的起点就是这次会话的起点")
	require.Len(t, segments[1].Sources(), 1)

	// 空批次不改清单（收尾阶段可能什么都采不到）。
	require.Len(t, appendSegmentFiles(segments, nil, origin.Add(limit+time.Minute), limit), 2)
}

// TestSegmentCapEndNeverGoesBackwards 是 startSegment 夹取会话区间的前提：
// 无论当前片在什么状态，cap 都必须**严格晚于**会话起点，否则会当场误判成
// "没有剩余区间了"、把任务提前判定成功。
func TestSegmentCapEndNeverGoesBackwards(t *testing.T) {
	limit := 90 * time.Minute
	origin := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	segments := []gbmodels.RecordCacheSegment{{Start: origin.Format(time.RFC3339), Path: "p/a.mp4"}}

	for _, offset := range []time.Duration{0, time.Minute, limit - time.Minute, limit, 2 * limit} {
		segStart := origin.Add(offset)
		require.True(t, segmentCapEnd(segments, segStart, limit).After(segStart),
			"偏移 %s：会话上限必须晚于起点", offset)
	}
	require.True(t, segmentCapEnd(nil, origin, limit).After(origin), "还没有任何片时用完整上限")
}
