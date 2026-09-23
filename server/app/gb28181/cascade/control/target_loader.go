package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
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
		if channel.DeviceID != device.DeviceID {
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
