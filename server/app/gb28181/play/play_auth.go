package play

import (
	"context"
	"errors"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type preparedTokenIssuer interface {
	playauth.ContextDirectIssuer
	playauth.ContextPreparedIssuer
	AuthorizeDeviceEpochContext(context.Context, string, int64) error
}

type authorizationLifecycle interface {
	preparedTokenIssuer
	ValidateQueuedAuthorizationContext(context.Context, playauth.QueuedAuthorization) error
	BindAuthorizationContext(context.Context, playauth.QueuedAuthorization, uint64) error
	TerminateMediaGeneration(uint64) int
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

// AuthorizeFixedPlayback resolves a fixed URL without opening RTP, sending
// INVITE, or creating live ownership. Playback auth decorates it when enabled.
func (s *Service) AuthorizeFixedPlayback(ctx context.Context, req AuthorizedRequest) (*Result, error) {
	if s.operationBarrierRequired && s.operationBarrier == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	deviceID, channelID, clientIP := req.DeviceID, req.ChannelID, req.ClientIP
	settings := gbconfig.CurrentFixedAddressPlaybackSettings()
	authSettings := gbconfig.CurrentPlayAuthSettings()
	if s.operationBarrier != nil {
		if err := s.operationBarrier.AuthorizeEpoch(ctx, deviceID, req.DeviceEpoch); err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
	}
	if !settings.FixedAddressEnabled || !s.useMultiNode() {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if s.urlResolver == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	var issuer authorizationLifecycle
	var prepared playauth.Prepared
	var err error
	if authSettings.Enabled {
		var ok bool
		issuer, ok = s.tokenIssuer.(authorizationLifecycle)
		if !ok || issuer == nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
		if err := issuer.AuthorizeDeviceEpochContext(ctx, deviceID, req.DeviceEpoch); err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
		prepared, err = issuer.Prepare()
		if err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
	}
	if authSettings.Enabled && authSettings.BindClientIP {
		if _, err := playauth.NormalizeClientIP(clientIP); err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
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
	urls, warnings := s.urlResolver.Resolve(ctx, mediaNode, zlmApp, streamID)
	if len(urls.AsMap()) == 0 {
		return nil, ErrPlayAuthorizationUnavailable
	}
	result := &Result{
		StreamID: streamID, App: zlmApp,
		Node: &ResultNode{ID: mediaNode.ID, Name: mediaNode.Name, Host: mediaNode.Host},
		URLs: urls, URLWarnings: warnings,
		ModeAtStart: LiveModeFixed,
	}
	if authSettings.Enabled {
		grant, err := issuer.BindContext(ctx, prepared, playauth.Binding{
			DeviceID: deviceID, ChannelID: channelID, App: zlmApp,
			DeviceEpoch: req.DeviceEpoch,
			Stream:      streamID, MediaServerID: mediaNode.MediaServerUUID,
			BindClientIP: authSettings.BindClientIP, ClientIP: clientIP,
		})
		if err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
		urls, err = playauth.DecorateURLs(urls, grant.Token)
		if err != nil {
			return nil, ErrPlayAuthorizationUnavailable
		}
		result.URLs = urls
		result.AuthorizationExpiresAt = grant.ExpiresAt.Unix()
		result.AuthorizationCorrelationID = playauth.CorrelationID(grant.AuthorizationGeneration)
	}
	result.WSFlvURL = valueOrEmpty(urls.WSFLV)
	result.HTTPFlvURL = valueOrEmpty(urls.HTTPFLV)
	result.HLSURL = valueOrEmpty(urls.HLS)
	ApplyPlaybackSelection(result, gbconfig.CurrentDefaultPlaybackProtocol(), false)
	return result, nil
}

// queuedAuthorization derives the expected resource from the dispatch target,
// not from mutable authorization data or a newly loaded device epoch.
func (s *Service) queuedAuthorization(req Request) (playauth.QueuedAuthorization, error) {
	if req.AuthorizationID == "" || req.RequiredNode <= 0 || s.registry == nil ||
		!gbconfig.CurrentFixedAddressPlaybackSettings().FixedAddressEnabled {
		return playauth.QueuedAuthorization{}, ErrPlayAuthorizationUnavailable
	}
	streamID, err := FixedStreamID(req.DeviceID, req.ChannelID)
	if err != nil {
		return playauth.QueuedAuthorization{}, ErrPlayAuthorizationUnavailable
	}
	mediaNode, ok := s.registry.Get(req.RequiredNode)
	if !ok || mediaNode == nil || mediaNode.MediaServerUUID == "" {
		return playauth.QueuedAuthorization{}, ErrPlayAuthorizationUnavailable
	}
	return playauth.QueuedAuthorization{
		AuthorizationGeneration: req.AuthorizationID,
		DeviceID:                req.DeviceID, ChannelID: req.ChannelID, DeviceEpoch: req.DeviceEpoch,
		App: zlmApp, Stream: streamID, MediaServerID: mediaNode.MediaServerUUID,
	}, nil
}

func (s *Service) validateQueuedAuthorization(ctx context.Context, req Request) error {
	lifecycle, ok := s.tokenIssuer.(authorizationLifecycle)
	if !ok || lifecycle == nil {
		return ErrPlayAuthorizationUnavailable
	}
	queued, err := s.queuedAuthorization(req)
	if err != nil {
		return err
	}
	return lifecycle.ValidateQueuedAuthorizationContext(ctx, queued)
}

func (s *Service) bindAuthorization(ctx context.Context, req Request, generation uint64) error {
	lifecycle, ok := s.tokenIssuer.(authorizationLifecycle)
	if !ok || lifecycle == nil {
		return ErrPlayAuthorizationUnavailable
	}
	queued, err := s.queuedAuthorization(req)
	if err != nil {
		return err
	}
	return lifecycle.BindAuthorizationContext(ctx, queued, generation)
}

func (s *Service) bindResultAuthorization(ctx context.Context, req Request, result *Result) error {
	queued, err := s.queuedAuthorization(req)
	if err != nil || result == nil || result.Generation == 0 || result.Node == nil ||
		result.Node.ID != req.RequiredNode || result.App != queued.App || result.StreamID != queued.Stream {
		return ErrPlayAuthorizationUnavailable
	}
	lifecycle, ok := s.tokenIssuer.(authorizationLifecycle)
	if !ok || lifecycle == nil {
		return ErrPlayAuthorizationUnavailable
	}
	return lifecycle.BindAuthorizationContext(ctx, queued, result.Generation)
}

func (s *Service) terminateAuthorizationGeneration(generation uint64) {
	if lifecycle, ok := s.tokenIssuer.(authorizationLifecycle); ok && lifecycle != nil {
		lifecycle.TerminateMediaGeneration(generation)
	}
}

// StartAuthorized issues a fresh authorization for every explicit play call.
// Randomness and optional client-IP validation happen before media side effects.
func (s *Service) StartAuthorized(ctx context.Context, req AuthorizedRequest) (*Result, error) {
	deviceID, channelID, clientIP := req.DeviceID, req.ChannelID, req.ClientIP
	settings := gbconfig.CurrentPlayAuthSettings()
	if !settings.Enabled {
		return s.EnsureLive(ctx, Request{DeviceID: deviceID, ChannelID: channelID, DeviceEpoch: req.DeviceEpoch, Trigger: "explicit"})
	}
	issuer, ok := s.tokenIssuer.(preparedTokenIssuer)
	if !ok || issuer == nil {
		return nil, ErrPlayAuthorizationUnavailable
	}
	if err := issuer.AuthorizeDeviceEpochContext(ctx, deviceID, req.DeviceEpoch); err != nil {
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
	result, err := s.EnsureLive(ctx, Request{DeviceID: deviceID, ChannelID: channelID, DeviceEpoch: req.DeviceEpoch, Trigger: "explicit"})
	if err != nil {
		return nil, err
	}
	if err := s.authorizePreparedResult(ctx, result, req, settings, issuer, prepared); err != nil {
		// The generation may already serve another authorized caller even
		// when this request created it. Caller denial is not media ownership;
		// transfer cleanup and actual start failure compensation remain separate.
		return nil, err
	}
	return result, nil
}

func (s *Service) authorizePreparedResult(
	ctx context.Context,
	result *Result,
	req AuthorizedRequest,
	settings gbconfig.PlayAuthSettings,
	issuer preparedTokenIssuer,
	prepared playauth.Prepared,
) error {
	deviceID, channelID, clientIP := req.DeviceID, req.ChannelID, req.ClientIP
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
	binding.DeviceEpoch = req.DeviceEpoch
	grant, err := issuer.BindContext(ctx, prepared, binding)
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
func (s *Service) authorizeFixedResult(ctx context.Context, result *Result, req AuthorizedRequest, mediaNode *node.Node) error {
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
	grant, err := issuer.BindContext(ctx, prepared, playauth.Binding{
		DeviceID: req.DeviceID, ChannelID: req.ChannelID, DeviceEpoch: req.DeviceEpoch, App: result.App,
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
