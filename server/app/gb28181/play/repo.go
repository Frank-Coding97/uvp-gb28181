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

func (gormChannelRepo) FindChannelByStream(ctx context.Context, streamID string) (*gbmodels.GbChannel, error) {
	return gbmodels.FindChannelByStreamID(ctx, streamID)
}

func (gormChannelRepo) UpdateStream(ctx context.Context, deviceID, channelID, streamID string) error {
	return gbmodels.UpdateChannelStream(ctx, deviceID, channelID, streamID)
}

func (gormChannelRepo) ClearStream(ctx context.Context, streamID string) error {
	return gbmodels.ClearChannelStream(ctx, streamID)
}

func (gormChannelRepo) SetCurrent(ctx context.Context, deviceID, channelID, streamID, ssrc string) error {
	return gbmodels.SetChannelCurrent(ctx, deviceID, channelID, streamID, ssrc)
}

func (gormChannelRepo) ClearIfCurrent(ctx context.Context, streamID, ssrc string) (bool, error) {
	return gbmodels.ClearChannelCurrentIfCurrent(ctx, streamID, ssrc)
}

func (gormChannelRepo) ListPlayingChannels(ctx context.Context) (gbmodels.GbChannelList, error) {
	return gbmodels.ListPlayingChannels(ctx)
}

// CurrentSSRCForChannel preserves compatibility with rows created before
// current_ssrc existed. Only a valid 10-digit legacy stream_id can be treated
// as its SSRC; fixed or otherwise malformed stream IDs never receive a guess.
func CurrentSSRCForChannel(ch *gbmodels.GbChannel) string {
	if ch == nil {
		return ""
	}
	if ch.CurrentSSRC != "" {
		return ch.CurrentSSRC
	}
	if len(ch.StreamID) != 10 {
		return ""
	}
	for _, digit := range ch.StreamID {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	return ch.StreamID
}

// NewChannelRepo 默认通道仓库(生产路径)
func NewChannelRepo() ChannelRepo { return gormChannelRepo{} }
