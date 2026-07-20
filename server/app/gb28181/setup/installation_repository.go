package setup

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type InstallationRepository struct {
	db *gorm.DB
}

func NewInstallationRepository(db *gorm.DB) *InstallationRepository {
	return &InstallationRepository{db: db}
}

func (r *InstallationRepository) WithDB(db *gorm.DB) *InstallationRepository {
	return NewInstallationRepository(db)
}

func (r *InstallationRepository) Get(ctx context.Context) (*SystemInstallation, error) {
	var row SystemInstallation
	err := r.db.WithContext(ctx).Where("id = ?", SingletonID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *InstallationRepository) Create(ctx context.Context, row *SystemInstallation) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *InstallationRepository) Save(ctx context.Context, row *SystemInstallation) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *InstallationRepository) SetInstanceIDIfEmpty(ctx context.Context, instanceID string) error {
	return r.db.WithContext(ctx).
		Model(&SystemInstallation{}).
		Where("id = ? AND instance_id = ''", SingletonID).
		Update("instance_id", instanceID).Error
}
