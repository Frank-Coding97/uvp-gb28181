package models

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockGBDeviceForMaintenance loads one parent device while holding the
// database row lock used by device-scoped maintenance operations. Keeping the
// helper in models makes firmware upgrade and TeleBoot share the same lock
// boundary even though their state machines live in separate packages.
func LockGBDeviceForMaintenance(tx *gorm.DB, deviceID uint, deviceCode string) (GbDevice, error) {
	var device GbDevice
	if tx == nil || deviceID == 0 {
		return device, gorm.ErrRecordNotFound
	}
	query := tx.Model(&GbDevice{}).Where("id = ?", deviceID)
	switch strings.ToLower(tx.Dialector.Name()) {
	case "mysql", "postgres", "postgresql":
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	case "sqlserver":
		result := tx.Raw("SELECT * FROM gb_device WITH (UPDLOCK,HOLDLOCK,ROWLOCK) WHERE id = ?", deviceID).Scan(&device)
		if result.Error != nil {
			return device, result.Error
		}
		if device.ID == 0 {
			return device, gorm.ErrRecordNotFound
		}
		if strings.TrimSpace(deviceCode) != "" && strings.TrimSpace(device.DeviceID) != strings.TrimSpace(deviceCode) {
			return device, fmt.Errorf("maintenance device code mismatch")
		}
		return device, nil
	}
	result := query.Limit(1).Find(&device)
	if result.Error != nil {
		return device, result.Error
	}
	if result.RowsAffected == 0 {
		return device, gorm.ErrRecordNotFound
	}
	if strings.TrimSpace(deviceCode) != "" && strings.TrimSpace(device.DeviceID) != strings.TrimSpace(deviceCode) {
		return device, fmt.Errorf("maintenance device code mismatch")
	}
	return device, nil
}
