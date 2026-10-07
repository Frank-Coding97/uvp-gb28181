package recordcache

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// newTaskID 生成任务号。用 UUID 而不是自增：任务号会出现在下载 URL 里，
// 可枚举的自增号等于把"别人缓存了什么录像"暴露出去。
func newTaskID() string { return uuid.NewString() }

// newRequestID 是回放会话幂等键的前缀（每片再拼片序号）。
func newRequestID() string { return "rc-" + uuid.NewString() }

// viewOf 把模型转成对外视图。
//
// live=true 时额外向 ZLM 取一次实时状态（码率 / 本片已推进的媒体时长 / 已收字节）：
// 这些都是**展示字段**，取不到就留零值，不能因为 ZLM 查询失败而让详情接口失败。
//
// ⛔ 列表接口传 live=false 是有意的：N 条任务就是 N 次外部 HTTP 调用，
// 会把列表接口的延迟绑在 ZLM 上。
func (s *Service) viewOf(ctx context.Context, task *gbmodels.GbRecordCacheTask, live bool) TaskView {
	if task == nil {
		return TaskView{}
	}
	segments := task.DecodeSegments()
	files := make([]FileView, 0, len(segments))
	for index, segment := range segments {
		files = append(files, FileView{
			Index: index, Name: segment.Name, Size: segment.Size,
			StartTime: segment.Start, EndTime: segment.End,
			// ⛔ 按**源文件**判可下载性：片级 Path/Name 只是第一个源文件的副本，
			// 拿它当唯一依据会在"片内第一个文件缺失、其余都好"时谎报不可下载。
			Downloadable: downloadableSegment(segment),
		})
	}
	total := task.EndTime.Sub(task.StartTime).Seconds()
	cached := task.CachedMediaDuration().Seconds()

	// ⛔ 正在录的这一片**还没进 segments**：分片清单只在整片收尾时才落库，
	// 而单片媒体上限最长可到 SegmentMediaCap（常见设备 4× ⇒ 90 分钟媒体
	// ≈ 22.5 分钟墙钟）。不补这一段，进度在一整片之内恒为 0 ——
	// 用户看到的是"1.8 MB/s 明明在拉、进度 0% 纹丝不动"，会以为卡死。
	// ZLM 的 getMediaInfo 对每条 track 都给出已推流的 duration（毫秒），实时增长，
	// 正是"本片已经拉了多长媒体"。
	var speed, liveBytes uint64
	if live && task.State == gbmodels.RecordCacheStateRunning {
		var liveSeconds float64
		speed, liveSeconds, liveBytes = s.liveStats(ctx, task)
		cached += liveSeconds
	}
	if cached > total {
		cached = total
	}
	progress := 0.0
	if total > 0 {
		progress = cached / total
	}
	if progress > 1 {
		progress = 1
	}
	if task.State == gbmodels.RecordCacheStateSucceeded {
		progress = 1
		cached = total
	}

	view := TaskView{
		TaskID: task.TaskID, ChannelID: task.ChannelID, DeviceID: task.DeviceID,
		ChannelCode: task.ChannelCode, ChannelName: task.ChannelName, DeviceName: task.DeviceName,
		StartTime: task.StartTime.Format(time.RFC3339), EndTime: task.EndTime.Format(time.RFC3339),
		RecordType: task.RecordType, DownloadSpeed: task.DownloadSpeed,
		State: task.State, LastError: task.LastError, Favorite: task.Favorite,
		CachedBytes: task.CachedBytes + liveBytes, CachedSeconds: cached, TotalSeconds: total, Progress: progress,
		EstimatedBytes: task.EstimatedBytes,
		Files:          files,
		CreatedByName:  task.CreatedByName, CreatedAt: task.CreatedAt.Format(time.RFC3339),
	}
	if task.StartedAt != nil {
		view.StartedAt = task.StartedAt.Format(time.RFC3339)
	}
	if task.FinishedAt != nil {
		view.FinishedAt = task.FinishedAt.Format(time.RFC3339)
	}
	if task.ExpiresAt != nil {
		view.ExpiresAt = task.ExpiresAt.Format(time.RFC3339)
	}
	if live {
		view.SpeedBytesPerSec = speed
	}
	return view
}

// liveStats 取 ZLM 上该流的实时状态：
//
//	speed        —— 瞬时码率（字节/秒），用于展示；
//	mediaSeconds —— 该流**已推进的媒体时长**（秒），用于片内进度；
//	streamBytes  —— 该流累计接收字节，近似等于本片已落盘大小。
//
// ⛔ mediaSeconds 是这里最要紧的一个：ZLM 的 getMediaInfo 对每条 track 都给出
// duration（毫秒，已推流的时间戳跨度）。设备按 N 倍速推历史录像时，它增长的
// 速度就是 N 倍速，所以它就是"本片已经拉了多长媒体" ——
// 进度条不必再等整片收尾，按轮询粒度就能刷新。
//
// 取不到就返回零值：这几个都是**展示字段**，ZLM 查询失败不能让详情接口失败。
func (s *Service) liveStats(ctx context.Context, task *gbmodels.GbRecordCacheTask) (speed uint64, mediaSeconds float64, streamBytes uint64) {
	if task == nil || strings.TrimSpace(task.Stream) == "" {
		return 0, 0, 0
	}
	recorder, err := s.recorderFor(task.NodeID)
	if err != nil {
		return 0, 0, 0
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info, err := recorder.GetMediaInfo(queryCtx, mediaSchema, mediaVHost, mediaApp, task.Stream)
	if err != nil || info == nil {
		return 0, 0, 0
	}
	// 按最大的 track 时长算：音视频的 duration 会差几十毫秒，
	// 取大的那个才是这一段真正覆盖到的媒体范围。
	for _, track := range info.Tracks {
		if seconds := track.Duration / 1000; seconds > mediaSeconds {
			mediaSeconds = seconds
		}
	}
	return info.BytesSpeed, mediaSeconds, info.TotalBytes
}
