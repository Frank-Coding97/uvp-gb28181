package models

import "time"

type GbSipMetricMinute struct {
	ID                 uint64    `gorm:"primaryKey"`
	BucketStart        time.Time `gorm:"column:bucket_start;not null;uniqueIndex:uk_sip_metric_minute_bucket,priority:1;index:idx_sip_metric_minute_bucket"`
	Method             string    `gorm:"column:method;size:16;not null;uniqueIndex:uk_sip_metric_minute_bucket,priority:2"`
	Direction          string    `gorm:"column:direction;size:8;not null;uniqueIndex:uk_sip_metric_minute_bucket,priority:3"`
	RequestCount       uint64    `gorm:"column:request_count;not null;default:0"`
	TransactionCount   uint64    `gorm:"column:transaction_count;not null;default:0"`
	TransactionSuccess uint64    `gorm:"column:transaction_success;not null;default:0"`
	TransactionFailure uint64    `gorm:"column:transaction_failure;not null;default:0"`
	CreatedAt          time.Time `gorm:"column:created_at;not null"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null"`
}

func (GbSipMetricMinute) TableName() string { return "gb_sip_metric_minute" }

type GbSipMetricFlush struct {
	ID        uint64    `gorm:"primaryKey"`
	FlushID   string    `gorm:"column:flush_id;size:64;not null;uniqueIndex:uk_sip_metric_flush_id"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index:idx_sip_metric_flush_created"`
}

func (GbSipMetricFlush) TableName() string { return "gb_sip_metric_flush" }

type GbSipMetricGap struct {
	ID           uint64    `gorm:"primaryKey"`
	StartedAt    time.Time `gorm:"column:started_at;not null;index:idx_sip_metric_gap_window,priority:1"`
	EndedAt      time.Time `gorm:"column:ended_at;not null;index:idx_sip_metric_gap_window,priority:2"`
	Reason       string    `gorm:"column:reason;size:32;not null"`
	DroppedCount uint64    `gorm:"column:dropped_count;not null;default:0"`
	CreatedAt    time.Time `gorm:"column:created_at;not null"`
}

func (GbSipMetricGap) TableName() string { return "gb_sip_metric_gap" }
