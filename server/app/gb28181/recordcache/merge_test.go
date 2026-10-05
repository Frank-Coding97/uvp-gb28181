package recordcache

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/mp4join/mp4fixture"
	gbrecording "uvplatform.cn/uvp-gb28181/app/gb28181/recording"
)

// ── 素材与夹具 ────────────────────────────────────────────────────────
//
// ⛔ 为什么这一层也要造真 MP4：mp4join 的单测证明的是"box 拼得对"，
// 证明不了"Content 在多分片任务上真的走合并分支、按清单逐片拉、发一个文件出去"。
// 用半个字节的假文件测，就只能测到错误分支。

func segmentPath(index int) string {
	return fmt.Sprintf("/rec/pb-stream-%d/seg-%d.mp4", index, index)
}

// 这一段是「真的会切成两片」的录像长度。
//
// ⛔ 补拉收进同一片之后，60 分钟以内的窗口**构造不出多片**了：4× 设备的单片上限
// 是 90 分钟媒体，30/60 分钟录像无论补拉几次都只会有 1 片（多个源文件）。
// 要继续测"跨片"这条路，必须让录像比单片上限长。
const (
	longWindow = 120 * time.Minute // 录像总长
	longFirst  = 90 * time.Minute  // 第一片装到单片上限（= segmentMediaLimit(4)）
	longSecond = 30 * time.Minute  // 剩下的一截自成第二片
)

// runTwoSegmentTask 用真实状态机把任务推到「已完成、两个分片」。
//
// ⛔ 必须走 Tick 而不是手写一行库记录：手写的那行绕过了分片清单编码
// （`segments` 列），而"按清单逐片拉"正是这里要钉住的契约。
func runTwoSegmentTask(t *testing.T, h *harness, first, second time.Duration, sizeA, sizeB int64) gbmodels.GbRecordCacheTask {
	t.Helper()
	task := h.create(t, 0)

	h.service.Tick(context.Background()) // 建第一片会话
	h.fileIndex.add("pb-stream-1", IndexedSegment{
		Name: "seg-1.mp4", Path: segmentPath(1), Size: sizeA,
		DurationMS: first.Milliseconds(), StartedAt: h.segmentStart, Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background()) // 收尾第一片 → 排下一片
	h.service.Tick(context.Background()) // 建第二片会话

	h.fileIndex.add("pb-stream-2", IndexedSegment{
		Name: "seg-2.mp4", Path: segmentPath(2), Size: sizeB,
		DurationMS: second.Milliseconds(), StartedAt: h.segmentStart.Add(first), Period: "2026-10-03",
	})
	h.playback.dropSessions()
	h.service.Tick(context.Background()) // 收尾第二片 → merging

	// ⛔ 多片收尾后会先转入「整理中」，由后台把各片合成一个文件再判定成功。
	// 不等这一步就会读到中间态 merging。
	row := h.waitMerge(t, task.TaskID)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State,
		"两片之和覆盖整个请求区间时必须收敛到成功")
	require.Len(t, row.DecodeSegments(), 2)
	return row
}

// topLevelBoxes 扫顶层 box，返回 类型 → 各实例的载荷。
//
// ⛔ 刻意不借用 mp4join 的解析器：用被测代码验证被测代码等于什么都没验证。
func topLevelBoxes(t *testing.T, data []byte) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		typ := string(data[off+4 : off+8])
		if size < 8 || off+size > len(data) {
			t.Fatalf("顶层 box %q 尺寸异常（size=%d, off=%d, len=%d）", typ, size, off, len(data))
		}
		out[typ] = append(out[typ], data[off+8:off+size])
		off += size
	}
	return out
}

func boxPayload(t *testing.T, data []byte, typ string) []byte {
	t.Helper()
	boxes := topLevelBoxes(t, data)
	require.Len(t, boxes[typ], 1, "期望恰好一个 %s", typ)
	return boxes[typ][0]
}

// stszSampleCount 读出被合并 moov 里唯一的 stsz 的样本数。
func stszSampleCount(t *testing.T, data []byte) uint32 {
	t.Helper()
	require.Equal(t, 1, bytes.Count(data, []byte("stsz")), "单轨素材应只有一个 stsz")
	idx := bytes.Index(data, []byte("stsz"))
	// box: size(4) type(4) version+flags(4) sample_size(4) sample_count(4)
	return binary.BigEndian.Uint32(data[idx+12 : idx+16])
}

func videoFragment(samples uint32) []byte {
	return mp4fixture.Fragment(mp4fixture.Spec{Handlers: []string{"vide"}, Tag: "hvc1", Samples: samples})
}

// ── 主路径：多分片 → 一个文件 ─────────────────────────────────────────

func TestContentMergesMultipleSegmentsIntoOneFile(t *testing.T) {
	h := newHarness(t, 4, longWindow)
	partA, partB := videoFragment(8), videoFragment(5)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := runTwoSegmentTask(t, h, longFirst, longSecond,
		int64(len(partA)), int64(len(partB)))

	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, ""))

	res := rec.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "video/mp4", res.Header.Get("Content-Type"))
	require.Contains(t, res.Header.Get("Content-Disposition"), ".mp4",
		"必须带文件名，浏览器才会直接落成文件而不是打开播放器")
	require.Equal(t, []string{segmentPath(1), segmentPath(2)}, h.recorder.downloadHits,
		"必须按分片清单顺序逐片拉取：顺序错了时间轴就断了")

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(len(body)), res.Header.Get("Content-Length"),
		"Content-Length 必须等于实际发出的字节数，否则浏览器下载会截断/卡住")

	boxes := topLevelBoxes(t, body)
	require.Contains(t, boxes, "ftyp")
	require.Contains(t, boxes, "moov")
	require.Len(t, boxes["mdat"], 1, "合并结果只能有一个 mdat")
	// ⛔ 无损的硬约束：输出 mdat 数据区 = 各片数据区依次相连。
	require.Equal(t,
		mp4fixture.Concat(boxPayload(t, partA, "mdat"), boxPayload(t, partB, "mdat")),
		boxes["mdat"][0])
	// ⛔ 样本数必须是两片之和：若等于第一片，说明 moov 没被重建，
	// 文件"能播"但只有前一片的内容 —— 这正是最容易被漏掉的一类错。
	require.Equal(t, uint32(13), stszSampleCount(t, body))
}

// ── 按段下载：多分片任务上给了序号就必须发那一片 ───────────────────────

// TestContentServesRequestedSegmentOfMultiSegmentedTask 钉住被修掉的旧行为。
//
// ⛔ 以前多分片分支**无条件忽略**序号发合并产物：前端「第 2 段」点下去拿到的是整段
// 录像 —— 界面在说谎；用户看到"怎么这么大"以为失败，再点一次，每次都拉一遍整段。
func TestContentServesRequestedSegmentOfMultiSegmentedTask(t *testing.T) {
	h := newHarness(t, 4, longWindow)
	partA, partB := videoFragment(8), videoFragment(5)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := runTwoSegmentTask(t, h, longFirst, longSecond,
		int64(len(partA)), int64(len(partB)))
	require.True(t, len(task.DecodeSegments()) > 1, "这个用例的前提是多分片任务")
	h.recorder.downloadHits = nil

	recorder := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), recorder, task.TaskID, nil, 1, "bytes=0-9"))

	require.Equal(t, segmentPath(2), h.content.last.FilePath,
		"给序号就直接发那一片，不能绕去合并产物")
	require.Equal(t, gbrecording.CapabilityModeDownload, h.content.last.Mode)
	require.Equal(t, "bytes=0-9", h.content.last.Range)
	require.Empty(t, h.recorder.downloadHits,
		"按段下载不该有任何拉取/合并动作 —— 那一片本来就在节点上")
}

// ── 降级路径：合不出来也要让用户拿到东西 ──────────────────────────────

func TestContentMergeFailureStaysDegradableAndKeepsTaskIntact(t *testing.T) {
	h := newHarness(t, 4, longWindow)
	good := videoFragment(8)
	h.recorder.serve(segmentPath(1), good)
	// 第二片只拉到半截（真实故障：拉取途中媒体节点重启 / 文件还在写入）。
	h.recorder.serve(segmentPath(2), good[:len(good)/2])
	task := runTwoSegmentTask(t, h, longFirst, longSecond,
		int64(len(good)), int64(len(good)))

	rec := httptest.NewRecorder()
	err := h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, "")
	require.ErrorIs(t, err, ErrMergeFailed,
		"分片本身是好的、只是合不成一个 ⇒ 必须报成可识别的『请按分段下载』，不能报成 404『暂无可下载的文件』")
	require.Empty(t, rec.Body.Bytes(), "失败时不能已经把半个文件发出去了")

	row, err := h.repo.FindByTaskID(context.Background(), task.TaskID, nil)
	require.NoError(t, err)
	require.Equal(t, gbmodels.RecordCacheStateSucceeded, row.State,
		"下载环节失败绝不能把已经跑好的任务标成 failed")
	require.Len(t, row.DecodeSegments(), 2,
		"分片清单必须原样保留 —— 它才是用户按分段下载的唯一依据")
}

func TestContentRejectsMergeAboveSizeLimit(t *testing.T) {
	h := newHarness(t, 4, longWindow)
	half := mergeTotalSizeLimit/2 + 1
	task := runTwoSegmentTask(t, h, longFirst, longSecond, half, half)

	rec := httptest.NewRecorder()
	err := h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, "")
	require.ErrorIs(t, err, ErrMergeFailed)
	require.Contains(t, err.Error(), "上限")
	require.Empty(t, h.recorder.downloadHits,
		"超限必须在发起任何下载之前就拒掉：先把 4GiB 拉到本地再报错等于把后端磁盘写满")
}

// TestContentServesPreMergedArtifactWithoutReMerging 是这次改造的核心验收：
// 产物在收尾期就备好了，下载**一次都不回媒体节点拉分片** ——
// 这正是"点下载立刻开始、进度是真实网速"的来源。
func TestContentServesPreMergedArtifactWithoutReMerging(t *testing.T) {
	h := newHarness(t, 4, longWindow)
	partA, partB := videoFragment(4), videoFragment(4)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := runTwoSegmentTask(t, h, longFirst, longSecond,
		int64(len(partA)), int64(len(partB)))

	path, size, ok := h.service.mergedArtifact(&task)
	require.True(t, ok, "收尾整理必须留下一个就绪的合成产物")
	require.Contains(t, path, task.TaskID, "产物按任务号命名，删除/保留期清理才找得回来")
	// 合并会去掉后续分片的文件头尾，所以产物略小于"各片之和"，但不可能更大。
	require.Greater(t, size, int64(0))
	require.LessOrEqual(t, size, int64(len(partA)+len(partB)))

	// 收尾整理已经拉过一遍分片；从这里开始计数，下载阶段必须一次都不再拉。
	h.recorder.downloadHits = nil
	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, ""))

	require.Empty(t, h.recorder.downloadHits,
		"下载必须直接发收尾期已合成好的文件；再回媒体节点拉分片就等于没做这次改造")
	require.Equal(t, http.StatusOK, rec.Result().StatusCode)
	require.Equal(t, uint32(8), stszSampleCount(t, rec.Body.Bytes()),
		"发出去的必须是两片合起来的完整文件（4+4 个样本），不是只有第一片")
}

// TestStreamMergedFallbackRemovesTempDirectory 钉住兜底路径的资源纪律：
// 产物缺失（系统清了临时目录 / 上次合并失败）时才现场合并，且临时目录用完即删。
func TestStreamMergedFallbackRemovesTempDirectory(t *testing.T) {
	before := listMergeTempDirs(t)
	h := newHarness(t, 4, longWindow)
	partA, partB := videoFragment(4), videoFragment(4)
	h.recorder.serve(segmentPath(1), partA)
	h.recorder.serve(segmentPath(2), partB)
	task := runTwoSegmentTask(t, h, longFirst, longSecond,
		int64(len(partA)), int64(len(partB)))

	// 制造"产物没了"：这正是重启后系统清理临时目录会发生的场景。
	h.service.removeMergedArtifact(task.TaskID)
	h.recorder.downloadHits = nil

	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, ""))
	require.Equal(t, []string{segmentPath(1), segmentPath(2)}, h.recorder.downloadHits,
		"产物缺失时必须退回现场合并，而不是告诉用户没有文件")

	require.Equal(t, before, listMergeTempDirs(t),
		"临时目录必须用完即删：后端没有对象存储，残留一次就是永久占着磁盘（每份都是几十上百 MB）")
}

func listMergeTempDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), mergeTempPattern) {
			out = append(out, entry.Name())
		}
	}
	return out
}

// ── 单片仍走同源透传 ─────────────────────────────────────────────────

func TestContentSingleSegmentDoesNotDownloadToBackend(t *testing.T) {
	// 单片必须在媒体节点上直接透传（含 Range），不经后端落地 ——
	// 否则每次下载都要在后端多写一份等大的临时文件。
	h := newHarness(t, 4, 60*time.Minute)
	task := h.create(t, 0)
	h.service.Tick(context.Background())
	h.recorder.addFile("pb-stream-1", "seg-1.mp4", 10, 60*time.Minute.Milliseconds())
	h.playback.dropSessions()
	h.service.Tick(context.Background())

	rec := httptest.NewRecorder()
	require.NoError(t, h.service.Content(context.Background(), rec, task.TaskID, nil, WholeTaskIndex, "bytes=0-9"))
	require.Equal(t, "seg-1.mp4", h.content.last.FileName)
	require.Equal(t, "bytes=0-9", h.content.last.Range)
	require.Empty(t, h.recorder.downloadHits, "单片不应触发后端合并下载")
}

// ── 纯函数：Range 与文件名 ───────────────────────────────────────────

func TestParseRangeHeader(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		size       int64
		start, end int64
		ok         bool
	}{
		{"区间", "bytes=0-99", 100, 0, 99, true},
		{"省略结束", "bytes=90-", 100, 90, 99, true},
		{"末 N 字节", "bytes=-10", 100, 90, 99, true},
		{"末 N 超过文件", "bytes=-1000", 100, 0, 99, true},
		{"结束越界夹回", "bytes=90-1000", 100, 90, 99, true},
		{"带空格", "bytes= 10 - 20 ", 100, 10, 20, true},
		{"起点越界", "bytes=100-", 100, 0, 0, false},
		{"结束小于起点", "bytes=50-10", 100, 0, 0, false},
		{"多区间不支持", "bytes=0-9,20-29", 100, 0, 0, false},
		{"非 bytes 单位", "items=0-9", 100, 0, 0, false},
		{"空值", "", 100, 0, 0, false},
		{"零长文件", "bytes=0-0", 0, 0, 0, false},
		{"缺横线", "bytes=10", 100, 0, 0, false},
		{"末 0 字节", "bytes=-0", 100, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := parseRangeHeader(tc.value, tc.size)
			require.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			require.Equal(t, tc.start, start)
			require.Equal(t, tc.end, end)
		})
	}
}

func TestServeLocalFileRangeAndUnsatisfiable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "merged.mp4")
	content := []byte("0123456789")
	require.NoError(t, os.WriteFile(path, content, 0o644))

	rec := httptest.NewRecorder()
	require.NoError(t, serveLocalFile(rec, path, "a.mp4", ""))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "10", rec.Header().Get("Content-Length"))
	require.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "a.mp4")
	require.Equal(t, content, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	require.NoError(t, serveLocalFile(rec, path, "a.mp4", "bytes=2-5"))
	require.Equal(t, http.StatusPartialContent, rec.Code)
	require.Equal(t, "bytes 2-5/10", rec.Header().Get("Content-Range"))
	require.Equal(t, "4", rec.Header().Get("Content-Length"))
	require.Equal(t, []byte("2345"), rec.Body.Bytes())

	rec = httptest.NewRecorder()
	require.NoError(t, serveLocalFile(rec, path, "a.mp4", "bytes=-3"))
	require.Equal(t, http.StatusPartialContent, rec.Code)
	require.Equal(t, []byte("789"), rec.Body.Bytes())

	rec = httptest.NewRecorder()
	require.NoError(t, serveLocalFile(rec, path, "a.mp4", "bytes=99-"))
	require.Equal(t, http.StatusRequestedRangeNotSatisfiable, rec.Code)
	require.Equal(t, "bytes */10", rec.Header().Get("Content-Range"),
		"416 必须带 bytes */size，播放器据此知道文件真实长度")
	require.Empty(t, rec.Body.Bytes())

	rec = httptest.NewRecorder()
	require.ErrorIs(t, serveLocalFile(rec, filepath.Join(dir, "missing.mp4"), "a.mp4", ""), ErrNotDownloadable)
}

func TestMergedFileName(t *testing.T) {
	require.Equal(t, "record-cache-01234567.mp4",
		mergedFileName(&gbmodels.GbRecordCacheTask{TaskID: "01234567-89ab-cdef-0123-456789abcdef"}),
		"用任务号前 8 位做文件名：用户同时下多个录像时能对上号")
	require.Equal(t, "record-cache-abc.mp4",
		mergedFileName(&gbmodels.GbRecordCacheTask{TaskID: "abc"}))
	require.Equal(t, "record-cache-recording.mp4",
		mergedFileName(&gbmodels.GbRecordCacheTask{TaskID: "   "}),
		"任务号缺失也要给出一个合法文件名，不能发出 \"\" 或 \".mp4\"")
}

func TestTotalSegmentSize(t *testing.T) {
	require.Equal(t, int64(0), totalSegmentSize(nil))
	require.Equal(t, int64(30), totalSegmentSize([]gbmodels.RecordCacheSegment{{Size: 10}, {Size: 20}}))
}
