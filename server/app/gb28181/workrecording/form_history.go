package workrecording

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm/clause"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// formHistoryListLimit 上限：超过后下拉框会被噪声淹没，且 SELECT 扫表变慢。
// 50 是经验值：足够覆盖一两个月的常用项目/人员/站区。
const formHistoryListLimit = 50

var ErrFormHistoryInvalidField = errors.New("历史值字段必须在 4 个白名单内")

// clauseOnConflictUpdateAll 把两个有值列同步到「已存在」行。
// use_count 走数据库原子累加（"use_count + 1"），last_used_at 取本次提交时间。
// SQLite 同样支持 ON CONFLICT ... DO UPDATE；用小写 excluded 兼容 PG/SQLite 习惯。
func clauseOnConflictUpdateAll() clause.OnConflict {
	return clause.OnConflict{
		Columns: []clause.Column{{Name: "field_key"}, {Name: "value"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"use_count":    clause.Expr{SQL: "use_count + 1"},
			"last_used_at": clause.Expr{SQL: "excluded.last_used_at"},
		}),
	}
}

// upsertFormHistory 写入一条历史值，重复值自动累加 use_count 并刷新 last_used_at。
// 一次 batch 提交触发 4~N 条写入（N 是作业人员数），所以每条单独 upsert 不在事务里：
// 表本身没有外键，写失败也不应阻断作业单创建。
func (s *BatchService) upsertFormHistory(ctx context.Context, field string, value string, now time.Time) {
	if s == nil || s.db == nil || field == "" || value == "" {
		return
	}
	// MySQL 5.7+ / PostgreSQL 9.5+ / SQL Server 2008+ 都支持 ON DUPLICATE KEY / ON CONFLICT / MERGE。
	// GORM 的 Clauses(clause.OnConflict) 会按方言展开成对应语法。
	err := s.db.WithContext(ctx).Clauses(clauseOnConflictUpdateAll()).Create(&models.GbWorkOrderFormHistory{
		FieldKey:   field,
		Value:      value,
		UseCount:   1,
		LastUsedAt: now,
	}).Error
	if err != nil {
		s.recordFormHistoryFailure(ctx, field, err)
	}
}

// recordFormHistoryFailure 把写失败记到运行日志，但不抛错。
// 默认实现：留 hook，bootstrap.go 可以注入 zap/logrus 等真实 logger。
func (s *BatchService) recordFormHistoryFailure(ctx context.Context, field string, err error) {
	if s == nil || s.formHistoryFailureSink == nil {
		return
	}
	s.formHistoryFailureSink(ctx, field, err)
}

// recordFormHistory 提取 4 个白名单字段的当前值，分别 upsert。
// workPersonnel 列表逐个 upsert，顿号分隔的整串不存。
// 时间戳走 s.now() 而非 time.Now()，让测试能冻结。
func (s *BatchService) recordFormHistory(ctx context.Context, form Form) {
	now := s.now()
	if v := strings.TrimSpace(form.ProjectName); v != "" {
		s.upsertFormHistory(ctx, models.FormHistoryFieldProjectName, v, now)
	}
	if v := strings.TrimSpace(form.StationArea); v != "" {
		s.upsertFormHistory(ctx, models.FormHistoryFieldStationArea, v, now)
	}
	if v := strings.TrimSpace(form.WorkLeader); v != "" {
		s.upsertFormHistory(ctx, models.FormHistoryFieldWorkLeader, v, now)
	}
	for _, name := range form.WorkPersonnel {
		if v := strings.TrimSpace(name); v != "" {
			s.upsertFormHistory(ctx, models.FormHistoryFieldWorkPersonnel, v, now)
		}
	}
}

// ListFormHistory 给前端 a-auto-complete 喂数据。
// fieldKey 必须在 4 个白名单内（前端硬编码），服务端再校验一次防绕。
// limit 为 0 或负时用 formHistoryListLimit；超过 formHistoryListLimit 自动夹到上限。
func (s *BatchService) ListFormHistory(ctx context.Context, fieldKey string, limit int) ([]FormHistoryEntry, error) {
	if s == nil || s.db == nil {
		return nil, ErrBatchInvalid
	}
	if !models.IsValidFormHistoryField(fieldKey) {
		return nil, ErrFormHistoryInvalidField
	}
	if limit <= 0 || limit > formHistoryListLimit {
		limit = formHistoryListLimit
	}
	var rows []models.GbWorkOrderFormHistory
	if err := s.db.WithContext(ctx).
		Where("field_key = ?", fieldKey).
		Order("last_used_at DESC, use_count DESC, id DESC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	entries := make([]FormHistoryEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, FormHistoryEntry{
			Value:      row.Value,
			UseCount:   row.UseCount,
			LastUsedAt: row.LastUsedAt,
		})
	}
	return entries, nil
}
