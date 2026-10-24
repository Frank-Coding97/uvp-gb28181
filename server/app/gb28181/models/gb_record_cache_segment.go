package models

import (
	"encoding/json"
	"strings"
	"time"
)

// RecordCachePart 是一个分片内部的**源文件**（媒体节点上的一段 MP4）。
//
// ⛔ 什么时候一片会有多个源文件：设备在 download 模式下按**墙钟**推流
// （请求 R 秒媒体 @N 倍速 ⇒ 推 R/N 秒就停），而它的真实倍速总是略低于标称，
// 于是每次都少推 2%~4% 的尾部。平台按"实际录到的媒体时长"推进游标、
// 再开一次会话补拉 —— 补拉出来的是**同一段内容的延续**，只是落了另一个文件。
//
// 早期实现按"一次会话 = 一片"记账，30 分钟录像被切成 1 大 2 小共三片，
// 用户在界面上看到"三段"，以为录像是碎的。现在补拉文件并进同一片。
type RecordCachePart struct {
	// Stream 是 ZLM 上的流名（每次建会话随机生成，丢了就找不回文件）。
	Stream string `json:"stream"`
	// Name/Path 是 ZLM 给出的文件标识（Path 相对录制根目录）。
	Name string `json:"name"`
	Path string `json:"path"`
	// Size 是落盘字节数（可能为 0：ZLM 在文件仍被写时不给 file_size）。
	Size int64 `json:"size"`
	// MS 是这份文件的**媒体时长**（毫秒）。
	MS int64 `json:"ms"`
	// Period 是 ZLM 的日期分区（YYYY-MM-DD）。补拉可能跨到第二天，
	// 所以 period 记在**文件**这一级，不能只记在片这一级。
	Period string `json:"period"`
}

// RecordCacheSegment 是缓存任务的一个分片产出。
//
// 一个分片 = **一段连续的录像内容区间**（长度上限见 recordcache 的
// segmentMediaLimit：会话墙钟预算 × 倍速 × 安全系数）。区间内可能开了多次
// 回放会话（设备中途静默后平台补拉），每次会话产出的文件都记在 Parts 里。
//
// ⛔ 片级字段与 Parts 是**派生关系**，不是并列的两套账：
//   - Size / MS 是各 part 之和（聚合值）；
//   - Stream / Name / Path 取第一个 part（老代码把"片的文件标识"当单一值用）；
//   - Start / End 是这一片覆盖的内容区间。
//
// 只改一边不改另一边，会表现为"进度 200%""文件删了两遍""下载长度对不上"
// 这类互相矛盾的现象 —— 收尾合并与删除都必须走 Parts。
type RecordCacheSegment struct {
	Stream string `json:"stream"`
	// Name/Path 是 ZLM 给出的文件标识（Path 是相对录制根目录的路径）。
	Name string `json:"name"`
	Path string `json:"path"`
	// Size 是落盘字节数（可能为 0：ZLM 在文件仍被写时不给 file_size）。
	Size int64 `json:"size"`
	// MS 是**媒体时长**（毫秒）。它是游标推进的唯一可靠依据：
	// 倍速下载下墙钟 5 分钟能拉 20 分钟媒体，按墙钟推进会算错。
	MS int64 `json:"ms"`
	// Start/End 是这一片覆盖的**录像内容**区间（RFC3339）。
	Start string `json:"start"`
	End   string `json:"end"`
	// Period 是 ZLM 的日期分区（YYYY-MM-DD），删除文件时要按它定位。
	Period string `json:"period"`
	// Parts 是本片的全部源文件（按时间顺序）。
	Parts []RecordCachePart `json:"parts"`
}

// Sources 返回本片在媒体节点上的全部源文件（按时间顺序）。
//
// ⛔ 必须容忍**老数据**：改造前落库的分片清单没有 parts，只有片级的三元组。
// 不做这个回退，历史任务会瞬间变成"暂无可下载的文件"（文件明明还在节点上）。
func (s RecordCacheSegment) Sources() []RecordCachePart {
	if len(s.Parts) > 0 {
		return s.Parts
	}
	if strings.TrimSpace(s.Path) == "" && strings.TrimSpace(s.Name) == "" {
		return nil
	}
	return []RecordCachePart{{
		Stream: s.Stream, Name: s.Name, Path: s.Path,
		Size: s.Size, MS: s.MS, Period: s.Period,
	}}
}

// DecodeSegments 解析分片清单。空串 / 坏 JSON 一律返回空切片：
// 分片清单是**展示与下载**用的附加信息，坏掉不应该让任务本身不可用
// （任务的状态与游标都在独立列上）。
func (t *GbRecordCacheTask) DecodeSegments() []RecordCacheSegment {
	if t == nil || strings.TrimSpace(t.Segments) == "" {
		return nil
	}
	var segments []RecordCacheSegment
	if err := json.Unmarshal([]byte(t.Segments), &segments); err != nil {
		return nil
	}
	return segments
}

// EncodeSegments 序列化分片清单并写回字段。
// nil/空切片统一存 `[]`，避免出现"空串"与"[]"两种空表示。
func (t *GbRecordCacheTask) EncodeSegments(segments []RecordCacheSegment) error {
	if t == nil {
		return nil
	}
	if len(segments) == 0 {
		t.Segments = "[]"
		return nil
	}
	encoded, err := json.Marshal(segments)
	if err != nil {
		return err
	}
	t.Segments = string(encoded)
	return nil
}

// AppendSegment 追加一个分片并返回新的清单。
func (t *GbRecordCacheTask) AppendSegment(segment RecordCacheSegment) error {
	segments := append(t.DecodeSegments(), segment)
	return t.EncodeSegments(segments)
}

// CachedMediaDuration 是各分片的媒体时长合计。
// ⛔ 不要用 FinishedAt-StartedAt 代替：那是墙钟，倍速下载下与媒体时长差一个倍速。
func (t *GbRecordCacheTask) CachedMediaDuration() time.Duration {
	var total time.Duration
	for _, segment := range t.DecodeSegments() {
		total += time.Duration(segment.MS) * time.Millisecond
	}
	return total
}

// SourceFileCount 统计任务在媒体节点上的源文件总数（跨片求和）。
//
// ⛔ 它是"收尾要不要合成一个文件"的**唯一判据**，不是分片数：
// 补拉收进同一片之后，30 分钟录像的常见形态是「1 片 3 个源文件」——
// 按分片数判会直接跳过合成，用户下到的只有第一段，尾部全丢。
func (t *GbRecordCacheTask) SourceFileCount() int {
	total := 0
	for _, segment := range t.DecodeSegments() {
		total += len(segment.Sources())
	}
	return total
}
