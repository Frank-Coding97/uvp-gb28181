package playauth

import "context"

// DeviceCleanupTarget is a discovery snapshot, not permission to act. Every
// recovery page must revalidate the exact PK/code and target epoch in SQL.
type DeviceCleanupTarget struct {
	DevicePK int64
	DeviceCleanupState
}

type DeviceCleanupDiscovery struct {
	Targets []DeviceCleanupTarget
	NextPK  int64
	Invalid int
}

// DiscoverPending returns at most 100 raw rows in PK order. Malformed rows
// produce an error but retain the raw page cursor and independently valid
// targets, so a poisoned first row cannot permanently starve later devices.
// A database/query failure returns no cursor. Neither case proves coverage.
func (s *DeviceCleanupStore) DiscoverPending(ctx context.Context, afterPK, throughPK int64, limit int) (DeviceCleanupDiscovery, error) {
	var page DeviceCleanupDiscovery
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil || afterPK < 0 || throughPK < afterPK {
		return page, ErrDeviceCleanupUnavailable
	}
	if limit <= 0 {
		limit = 32
	}
	if limit > 100 {
		limit = 100
	}
	var rows []deviceCleanupRow
	err := s.db.WithContext(ctx).Table("gb_device").
		Select("id, device_id, access_epoch, cleanup_completed_epoch").
		Where("id > ? AND id <= ? AND deleted_at IS NULL AND cleanup_completed_epoch < access_epoch", afterPK, throughPK).
		Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return page, ErrDeviceCleanupUnavailable
	}
	for _, row := range rows {
		page.NextPK = row.ID
		state, err := validateDeviceCleanupRow(row)
		if err != nil {
			page.Invalid++
			continue
		}
		page.Targets = append(page.Targets, DeviceCleanupTarget{DevicePK: row.ID, DeviceCleanupState: state})
	}
	if page.Invalid != 0 {
		return page, ErrDeviceCleanupUnavailable
	}
	return page, nil
}

// PendingUpperBound fixes the finite device boundary for one cursor round.
// Devices inserted beyond this PK are discovered in a later round instead
// of extending the current round indefinitely.
func (s *DeviceCleanupStore) PendingUpperBound(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil || ctx == nil || ctx.Err() != nil {
		return 0, ErrDeviceCleanupUnavailable
	}
	var row struct{ UpperPK *int64 }
	err := s.db.WithContext(ctx).Table("gb_device").Select("MAX(id) AS upper_pk").
		Where("deleted_at IS NULL AND cleanup_completed_epoch < access_epoch").Scan(&row).Error
	if err != nil {
		return 0, ErrDeviceCleanupUnavailable
	}
	if row.UpperPK == nil {
		return 0, nil
	}
	if *row.UpperPK <= 0 {
		return 0, ErrDeviceCleanupUnavailable
	}
	return *row.UpperPK, nil
}
