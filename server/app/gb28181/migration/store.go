package migration

import (
	"time"

	"gorm.io/gorm"
)

// schemaMigration 版本表 gb_schema_migrations 的一条记录。
type schemaMigration struct {
	Version   string    `gorm:"column:version;primaryKey;type:varchar(255)"`
	AppliedAt time.Time `gorm:"column:applied_at"`
}

// TableName 版本表表名。
func (schemaMigration) TableName() string { return "gb_schema_migrations" }

// Store 封装版本表的读写,建表语句走 GORM 自动迁移(方言无关)。
type Store struct {
	db *gorm.DB
}

// NewStore 基于 GORM 连接构造 Store。
func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// EnsureTable 幂等建版本表。
func (s *Store) EnsureTable() error {
	return s.db.AutoMigrate(&schemaMigration{})
}

// ListApplied 返回已应用版本,按版本升序。
func (s *Store) ListApplied() ([]string, error) {
	var rows []schemaMigration
	if err := s.db.Order("version").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Version)
	}
	return out, nil
}

// MarkApplied 批量标记版本为已应用。
func (s *Store) MarkApplied(versions []string) error {
	if len(versions) == 0 {
		return nil
	}
	now := time.Now()
	rows := make([]schemaMigration, 0, len(versions))
	for _, v := range versions {
		rows = append(rows, schemaMigration{Version: v, AppliedAt: now})
	}
	return s.db.Create(&rows).Error
}

// DeleteApplied 删除单条版本记录(down 回滚后调用)。
func (s *Store) DeleteApplied(version string) error {
	return s.db.Where("version = ?", version).Delete(&schemaMigration{}).Error
}

// IsEmpty 版本表是否为空(首启基线化判定用)。
func (s *Store) IsEmpty() (bool, error) {
	var count int64
	if err := s.db.Model(&schemaMigration{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}
