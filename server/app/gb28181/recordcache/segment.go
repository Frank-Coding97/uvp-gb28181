package recordcache

import (
	"strings"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// ── 分片记账：一次会话的产出算哪一片 ──────────────────────────────────
//
// 产品口径：**一片 = 一段连续的录像内容区间**，不是"一次回放会话"。
//
// 设备在 download 模式下按墙钟推流（请求 R 秒媒体 @N 倍速 ⇒ 推 R/N 秒就停），
// 而真实有效倍速总比标称低一点（本环境实测 3.88~4.00×），于是每次都少推
// 2%~4% 的尾部；平台按"实际录到的媒体时长"推进游标、再开会话补拉。
//
// 补拉出来的文件属于**同一段内容**，必须并进同一片。早期实现按"一次会话 = 一片"
// 记账，30 分钟录像被切成 1 大 2 小三片，用户在界面上看到"三段"，
// 以为录像是碎的（还多花两次 15~20 秒的建会话开销）。

// segmentOrigin 返回该片覆盖区间的起点（片级 Start，RFC3339）。
//
// ⛔ 判分片边界必须锚在**片起点**上，不能用某个源文件的时间：补拉会让"本段的
// 起点"不断往后漂，而片起点在首次落片时就定下、之后不再变。
func segmentOrigin(segment gbmodels.RecordCacheSegment) (time.Time, bool) {
	text := strings.TrimSpace(segment.Start)
	if text == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// segmentFull 报告"当前片是否已经装到单片上限"。
//
// ⛔ 判据只有这一条，而 `startSegment`（夹会话区间）与 `appendSegmentFiles`
// （决定并进哪一片）必须用**同一个**判据。两处不一致的后果不是报错，而是静默地
// 把片撑到两倍上限（单文件几个 GB、超出自动合并上限），或者当场重开一片。
func segmentFull(segments []gbmodels.RecordCacheSegment, segStart time.Time, limit time.Duration) bool {
	if len(segments) == 0 || limit <= 0 {
		return false
	}
	origin, ok := segmentOrigin(segments[len(segments)-1])
	if !ok {
		return false
	}
	return !segStart.Before(origin.Add(limit))
}

// segmentCapEnd 是本片允许拉到的媒体终点。
//
// 当前片还装得下时夹到"片起点 + 上限"（一次会话最多把**当前片**装满，不许越到
// 下一片）；当前片已经满了，说明本次会话会另起一片，直接用完整的单片上限即可
// （新片的起点就是这次会话的起点）。
func segmentCapEnd(segments []gbmodels.RecordCacheSegment, segStart time.Time, limit time.Duration) time.Time {
	if limit <= 0 {
		return segStart
	}
	if len(segments) == 0 || segmentFull(segments, segStart, limit) {
		return segStart.Add(limit)
	}
	origin, ok := segmentOrigin(segments[len(segments)-1])
	if !ok {
		return segStart.Add(limit)
	}
	return origin.Add(limit)
}

// appendSegmentFiles 把一次会话采集到的文件并入分片清单。
//
// 默认并入**当前片**（补拉是同一段内容的延续）；只有当前片已经装到单片上限时，
// 这一批才另起一片。返回更新后的清单（可能与入参共享底层数组，调用方直接用返回值）。
func appendSegmentFiles(
	segments, fresh []gbmodels.RecordCacheSegment,
	segStart time.Time,
	limit time.Duration,
) []gbmodels.RecordCacheSegment {
	if len(fresh) == 0 {
		return segments
	}
	if len(segments) > 0 && !segmentFull(segments, segStart, limit) {
		segments[len(segments)-1] = absorbSegmentFiles(segments[len(segments)-1], fresh, segStart)
		return segments
	}
	return append(segments, absorbSegmentFiles(gbmodels.RecordCacheSegment{}, fresh, segStart))
}

// absorbSegmentFiles 把 fresh 里的文件作为源文件并入 base，并重算片级聚合字段。
//
// ⛔ 聚合字段（Size / MS / Name / Path / Stream）一律**重算**，不能累加：
// 重复并入同一批会让它们翻倍，而进度、删除、下载分别读这几个字段，
// 翻倍会同时表现为"进度 200%""同一个文件删两遍""下载长度对不上"。
func absorbSegmentFiles(
	base gbmodels.RecordCacheSegment,
	fresh []gbmodels.RecordCacheSegment,
	segStart time.Time,
) gbmodels.RecordCacheSegment {
	// ⛔ 显式复制：老数据走 Sources() 会现造一个切片，新数据直接返回 base.Parts，
	// 后者 append 可能复用同一底层数组 —— 复制一份省掉这类纠缠。
	parts := make([]gbmodels.RecordCachePart, 0, len(base.Parts)+len(fresh))
	parts = append(parts, base.Sources()...)
	for _, file := range fresh {
		parts = append(parts, file.Sources()...)
	}
	base.Parts = parts
	recomputeSegment(&base)
	if strings.TrimSpace(base.Start) == "" {
		base.Start = segStart.Format(time.RFC3339)
	}
	return base
}

// recomputeSegment 按 Parts 重算片级聚合字段（Size / MS / Stream / Name / Path）。
func recomputeSegment(segment *gbmodels.RecordCacheSegment) {
	if segment == nil {
		return
	}
	var size, media int64
	for _, part := range segment.Parts {
		size += part.Size
		media += part.MS
	}
	segment.Size, segment.MS = size, media
	if len(segment.Parts) > 0 {
		// 片级三元组 = 第一个源文件：老代码把"片的文件标识"当单一值用
		// （同源透传、按名删除），保留这个读法能让下载与删除不必区分片内几个文件。
		first := segment.Parts[0]
		segment.Stream, segment.Name, segment.Path = first.Stream, first.Name, first.Path
		if strings.TrimSpace(segment.Period) == "" {
			segment.Period = first.Period
		}
	}
	if origin, ok := segmentOrigin(*segment); ok {
		segment.End = origin.Add(time.Duration(segment.MS) * time.Millisecond).Format(time.RFC3339)
	}
}

// sourcesOf 把分片清单展平成"要按顺序拉回来的源文件路径"。
//
// ⛔ 必须按「片序 → 片内序」展平：补拉出来的文件在媒体节点上各是一个独立 MP4，
// 顺序错了拼出来的时间轴就是跳的（而且文件本身还能正常播放，极难发现）。
func sourcesOf(segments []gbmodels.RecordCacheSegment) []string {
	var paths []string
	for _, segment := range segments {
		for _, part := range segment.Sources() {
			paths = append(paths, strings.TrimSpace(part.Path))
		}
	}
	return paths
}

// downloadableSegment 报告该片是否有可下载的源文件（任一源文件路径非空即可）。
func downloadableSegment(segment gbmodels.RecordCacheSegment) bool {
	for _, source := range segment.Sources() {
		if strings.TrimSpace(source.Path) != "" {
			return true
		}
	}
	return false
}
