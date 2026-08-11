package play

import (
	"context"
	"errors"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type preparedTokenIssuer interface {
	playauth.DirectIssuer
	Prepare() (playauth.Prepared, error)
	Bind(playauth.Prepared, playauth.Binding) (playauth.Grant, error)
}

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

func (s *Service) resultForCaller(_ Request, result *Result) (*Result, error) {
	return cloneResult(result), nil
}

var (
	ErrPlayAuthorizationUnavailable     = errors.New("play authorization unavailable")
	ErrFixedPlaybackRequiresManagedNode = errors.New("fixed playback requires a managed ZLM node")
)

// StartAuthorized issues a fresh authorization for every explicit play call.
// Randomness and optional client-IP validation happen before media side effects.
func (s *Service) StartAuthorized(ctx context.Context, deviceID, channelID, clientIP string) (*Result, error) {
	settings := gbconfig.CurrentPlayAuthSettings()
	if !settings.Enabled {
		return s.EnsureLive(ctx, Request{DeviceID: deviceID, ChannelID: channelID, Trigger: "explicit"})
	}
	issuer, ok := s.tokenIssuer.(preparedTokenIssuer)
	if !ok || issuer == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if settings.BindClientIP {
		if _, err := playauth.NormalizeClientIP(clientIP); err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
	}
	prepared, err := issuer.Prepare()
	if err != nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	result, err := s.EnsureLive(ctx, Request{DeviceID: deviceID, ChannelID: channelID, Trigger: "explicit"})
	if err != nil {
		return nil, err
	}
	if err := s.authorizePreparedResult(result, deviceID, channelID, clientIP, settings, issuer, prepared); err != nil {
		if result != nil && !result.Reused {
			ref := stream.LiveRef{StreamID: result.StreamID, SSRC: result.SSRC, Generation: result.Generation}
			if result.Node != nil {
				ref.NodeID = result.Node.ID
			}
			_, _ = s.coordinator().StopIfCurrent(context.WithoutCancel(ctx), ref)
		}
		return nil, err
	}
	return result, nil
}

func (s *Service) authorizePreparedResult(
	result *Result,
	deviceID, channelID, clientIP string,
	settings gbconfig.PlayAuthSettings,
	issuer preparedTokenIssuer,
	prepared playauth.Prepared,
) error {
	if result == nil || result.Node == nil || s.registry == nil {
		return ErrPlayAuthorizationUnavailable
	}
	mediaNode, ok := s.registry.Get(result.Node.ID)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID == "" {
		return ErrPlayAuthorizationUnavailable
	}
	binding, err := s.ResolvePlaybackMediaContext(result.App, result.StreamID, mediaNode.MediaServerUUID)
	if err != nil || binding.DeviceID != deviceID || binding.ChannelID != channelID {
		return ErrPlayAuthorizationUnavailable
	}
	binding.BindClientIP = settings.BindClientIP
	binding.ClientIP = clientIP
	grant, err := issuer.Bind(prepared, binding)
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
	result.AuthorizationExpiresAt = grant.ExpiresAt.Unix()
	ApplyPlaybackSelection(result, result.DefaultProtocol, false)
	return nil
}

// authorizeFixedResult remains as a narrow compatibility helper for unit
// tests; production explicit playback uses StartAuthorized for both modes.
func (s *Service) authorizeFixedResult(result *Result, deviceID, channelID string, mediaNode *node.Node) error {
	if result == nil || result.ModeAtStart != LiveModeFixed {
		return nil
	}
	issuer, ok := s.tokenIssuer.(preparedTokenIssuer)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID == "" {
		return ErrPlayAuthorizationUnavailable
	}
	prepared, err := issuer.Prepare()
	if err != nil {
		return ErrPlayAuthorizationUnavailable
	}
	grant, err := issuer.Bind(prepared, playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: result.App,
		Stream: result.StreamID, MediaServerID: mediaNode.MediaServerUUID,
		MediaGeneration: result.Generation,
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
	result.AuthorizationExpiresAt = grant.ExpiresAt.Unix()
	ApplyPlaybackSelection(result, result.DefaultProtocol, false)
	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
