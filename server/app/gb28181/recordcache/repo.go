package recordcache

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// GormRepo 是 Repository 的 GORM 实现。
type GormRepo struct{ db *gorm.DB }

func NewGormRepo(db *gorm.DB) *GormRepo { return &GormRepo{db: db} }

func (r *GormRepo) Create(ctx context.Context, task *gbmodels.GbRecordCacheTask) error {
	if r == nil || r.db == nil || task == nil {
		return ErrInvalidRequest
	}
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *GormRepo) Update(ctx context.Context, task *gbmodels.GbRecordCacheTask) error {
	if r == nil || r.db == nil || task == nil || task.ID == 0 {
		return ErrInvalidRequest
	}
	// ⛔ 只更新列，不 Save 整行：Save 会带零值覆盖掉并发 tick 写进去的字段
	// （缓存任务是"多轮推进 + 用户随时取消"，整行覆盖会互相抹掉对方的进度）。
	//
	// ⛔⛔ 这份列清单必须覆盖**所有运行期会变的列**，少一列就是"不报错的错法"：
	// 最典型的是 `segments`（分片产出清单）—— 漏了它，EncodeSegments 写进内存的值
	// 永远落不了库，于是 CachedMediaDuration() 恒为 0、游标走"按请求区间推进"的
	// 退化路径、任务宣布成功但**用户一个文件都拿不到**，而 ZLM 上文件明明录着。
	// 契约测试 `TestRecordCacheUpdatePersistsEveryMutableColumn` 钉住这份清单。
	return r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{}).
		Where("id = ?", task.ID).
		Select("session_id", "node_id", "vhost", "app", "stream",
			"file_id", "file_name", "file_path", "file_size",
			"cached_bytes", "estimated_bytes", "cursor_at", "segments",
			"state", "last_error", "started_at", "finished_at", "expires_at", "favorite").
		Updates(map[string]any{
			"session_id":      task.SessionID,
			"node_id":         task.NodeID,
			"vhost":           task.VHost,
			"app":             task.App,
			"stream":          task.Stream,
			"file_id":         task.FileID,
			"file_name":       task.FileName,
			"file_path":       task.FilePath,
			"file_size":       task.FileSize,
			"cached_bytes":    task.CachedBytes,
			"estimated_bytes": task.EstimatedBytes,
			"cursor_at":       task.CursorAt,
			"segments":        task.Segments,
			"state":           task.State,
			"last_error":      task.LastError,
			"started_at":      task.StartedAt,
			"finished_at":     task.FinishedAt,
			"expires_at":      task.ExpiresAt,
			// ⛔ 收藏必须在这一份列清单里：漏了它，SetFavorite 写进内存的值永远落不了库，
			// 表现是「星标点了又弹回去、清理照样把收藏的录像删掉」，且没有任何报错。
			"favorite": task.Favorite,
		}).Error
}

// FindByTaskID 按任务号取一行；scope 非 nil 时叠加数据权限过滤。
//
// ⛔ 不用 `First` + `ErrRecordNotFound` 判存在性（全仓禁用那个组合：
// GORM 的回调链会把 not-found 掩成零值，表现是"查到了但字段全空"）。
func (r *GormRepo) FindByTaskID(ctx context.Context, taskID string, scope Scope) (*gbmodels.GbRecordCacheTask, error) {
	if r == nil || r.db == nil || strings.TrimSpace(taskID) == "" {
		return nil, ErrInvalidRequest
	}
	query := r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{}).Where("task_id = ?", taskID)
	if scope != nil {
		query = query.Scopes(scope)
	}
	var rows []gbmodels.GbRecordCacheTask
	if err := query.Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, ErrTaskNotFound
	}
	return &rows[0], nil
}

func (r *GormRepo) List(ctx context.Context, query ListQuery) ([]gbmodels.GbRecordCacheTask, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, ErrInvalidRequest
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}
	size := query.PageSize
	if size <= 0 || size > 200 {
		size = 20
	}
	build := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{})
		if query.Scope != nil {
			q = q.Scopes(query.Scope)
		}
		if state := strings.TrimSpace(query.State); state != "" {
			q = q.Where("state = ?", state)
		}
		if query.ChannelID > 0 {
			q = q.Where("channel_id = ?", query.ChannelID)
		}
		// ⛔ 用指针而不是 bool：`favorite=false`（只看未收藏）和"不筛收藏"是两回事，
		// 用值类型会让零值 false 把"不筛选"悄悄变成"只看未收藏"。
		if query.Favorite != nil {
			q = q.Where("favorite = ?", *query.Favorite)
		}
		if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
			// 与列表展示口径一致：用户看到的是通道名/设备名，就按这两列搜。
			like := "%" + keyword + "%"
			q = q.Where("channel_name LIKE ? OR device_name LIKE ? OR device_id LIKE ? OR channel_code LIKE ?", like, like, like, like)
		}
		return q
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []gbmodels.GbRecordCacheTask
	if err := build().Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *GormRepo) Delete(ctx context.Context, id uint64) error {
	if r == nil || r.db == nil || id == 0 {
		return ErrInvalidRequest
	}
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&gbmodels.GbRecordCacheTask{}).Error
}

func (r *GormRepo) FindActiveByChannel(ctx context.Context, channelID uint) (*gbmodels.GbRecordCacheTask, error) {
	if r == nil || r.db == nil || channelID == 0 {
		return nil, ErrInvalidRequest
	}
	var rows []gbmodels.GbRecordCacheTask
	err := r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{}).
		Where("channel_id = ? AND state IN ?", channelID,
			[]string{gbmodels.RecordCacheStateQueued, gbmodels.RecordCacheStateRunning}).
		Order("id DESC").Limit(1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrTaskNotFound
	}
	return &rows[0], nil
}

func (r *GormRepo) ListAdvanceable(ctx context.Context, limit int) ([]gbmodels.GbRecordCacheTask, error) {
	if r == nil || r.db == nil {
		return nil, ErrInvalidRequest
	}
	if limit <= 0 {
		limit = 10
	}
	var rows []gbmodels.GbRecordCacheTask
	// ⛔ 不按 state 排序（没有优先级语义），只按 id 保证推进顺序稳定，
	// 免得同一批任务在每轮 tick 里被反复挑中/漏掉。
	//
	// ⛔ merging 必须在内：合成是异步的，进程若在合成途中重启，任务会停在
	// merging 且没有人在跑合并 —— 只有把它纳入推进集合，下一轮 tick 才会
	// 重新驱动一次（幂等：产物已存在就直接判定成功），否则它永远卡在那里。
	err := r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{}).
		Where("state IN ?", []string{
			gbmodels.RecordCacheStateQueued, gbmodels.RecordCacheStateRunning,
			gbmodels.RecordCacheStateMerging,
		}).
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *GormRepo) ListExpired(ctx context.Context, now time.Time, limit int) ([]gbmodels.GbRecordCacheTask, error) {
	if r == nil || r.db == nil {
		return nil, ErrInvalidRequest
	}
	if limit <= 0 {
		limit = 20
	}
	var rows []gbmodels.GbRecordCacheTask
	// 只有 succeeded / cancelled 才有文件要清；queued / running 的 expires_at 是空的。
	//
	// ⛔ 收藏的（favorite=1）**永不自动清理**：保留期只是"没人在意就删掉"的默认策略，
	// 用户显式收藏就是"我在意"。把这条过滤放在 SQL 里而不是在 Go 循环里跳过，
	// 是因为 ListExpired 每轮只取 limit 条 —— 在循环里跳过会让一批收藏任务
	// 反复占满名额，把真正该清的排在后面永远清不到。
	err := r.db.WithContext(ctx).Model(&gbmodels.GbRecordCacheTask{}).
		Where("state IN ?", []string{gbmodels.RecordCacheStateSucceeded, gbmodels.RecordCacheStateCancelled}).
		Where("favorite = ?", false).
		Where("expires_at IS NOT NULL AND expires_at <= ?", now).
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}
