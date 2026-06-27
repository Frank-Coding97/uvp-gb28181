package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// SchedulerLogDTO gorm 模型,对应 scheduler_log 表
//
// 业务侧实体在 scheduler.SchedulerLog(避免本包反向依赖 scheduler 包,
// 这里独立 DTO,转换由仓库方法负责)。
type SchedulerLogDTO struct {
	ID           int64     `gorm:"primaryKey;column:id"`
	HappenedAt   time.Time `gorm:"column:happened_at;not null;index:idx_happened_at"`
	Algorithm    string    `gorm:"column:algorithm;size:32;not null;default:''"`
	NodeID       int64     `gorm:"column:node_id;not null;default:0"`
	NodeName     string    `gorm:"column:node_name;size:64;not null;default:''"`
	StreamID     string    `gorm:"column:stream_id;size:64;not null;default:''"`
	DeviceID     string    `gorm:"column:device_id;size:64;not null;default:''"`
	ChannelID    string    `gorm:"column:channel_id;size:64;not null;default:''"`
	ErrorMessage string    `gorm:"column:error_message;size:255;not null;default:''"`
}

// TableName 显式表名
func (SchedulerLogDTO) TableName() string { return "scheduler_log" }

// SchedulerLogRow 上层 scheduler 包传入的纯数据结构(由 scheduler 包定义)
//
// 这里用接口签名而不导入 scheduler 包,避免 repo → scheduler 反向依赖。
// LogService 调用仓库时传 SchedulerLogRow 同构体的字段即可。
type SchedulerLogRow struct {
	ID           int64
	HappenedAt   time.Time
	Algorithm    string
	NodeID       int64
	NodeName     string
	StreamID     string
	DeviceID     string
	ChannelID    string
	ErrorMessage string
}

func (r SchedulerLogRow) toDTO() SchedulerLogDTO {
	return SchedulerLogDTO{
		ID:           r.ID,
		HappenedAt:   r.HappenedAt,
		Algorithm:    r.Algorithm,
		NodeID:       r.NodeID,
		NodeName:     r.NodeName,
		StreamID:     r.StreamID,
		DeviceID:     r.DeviceID,
		ChannelID:    r.ChannelID,
		ErrorMessage: r.ErrorMessage,
	}
}

func (d SchedulerLogDTO) toRow() SchedulerLogRow {
	return SchedulerLogRow{
		ID:           d.ID,
		HappenedAt:   d.HappenedAt,
		Algorithm:    d.Algorithm,
		NodeID:       d.NodeID,
		NodeName:     d.NodeName,
		StreamID:     d.StreamID,
		DeviceID:     d.DeviceID,
		ChannelID:    d.ChannelID,
		ErrorMessage: d.ErrorMessage,
	}
}

// GormSchedulerLogRepo gorm 仓库实现
//
// LogService 通过 scheduler.SchedulerLogRepo 接口持有它,
// 用 Insert / List / PruneOlderThan 三方法操作 scheduler_log 表。
type GormSchedulerLogRepo struct {
	db *gorm.DB
}

// NewGormSchedulerLogRepo 构造
func NewGormSchedulerLogRepo(db *gorm.DB) *GormSchedulerLogRepo {
	return &GormSchedulerLogRepo{db: db}
}

// Insert 单条写入
//
// 失败由调用方记日志(LogService worker);超长字段(error_message > 255)
// 调用方应先截断,这里不做截断。
func (r *GormSchedulerLogRepo) Insert(ctx context.Context, row SchedulerLogRow) error {
	d := row.toDTO()
	return r.db.WithContext(ctx).Create(&d).Error
}

// List 按 happened_at DESC 取最近 N 条(limit <= 0 全返)
//
// 给 Controller GET /api/gb28181/zlm/scheduler/logs 用。
func (r *GormSchedulerLogRepo) List(ctx context.Context, limit int) ([]SchedulerLogRow, error) {
	var rows []SchedulerLogDTO
	q := r.db.WithContext(ctx).Order("happened_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SchedulerLogRow, 0, len(rows))
	for _, d := range rows {
		out = append(out, d.toRow())
	}
	return out, nil
}

// PruneOlderThan 删除 happened_at < t 的行,返回删除条数
//
// bootstrap 24h ticker 调,保留近 7 天。
func (r *GormSchedulerLogRepo) PruneOlderThan(ctx context.Context, t time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where("happened_at < ?", t).Delete(&SchedulerLogDTO{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
