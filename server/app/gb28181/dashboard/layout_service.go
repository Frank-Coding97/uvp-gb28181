package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const homeDashboardKey = "home"

var ErrLayoutRevisionConflict = errors.New("dashboard layout revision conflict")

type StoredLayout struct {
	Revision uint64 `json:"revision"`
	Layout   Layout `json:"layout"`
}

type LayoutService struct {
	db *gorm.DB
}

func NewLayoutService(db *gorm.DB) *LayoutService {
	return &LayoutService{db: db}
}

func (service *LayoutService) Get(ctx context.Context, userID uint) (StoredLayout, error) {
	var record gbmodels.GbDashboardLayout
	err := service.db.WithContext(ctx).
		Where("user_id = ? AND dashboard_key = ?", userID, homeDashboardKey).
		Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return StoredLayout{Layout: DefaultLayout()}, nil
	}
	if err != nil {
		return StoredLayout{}, err
	}
	var layout Layout
	if err := json.Unmarshal([]byte(record.LayoutJSON), &layout); err != nil {
		return StoredLayout{Revision: record.Revision, Layout: DefaultLayout()}, nil
	}
	normalized, err := NormalizeLayout(layout)
	if err != nil {
		return StoredLayout{Revision: record.Revision, Layout: DefaultLayout()}, nil
	}
	return StoredLayout{Revision: record.Revision, Layout: normalized}, nil
}

func (service *LayoutService) Save(ctx context.Context, userID uint, expectedRevision uint64, layout Layout) (StoredLayout, error) {
	normalized, err := NormalizeLayout(layout)
	if err != nil {
		return StoredLayout{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return StoredLayout{}, fmt.Errorf("marshal dashboard layout: %w", err)
	}

	if expectedRevision == 0 {
		record := gbmodels.GbDashboardLayout{
			UserID: userID, DashboardKey: homeDashboardKey,
			SchemaVersion: normalized.SchemaVersion, Revision: 1, LayoutJSON: string(payload),
		}
		err = service.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error
		if err != nil {
			return StoredLayout{}, err
		}
		if record.ID == 0 {
			return StoredLayout{}, ErrLayoutRevisionConflict
		}
		return StoredLayout{Revision: 1, Layout: normalized}, nil
	}

	result := service.db.WithContext(ctx).Model(&gbmodels.GbDashboardLayout{}).
		Where("user_id = ? AND dashboard_key = ? AND revision = ?", userID, homeDashboardKey, expectedRevision).
		Updates(map[string]any{
			"schema_version": normalized.SchemaVersion,
			"layout_json":    string(payload),
			"revision":       gorm.Expr("revision + 1"),
		})
	if result.Error != nil {
		return StoredLayout{}, result.Error
	}
	if result.RowsAffected != 1 {
		return StoredLayout{}, ErrLayoutRevisionConflict
	}
	return StoredLayout{Revision: expectedRevision + 1, Layout: normalized}, nil
}

func (service *LayoutService) Reset(ctx context.Context, userID uint) (StoredLayout, error) {
	err := service.db.WithContext(ctx).
		Where("user_id = ? AND dashboard_key = ?", userID, homeDashboardKey).
		Delete(&gbmodels.GbDashboardLayout{}).Error
	if err != nil {
		return StoredLayout{}, err
	}
	return StoredLayout{Layout: DefaultLayout()}, nil
}
