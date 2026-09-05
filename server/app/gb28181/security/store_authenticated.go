package security

import (
	"context"
	"errors"
	"strings"
	"time"
)

type authenticatedEndpointDeviceRow struct {
	DeviceID         string     `gorm:"column:device_id"`
	Transport        string     `gorm:"column:transport"`
	IP               string     `gorm:"column:ip"`
	RegisterTime     *time.Time `gorm:"column:register_time"`
	RegisterExpireAt *time.Time `gorm:"column:register_expire_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

func (s *GormStore) LoadAuthenticatedEndpoints(ctx context.Context) ([]Endpoint, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("security store unavailable")
	}

	var rows []authenticatedEndpointDeviceRow
	if err := s.db.WithContext(ctx).
		Table("gb_device").
		Select("device_id, transport, ip, register_time, register_expire_at, deleted_at").
		Where("deleted_at IS NULL").
		Where("register_time IS NOT NULL").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	now := time.Now()
	endpoints := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		if row.RegisterTime == nil || row.RegisterTime.IsZero() || row.RegisterTime.After(now) || !ValidDeviceID(row.DeviceID) {
			continue
		}

		ip, err := ValidateSource(row.IP)
		if err != nil {
			continue
		}
		transport := strings.ToUpper(strings.TrimSpace(row.Transport))
		if transport != "UDP" && transport != "TCP" && transport != "TLS" {
			continue
		}

		expiresAt := time.Unix(1, 0) // nonzero expired value; zero denotes unlimited trust
		if row.RegisterExpireAt != nil && !row.RegisterExpireAt.IsZero() {
			expiresAt = *row.RegisterExpireAt
		}
		endpoints = append(endpoints, Endpoint{
			DeviceID:  row.DeviceID,
			Transport: transport,
			Address:   ip.String(),
			ExpiresAt: expiresAt,
			UpdatedAt: *row.RegisterTime,
		})
	}
	return endpoints, nil
}
