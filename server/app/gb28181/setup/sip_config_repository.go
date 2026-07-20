package setup

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type SIPConfigRepository struct {
	db *gorm.DB
}

func NewSIPConfigRepository(db *gorm.DB) *SIPConfigRepository {
	return &SIPConfigRepository{db: db}
}

func (r *SIPConfigRepository) WithDB(db *gorm.DB) *SIPConfigRepository {
	return NewSIPConfigRepository(db)
}

func (r *SIPConfigRepository) Get(ctx context.Context) (*SIPConfig, error) {
	var row SIPConfig
	err := r.db.WithContext(ctx).Where("id = ?", SingletonID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *SIPConfigRepository) Save(ctx context.Context, row *SIPConfig) error {
	row.ID = SingletonID
	return r.db.WithContext(ctx).Save(row).Error
}
