package play

import (
	"context"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// gormDeviceRepo 用 gbmodels 包级函数实现 DeviceRepo
type gormDeviceRepo struct{}

func (gormDeviceRepo) FindByDeviceID(ctx context.Context, deviceID string) (*gbmodels.GbDevice, error) {
	return gbmodels.FindByDeviceID(ctx, deviceID)
}

// NewDeviceRepo 默认设备仓库(生产路径)
func NewDeviceRepo() DeviceRepo { return gormDeviceRepo{} }

// gormChannelRepo 用 gbmodels 包级函数实现 ChannelRepo
type gormChannelRepo struct{}

func (gormChannelRepo) FindChannel(ctx context.Context, deviceID, channelID string) (*gbmodels.GbChannel, error) {
	return gbmodels.FindChannel(ctx, deviceID, channelID)
}

func (gormChannelRepo) UpdateStream(ctx context.Context, deviceID, channelID, streamID string) error {
	return gbmodels.UpdateChannelStream(ctx, deviceID, channelID, streamID)
}

func (gormChannelRepo) ClearStream(ctx context.Context, streamID string) error {
	return gbmodels.ClearChannelStream(ctx, streamID)
}

// NewChannelRepo 默认通道仓库(生产路径)
func NewChannelRepo() ChannelRepo { return gormChannelRepo{} }
