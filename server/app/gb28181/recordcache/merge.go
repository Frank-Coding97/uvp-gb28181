package recordcache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/mp4join"
)

// mergeTotalSizeLimit 是「后端临时合并」的分片总大小上限。
//
// 合并必须先把全部分片拉到后端，磁盘占用 ≈ 总大小。到这个量级（4 GiB）
// 就不再自动合并，让前端退回"按分段下载" —— 把后端磁盘写满，
// 比多给用户几个文件是更大的事故。
const mergeTotalSizeLimit = int64(4) << 30

// mergeTempPattern 是临时目录名前缀（测试用它断言"用完即删"）。
const mergeTempPattern = "uvp-record-cache-merge-"

// streamMerged 是**兜底路径**：没有已就绪的产物时，现场拉到后端临时
// 目录、无损合并成一个 MP4 再发送。传进来的 segments 可以是全部分片
// （整段兜底），也可以是单一片（片内含多次补拉、按段下载时用）。
//
// 正常情况下这条路径不该被走到 —— 多源文件的任务在收尾时就把产物拼好了
// （见 merged.go），下载直接发那个文件，用户点完立刻开始、进度是真实网速。
// 只有产物缺失才会落到这里：进程重启后系统清了临时目录、上次合并失败、
// 或产物被运维手工删掉。
//
// ⛔ 为什么必须"先落地再合并"：MP4 的 moov（索引）在文件尾部，合并要读各分片的
// sample 表并重算 chunk 偏移，没法边拉边合。
func (s *Service) streamMerged(ctx context.Context, writer http.ResponseWriter, task *gbmodels.GbRecordCacheTask, segments []gbmodels.RecordCacheSegment, rangeHeader string) error {
	recorder, err := s.recorderFor(task.NodeID)
	if err != nil {
		return err
	}
	if total := totalSegmentSize(segments); total > mergeTotalSizeLimit {
		return fmt.Errorf("%w: 分段合计 %d 字节超过自动合并上限，请按分段下载", ErrMergeFailed, total)
	}
	dir, err := os.MkdirTemp("", mergeTempPattern)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMergeFailed, err)
	}
	defer os.RemoveAll(dir)

	merged := filepath.Join(dir, "merged.mp4")
	if err := s.fetchAndConcat(ctx, recorder, sourcesOf(segments), dir, merged); err != nil {
		return err
	}
	return serveLocalFile(writer, merged, mergedFileName(task), rangeHeader)
}

// fetchAndConcat 按顺序把每个源文件拉到 workDir，再无损拼接到 dest。
//
// 收尾期的产物生成（merged.go）与下载兜底的现场合并共用这一段：两条路必须
// 逐字节同源，否则"产物"和"兜底结果"会悄悄不一致，出问题无从比对。
//
// ⛔ 入参是**源文件路径**（片内多文件展平后），不是分片：一片可能对应好几个文件，
// 按片循环会只拉每片的第一个文件、把其余内容静默丢掉。
func (s *Service) fetchAndConcat(ctx context.Context, recorder Recorder, sources []string, workDir, dest string) error {
	if len(sources) == 0 {
		return ErrNotDownloadable
	}
	paths := make([]string, 0, len(sources))
	for i, remote := range sources {
		if strings.TrimSpace(remote) == "" {
			return ErrNotDownloadable
		}
		local := filepath.Join(workDir, fmt.Sprintf("part-%03d.mp4", i))
		if err := s.fetchSegment(ctx, recorder, remote, local); err != nil {
			return err
		}
		paths = append(paths, local)
	}
	if err := mp4join.Concat(dest, paths); err != nil {
		// ⛔ 合并失败不等于"没有东西可下载"：分段文件本身是好的，用户仍能
		// 按分段一个个下。所以这里用**可识别**的 ErrMergeFailed，而不是
		// ErrNotDownloadable —— 后者在控制器上映射成 404「暂无可下载的文件」，
		// 而文件明明在，等于告诉用户"录像丢了"。
		//
		// ⛔ 也不能"退而求其次只发第一片"：用户要的是一整段录像，
		// 悄悄给个截断文件才是最难查的错。
		return fmt.Errorf("%w: 分段合并不成功(%v)", ErrMergeFailed, err)
	}
	return nil
}

// fetchSegment 把一个分片从媒体节点拉到本地临时文件。
func (s *Service) fetchSegment(ctx context.Context, recorder Recorder, remote, local string) error {
	response, err := recorder.DownloadFile(ctx, remote, "")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDownloadable, err)
	}
	if response == nil || response.Body == nil {
		return ErrNotDownloadable
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return ErrNotDownloadable
	}
	file, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDownloadable, err)
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		return fmt.Errorf("%w: %v", ErrNotDownloadable, err)
	}
	return file.Close()
}

func totalSegmentSize(segments []gbmodels.RecordCacheSegment) int64 {
	var total int64
	for _, segment := range segments {
		total += segment.Size
	}
	return total
}

// mergedFileName 是合并后的下载文件名。
func mergedFileName(task *gbmodels.GbRecordCacheTask) string {
	id := strings.TrimSpace(task.TaskID)
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		id = "recording"
	}
	return "record-cache-" + id + ".mp4"
}

// serveLocalFile 把本地文件按单区间 Range 发给浏览器。
//
// 不复用 http.ServeContent：它要 *http.Request，而这一层只持有 Range 头字符串
// （控制器侧的窄接口刻意不把整个请求透进来）。
func serveLocalFile(writer http.ResponseWriter, path, name, rangeHeader string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDownloadable, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotDownloadable, err)
	}
	size := info.Size()

	header := writer.Header()
	header.Set("Content-Type", "video/mp4")
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))

	start, length, partial := int64(0), size, false
	if strings.TrimSpace(rangeHeader) != "" {
		rangeStart, rangeEnd, ok := parseRangeHeader(rangeHeader, size)
		if !ok {
			header.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return nil
		}
		start, length, partial = rangeStart, rangeEnd-rangeStart+1, true
	}
	header.Set("Content-Length", strconv.FormatInt(length, 10))
	if partial {
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
		writer.WriteHeader(http.StatusPartialContent)
	} else {
		writer.WriteHeader(http.StatusOK)
	}
	// 响应头已经发出，之后的失败只能靠连接中断传达，不再返回错误。
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	_, _ = io.CopyN(writer, file, length)
	return nil
}

// parseRangeHeader 解析单区间 Range（bytes=start-end / bytes=start- / bytes=-N）。
// 返回闭区间 [start, end]。
func parseRangeHeader(value string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(value, "bytes=") || size <= 0 {
		return 0, 0, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(value, "bytes="))
	if strings.Contains(spec, ",") {
		return 0, 0, false // 多区间不支持：下载场景用不到
	}
	dash := strings.Index(spec, "-")
	if dash < 0 {
		return 0, 0, false
	}
	startText := strings.TrimSpace(spec[:dash])
	endText := strings.TrimSpace(spec[dash+1:])

	if startText == "" {
		// bytes=-N 取最后 N 字节
		n, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if endText != "" {
		parsed, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || parsed < start {
			return 0, 0, false
		}
		if parsed < end {
			end = parsed
		}
	}
	return start, end, true
}
