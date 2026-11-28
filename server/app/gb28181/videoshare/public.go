package videoshare

import (
	"context"
	"time"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type PublicShare struct {
	Name       string    `json:"name"`
	Online     bool      `json:"online"`
	HasAudio   bool      `json:"hasAudio"`
	ExpiresAt  time.Time `json:"expiresAt"`
	ServerTime time.Time `json:"serverTime"`
}

func (s *Service) ResolvePublic(ctx context.Context, publicID, signature string) (PublicShare, error) {
	share, err := s.Resolve(ctx, publicID, signature)
	if err != nil {
		return PublicShare{}, err
	}
	var channel gbmodels.GbChannel
	if err := s.db.WithContext(ctx).Select("id, name, status, audio_enabled").Where("id = ? AND deleted_at IS NULL", share.ChannelPK).Take(&channel).Error; err != nil {
		return PublicShare{}, err
	}
	if channel.ID == 0 {
		return PublicShare{}, ErrExpired
	}
	return PublicShare{Name: channel.Name, Online: channel.Status == gbmodels.ChannelStatusOnline, HasAudio: channel.AudioEnabled, ExpiresAt: *share.ExpiresAt, ServerTime: s.now().UTC()}, nil
}
