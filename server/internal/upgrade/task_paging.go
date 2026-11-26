package upgrade

import "sort"

// StatusTaskLimit 是状态响应里携带的「最近任务」条数上限。
//
// ⛔⛔ 状态接口不能回传全部历史任务：每个任务都带完整的中文更新说明（约 3KB/条），
// 条数随升级次数单调增长。线上 13 条任务时状态响应已达 72276 字节，超过业务进程
// 64KB 的读取上限后整页 503（表现为「在线升级失败」）。历史记录只走分页接口，
// 状态接口的体积必须与「装过多少个版本」无关。
const StatusTaskLimit = 20

// RecentTaskSummaries 返回按更新时间倒序的最近 limit 条任务，且**清空更新说明**。
//
// 说明正文只在两处需要：历史列表（分页接口会给全）和任务详情抽屉（按 ID 取单条）。
// 状态接口每 5 秒轮询一次，没有理由把几十 KB 的正文带着跑。
func RecentTaskSummaries(records []TaskRecord, limit int) []TaskRecord {
	if limit <= 0 {
		limit = StatusTaskLimit
	}
	sorted := SortTasksByUpdatedAt(records)
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	summaries := make([]TaskRecord, len(sorted))
	for i := range sorted {
		summary := sorted[i]
		summary.ReleaseNotes = ""
		summaries[i] = summary
	}
	return summaries
}

// SortTasksByUpdatedAt 按更新时间倒序排序，返回新切片（不改调用方的顺序）。
// 时间戳是固定宽度的 RFC3339，字符串比较即可；缺失时间的排最后。
func SortTasksByUpdatedAt(records []TaskRecord) []TaskRecord {
	sorted := append([]TaskRecord(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt > sorted[j].UpdatedAt
	})
	return sorted
}

// PageTasks 按页码（从 1 开始）切分任务列表，返回该页记录与总数。
// 每页条数收敛到 [1, MaxTaskPageSize]，避免调用方传 0 或离谱值把响应体撑大。
func PageTasks(records []TaskRecord, page, pageSize int) ([]TaskRecord, int) {
	sorted := SortTasksByUpdatedAt(records)
	total := len(sorted)
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > MaxTaskPageSize {
		pageSize = MaxTaskPageSize
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []TaskRecord{}, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return append([]TaskRecord(nil), sorted[start:end]...), total
}
