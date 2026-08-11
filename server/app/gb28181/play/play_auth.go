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

type authorizationLifecycle interface {
	preparedTokenIssuer
	BindAuthorization(string, uint64) error
	TerminateMediaGeneration(uint64) int
}

type autoOnDemandReadiness interface {
	IsAutoOnDemandReady(int64) bool
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

// AuthorizeFixedPlayback creates a short-lived, node-bound URL for a fixed
// stream without opening RTP, sending INVITE, or creating live ownership.
func (s *Service) AuthorizeFixedPlayback(ctx context.Context, deviceID, channelID, clientIP string) (*Result, error) {
	settings := gbconfig.CurrentFixedAddressPlaybackSettings()
	authSettings := gbconfig.CurrentPlayAuthSettings()
	if !settings.FixedAddressEnabled || !settings.AutoOnDemandEnabled || !authSettings.Enabled || !s.useMultiNode() {
		return nil, ErrPlayAuthorizationUnavailable
	}
	issuer, ok := s.tokenIssuer.(authorizationLifecycle)
	if !ok || issuer == nil || s.urlResolver == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if authSettings.BindClientIP {
		if _, err := playauth.NormalizeClientIP(clientIP); err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
	}
	prepared, err := issuer.Prepare()
	if err != nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	streamID, err := FixedStreamID(deviceID, channelID)
	if err != nil {
		return nil, err
	}
	pickedNode, err := s.picker.Pick(ctx, PickContext{DeviceID: deviceID, ChannelID: channelID, StreamID: streamID})
	if err != nil || pickedNode == nil || pickedNode.ID == 0 {
		return nil, ErrPlayAuthorizationUnavailable
	}
	mediaNode, ok := s.registry.Get(pickedNode.ID)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID == "" || !mediaNode.IsActive() || mediaNode.IsNearCapacity() {
		return nil, ErrPlayAuthorizationUnavailable
	}
	readiness, ok := s.registry.(autoOnDemandReadiness)
	if !ok || !readiness.IsAutoOnDemandReady(mediaNode.ID) {
		return nil, ErrPlayAuthorizationUnavailable
	}
	urls, warnings := s.urlResolver.Resolve(ctx, mediaNode, zlmApp, streamID)
	if len(urls.AsMap()) == 0 {
		return nil, ErrPlayAuthorizationUnavailable
	}
	grant, err := issuer.Bind(prepared, playauth.Binding{
		DeviceID: deviceID, ChannelID: channelID, App: zlmApp,
		Stream: streamID, MediaServerID: mediaNode.MediaServerUUID,
		BindClientIP: authSettings.BindClientIP, ClientIP: clientIP,
	})
	if err != nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	urls, err = playauth.DecorateURLs(urls, grant.Token)
	if err != nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	result := &Result{
		StreamID: streamID, App: zlmApp,
		Node: &ResultNode{ID: mediaNode.ID, Name: mediaNode.Name, Host: mediaNode.Host},
		URLs: urls, URLWarnings: warnings,
		AuthorizationExpiresAt: grant.ExpiresAt.Unix(), ModeAtStart: LiveModeFixed,
		AuthorizationCorrelationID: playauth.CorrelationID(grant.AuthorizationGeneration),
	}
	result.WSFlvURL = valueOrEmpty(urls.WSFLV)
	result.HTTPFlvURL = valueOrEmpty(urls.HTTPFLV)
	result.HLSURL = valueOrEmpty(urls.HLS)
	ApplyPlaybackSelection(result, gbconfig.CurrentDefaultPlaybackProtocol(), false)
	return result, nil
}

func (s *Service) bindAuthorization(authorizationID string, generation uint64) error {
	lifecycle, ok := s.tokenIssuer.(authorizationLifecycle)
	if !ok || lifecycle == nil {
		return ErrPlayAuthorizationUnavailable
	}
	return lifecycle.BindAuthorization(authorizationID, generation)
}

func (s *Service) terminateAuthorizationGeneration(generation uint64) {
	if lifecycle, ok := s.tokenIssuer.(authorizationLifecycle); ok && lifecycle != nil {
		lifecycle.TerminateMediaGeneration(generation)
	}
}

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
	result.AuthorizationCorrelationID = playauth.CorrelationID(grant.AuthorizationGeneration)
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
	result.AuthorizationCorrelationID = playauth.CorrelationID(grant.AuthorizationGeneration)
	ApplyPlaybackSelection(result, result.DefaultProtocol, false)
	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
