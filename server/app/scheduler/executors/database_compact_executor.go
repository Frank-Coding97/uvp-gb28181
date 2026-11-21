package executors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/schedulerhelper"
)

const DatabaseCompactExecutorName = "database-compact-executor"

const (
	// compactChunkPages 每次 PRAGMA incremental_vacuum 搬多少页（默认 4KB/页 ⇒ 约 4MB）。
	//
	// ⛔ 分块的意义就是把独占写锁切成毫秒级小段：设备心跳、录像索引、SIP 报文
	// 一直在写，一次搬 2GB 的大 VACUUM 会把它们挡上几分钟。
	compactChunkPages = 1000
	// compactMinFreePages 空闲页低于这个量就不值得开工（4000 页 ≈ 16MB）。
	compactMinFreePages = 4000
	// compactDefaultBudget 单次运行的时间预算，用完即停，第二晚接着回收。
	compactDefaultBudget = 2 * time.Minute
)

// DatabaseCompactExecutor 把日志清理腾出来的空闲页逐步还给操作系统。
//
// 背景（2026-11-21）：日志删了 100 多万行，磁盘却一点没回来 —— SQLite 删行只是
// 把页记进 freelist，文件大小不变，OS 看不到变化。彻底回收只有两条路：整库
// VACUUM（独占写锁几分钟，业务要停）或 auto_vacuum=INCREMENTAL + 本执行器
// 每天搬一点。选了后者：写锁按毫秒切段，业务无感。
//
// ⛔⛔ 前提是数据库文件头里 auto_vacuum=2。这个值只能在建库那一刻写入，
// 存量的老库（=1.1.25 及以前装的）改不了，必须先做一次
// `PRAGMA auto_vacuum=2; VACUUM;`。本执行器不改模式，只认模式：
// 发现是 0 就跳过并在摘要里写清楚，绝不偷偷跑整库 VACUUM。
type DatabaseCompactExecutor struct {
	DB *gorm.DB
	// Budget 单次运行的时间预算；零值走 compactDefaultBudget。
	Budget time.Duration
	// ChunkPages / MinFreePages 零值走默认值，仅供测试与后续调参使用。
	ChunkPages   int
	MinFreePages int
}

func (e *DatabaseCompactExecutor) Name() string { return DatabaseCompactExecutorName }

func (e *DatabaseCompactExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	db := e.DB
	if db == nil {
		db = app.DB()
	}
	if db == nil {
		return errors.New("database is unavailable")
	}
	// MySQL / PostgreSQL 由各自的表空间机制回收，不存在"文件内空闲页"。
	if db.Dialector.Name() != "sqlite" {
		schedulerhelper.ReportSummary(ctx, "跳过=非 SQLite 数据库，无需分块回收")
		return nil
	}

	mode, err := pragmaInt(ctx, db, "auto_vacuum")
	if err != nil {
		return fmt.Errorf("读取 auto_vacuum 失败: %w", err)
	}
	if mode != 2 {
		// 0=NONE / 1=FULL。这里刻意不动手：整库 VACUUM 会长时间占写锁，必须由人挑时机。
		schedulerhelper.ReportSummary(ctx, fmt.Sprintf(
			"跳过=auto_vacuum=%d，未启用增量回收（需一次 PRAGMA auto_vacuum=2 后执行 VACUUM 才切换，且必须停机）", mode))
		return nil
	}

	pageSize, err := pragmaInt(ctx, db, "page_size")
	if err != nil {
		return fmt.Errorf("读取 page_size 失败: %w", err)
	}
	if pageSize <= 0 {
		pageSize = 4096
	}
	freePages, err := pragmaInt(ctx, db, "freelist_count")
	if err != nil {
		return fmt.Errorf("读取 freelist_count 失败: %w", err)
	}

	chunk := e.ChunkPages
	if chunk <= 0 {
		chunk = compactChunkPages
	}
	minFree := e.MinFreePages
	if minFree <= 0 {
		minFree = compactMinFreePages
	}
	if freePages < minFree {
		schedulerhelper.ReportSummary(ctx, fmt.Sprintf(
			"跳过=空闲页 %d（约 %s）低于阈值 %d 页", freePages, humanBytes(int64(freePages)*int64(pageSize)), minFree))
		return nil
	}

	budget := e.Budget
	if budget <= 0 {
		budget = compactDefaultBudget
	}
	deadline := time.Now().Add(budget)
	startPages := freePages
	reclaimed := 0
	stoppedByBudget := false

	for freePages > 0 {
		if err := ctx.Err(); err != nil {
			// 取消/超时：已搬的部分都已提交，直接把进度写进摘要，别丢掉成绩。
			reportCompact(ctx, startPages, freePages, pageSize, reclaimed, "任务被取消")
			return err
		}
		if !time.Now().Before(deadline) {
			stoppedByBudget = true
			break
		}
		thisChunk := chunk
		if freePages < thisChunk {
			thisChunk = freePages
		}
		if err := db.WithContext(ctx).Exec(fmt.Sprintf("PRAGMA incremental_vacuum(%d)", thisChunk)).Error; err != nil {
			// 写锁竞争（database is locked）不该让整晚算失败：已回收的已经生效。
			reportCompact(ctx, startPages, freePages, pageSize, reclaimed, "执行中断："+err.Error())
			return fmt.Errorf("增量回收空闲页失败: %w", err)
		}
		next, err := pragmaInt(ctx, db, "freelist_count")
		if err != nil {
			reportCompact(ctx, startPages, freePages, pageSize, reclaimed, "读取进度失败")
			return fmt.Errorf("读取回收进度失败: %w", err)
		}
		if next >= freePages {
			// 一页都没搬动：尾部页可能被长读事务占着，下一晚再试，绝不死循环。
			reportCompact(ctx, startPages, freePages, pageSize, reclaimed, "本轮无法继续搬页（可能被读事务占用）")
			return nil
		}
		reclaimed += freePages - next
		freePages = next
	}

	note := ""
	switch {
	case freePages == 0:
		note = "空闲页已清空"
	case stoppedByBudget:
		note = "预算用尽，下次继续"
	}
	reportCompact(ctx, startPages, freePages, pageSize, reclaimed, note)
	return nil
}

func reportCompact(ctx context.Context, startPages, freePages, pageSize, reclaimed int, note string) {
	summary := fmt.Sprintf("空闲页 %d→%d（归还约 %s，每页 %d 字节）",
		startPages, freePages, humanBytes(int64(reclaimed)*int64(pageSize)), pageSize)
	if note != "" {
		summary += "；" + note
	}
	schedulerhelper.ReportSummary(ctx, summary)
}

func pragmaInt(ctx context.Context, db *gorm.DB, name string) (int, error) {
	var value int
	if err := db.WithContext(ctx).Raw("PRAGMA " + name).Scan(&value).Error; err != nil {
		return 0, err
	}
	return value, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
