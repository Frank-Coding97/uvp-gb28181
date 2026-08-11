package play

import (
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	ErrPlayAuthorizationUnavailable     = errors.New("play authorization unavailable")
	ErrFixedPlaybackRequiresManagedNode = errors.New("fixed playback requires a managed ZLM node")
)

func (s *Service) authorizeFixedResult(result *Result, deviceID, channelID string, mediaNode *node.Node) error {
	if result == nil || result.ModeAtStart != LiveModeFixed {
		return nil
	}
	if s.tokenIssuer == nil {
		return ErrPlayAuthorizationUnavailable
	}
	if mediaNode == nil || mediaNode.MediaServerUUID == "" {
		return ErrFixedPlaybackRequiresManagedNode
	}
	grant, err := s.tokenIssuer.IssueDirect(playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: result.App,
		Stream: result.StreamID, MediaServerID: mediaNode.MediaServerUUID,
	})
	if err != nil {
		return ErrPlayAuthorizationUnavailable
	}
	urls, err := playauth.DecorateURLs(result.URLs, grant.Token)
	if err != nil {
		return err
	}
	result.URLs = urls
	result.WSFlvURL = valueOrEmpty(urls.WSFLV)
	result.HTTPFlvURL = valueOrEmpty(urls.HTTPFLV)
	result.HLSURL = valueOrEmpty(urls.HLS)
	ApplyPlaybackSelection(result, result.DefaultProtocol, false)
	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
