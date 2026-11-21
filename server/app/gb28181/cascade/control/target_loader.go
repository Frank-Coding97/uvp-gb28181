package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
)

var ErrSourceTargetNotFound = errors.New("cascade control: source target not found")

type GormTargetLoader struct {
	db *gorm.DB
}

func NewGormTargetLoader(db *gorm.DB) *GormTargetLoader {
	return &GormTargetLoader{db: db}
}

func (l *GormTargetLoader) Load(ctx context.Context, sourceDeviceID, sourceChannelID uint64) (ptz.Target, error) {
	if l == nil || l.db == nil || sourceDeviceID == 0 || sourceChannelID == 0 {
		return ptz.Target{}, ErrSourceTargetNotFound
	}
	var device gbmodels.GbDevice
	var channel gbmodels.GbChannel
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&channel, sourceChannelID).Error; err != nil {
			return err
		}
		if result := tx.Limit(1).Find(&device, sourceDeviceID); result.Error != nil {
			return result.Error
		} else if result.RowsAffected == 0 {
			return ErrSourceTargetNotFound
		}
		if channel.DeviceID != device.DeviceID || channel.OwnerDeptID != device.OwnerDeptID {
			return ErrSourceTargetNotFound
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, ErrSourceTargetNotFound) {
		return ptz.Target{}, ErrSourceTargetNotFound
	}
	if err != nil {
		return ptz.Target{}, fmt.Errorf("load cascade PTZ source target: %w", err)
	}
	version := protocol.Version2016
	if strings.TrimSpace(device.EffectiveVersion) == protocol.Version2022 {
		version = protocol.Version2022
	}
	return ptz.Target{
		DeviceID: uint(device.ID), DeviceCode: device.DeviceID,
		ChannelID: uint(channel.ID), ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport,
		DeviceOnline:  device.Status == gbmodels.DeviceStatusOnline,
		ChannelOnline: channel.Status == gbmodels.ChannelStatusOnline,
		Profile:       protocol.ProfileFor(protocol.Version(version)),
	}, nil
}

// LoadAuthorized binds the source identity to the current, fully cleaned-up
// device epoch. Missing security state must never select the legacy send path.
func (l *GormTargetLoader) LoadAuthorized(ctx context.Context, sourceDeviceID, sourceChannelID uint64) (ptz.Target, error) {
	target, err := l.Load(ctx, sourceDeviceID, sourceChannelID)
	if err != nil {
		return ptz.Target{}, err
	}
	var epoch struct{ AccessEpoch, CleanupCompletedEpoch *int64 }
	err = l.db.WithContext(ctx).Clauses(dbresolver.Write).Table("gb_device").
		Select("gb_device.access_epoch, gb_device.cleanup_completed_epoch").
		Joins("JOIN gb_channel ON gb_channel.device_id = gb_device.device_id AND gb_channel.owner_dept_id = gb_device.owner_dept_id").
		Where("gb_device.id = ? AND gb_device.device_id = ? AND gb_channel.id = ? AND gb_channel.channel_id = ? AND gb_device.deleted_at IS NULL AND gb_channel.deleted_at IS NULL",
			target.DeviceID, target.DeviceCode, target.ChannelID, target.ChannelCode).Take(&epoch).Error
	if err != nil {
		return ptz.Target{}, fmt.Errorf("load cascade source authorization: %w", err)
	}
	if epoch.AccessEpoch == nil || epoch.CleanupCompletedEpoch == nil || *epoch.AccessEpoch <= 0 || *epoch.AccessEpoch != *epoch.CleanupCompletedEpoch {
		return ptz.Target{}, fmt.Errorf("%w: source authorization is not ready", ErrControlUnauthorized)
	}
	target.DeviceEpoch = *epoch.AccessEpoch
	return target, nil
}
