package play

import (
	"errors"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func cloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.URLs = PlaybackURLsFromMap(result.URLs.AsMap())
	cloned.URLWarnings = append([]string(nil), result.URLWarnings...)
	if result.Node != nil {
		nodeCopy := *result.Node
		cloned.Node = &nodeCopy
	}
	return &cloned
}

func (s *Service) resultForCaller(req Request, result *Result) (*Result, error) {
	result = cloneResult(result)
	if result == nil || result.ModeAtStart != LiveModeFixed {
		return result, nil
	}
	if result.Node == nil || s.registry == nil {
		return nil, ErrFixedPlaybackRequiresManagedNode
	}
	mediaNode, ok := s.registry.Get(result.Node.ID)
	if !ok {
		return nil, ErrFixedPlaybackRequiresManagedNode
	}
	if err := s.authorizeFixedResult(result, req.DeviceID, req.ChannelID, mediaNode); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) preflightFixedResult(req Request, result *Result, mediaNode *node.Node) error {
	if result == nil || result.ModeAtStart != LiveModeFixed {
		return nil
	}
	copy := cloneResult(result)
	return s.authorizeFixedResult(copy, req.DeviceID, req.ChannelID, mediaNode)
}

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
