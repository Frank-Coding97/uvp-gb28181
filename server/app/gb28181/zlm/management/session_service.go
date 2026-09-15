package management

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type SessionRuntimeReader interface {
	StreamRuntimeReader
	GetAllSessions(context.Context, int64, zlm.SessionFilter) ([]zlm.Session, error)
}

type NetworkSessionListRequest struct {
	NodeID int64             `json:"nodeId"`
	Filter zlm.SessionFilter `json:"filter"`
	Page   PageRequest       `json:"page"`
}

type NetworkSession struct {
	NodeID     int64  `json:"nodeId"`
	NodeUUID   string `json:"nodeUuid"`
	ID         string `json:"id"`
	PeerIP     string `json:"peerIp"`
	PeerPort   int    `json:"peerPort"`
	LocalIP    string `json:"localIp"`
	LocalPort  int    `json:"localPort"`
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	TypeID     string `json:"typeId"`
	// Media is intentionally omitted. A network session is not a media
	// viewer, and ZLM's getAllSession response does not prove a media tuple.
}

type NetworkSessionPage struct {
	Page[NetworkSession]
	NodeID   int64     `json:"nodeId"`
	NodeUUID string    `json:"nodeUuid"`
	AsOf     time.Time `json:"asOf"`
}

type MediaViewerListRequest struct {
	NodeID int64         `json:"nodeId"`
	Media  MediaIdentity `json:"media"`
	Page   PageRequest   `json:"page"`
}

type KickSessionRequest struct {
	NodeID     int64         `json:"nodeId"`
	Media      MediaIdentity `json:"media"`
	Identifier string        `json:"identifier"`
}

type KickSessionResult struct {
	NodeID              int64         `json:"nodeId"`
	Media               MediaIdentity `json:"media"`
	Identifier          string        `json:"identifier"`
	Kicked              bool          `json:"kicked"`
	AlreadyDisconnected bool          `json:"alreadyDisconnected"`
	Uncertain           bool          `json:"uncertain"`
	Retryable           bool          `json:"retryable"`
}

type SessionServiceDependencies struct {
	Registry     StreamNodeRegistry
	Runtime      SessionRuntimeReader
	Fresh        FreshMediaReader
	NodeExecutor *NodeExecutor
	Executor     MediaActionExecutor
}

type SessionServiceOption func(*SessionService)

func WithSessionOperationTimeout(timeout time.Duration) SessionServiceOption {
	return func(service *SessionService) {
		if timeout > 0 && timeout <= DefaultStreamOperationTimeout {
			service.operationTimeout = timeout
		}
	}
}

type SessionService struct {
	registry         StreamNodeRegistry
	runtime          SessionRuntimeReader
	fresh            FreshMediaReader
	executor         MediaActionExecutor
	operationTimeout time.Duration
}

func NewSessionService(dependencies SessionServiceDependencies, options ...SessionServiceOption) *SessionService {
	service := &SessionService{
		registry:         dependencies.Registry,
		runtime:          dependencies.Runtime,
		fresh:            dependencies.Fresh,
		executor:         dependencies.Executor,
		operationTimeout: DefaultStreamOperationTimeout,
	}
	if service.executor == nil && dependencies.NodeExecutor != nil {
		service.executor = NewNodeMediaExecutor(dependencies.NodeExecutor)
	}
	if service.fresh == nil {
		if runtime, ok := dependencies.Runtime.(*RuntimeReader); ok && runtime != nil {
			service.fresh = runtimeFreshMediaReader{reader: runtime}
		}
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (s *SessionService) ListNetworkSessions(ctx context.Context, request NetworkSessionListRequest) (NetworkSessionPage, error) {
	result := NetworkSessionPage{NodeID: request.NodeID, AsOf: time.Now().UTC()}
	if err := validateNodeID(request.NodeID); err != nil {
		return result, err
	}
	if err := validateSessionFilter(request.Filter); err != nil {
		return result, err
	}
	current, err := s.activeNode(request.NodeID)
	if err != nil {
		return result, err
	}
	result.NodeUUID = current.MediaServerUUID
	if s == nil || s.runtime == nil {
		return result, NewInternalError(nodeIDString(request.NodeID), "session runtime reader is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	sessions, err := s.runtime.GetAllSessions(opCtx, request.NodeID, request.Filter)
	if err != nil {
		return result, normalizeRuntimeReadError(err, request.NodeID)
	}
	sessions = boundedPageInput(sessions)
	items := make([]NetworkSession, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, NetworkSession{
			NodeID: request.NodeID, NodeUUID: current.MediaServerUUID,
			ID: safeRuntimeLabel(session.ID), PeerIP: safeRuntimeLabel(session.PeerIP), PeerPort: session.PeerPort,
			LocalIP: safeRuntimeLabel(session.LocalIP), LocalPort: session.LocalPort,
			Identifier: safeRuntimeLabel(session.Identifier), Type: safeRuntimeLabel(session.Type), TypeID: safeRuntimeLabel(session.TypeID),
		})
	}
	result.Page = Paginate(items, request.Page)
	return result, nil
}

func (s *SessionService) ListMediaViewers(ctx context.Context, request MediaViewerListRequest) (StreamViewerPage, error) {
	if err := validateNodeID(request.NodeID); err != nil {
		return StreamViewerPage{NodeID: request.NodeID, Target: request.Media}, err
	}
	if err := request.Media.Validate(); err != nil {
		return StreamViewerPage{NodeID: request.NodeID, Target: request.Media}, err
	}
	return s.listMediaViewers(ctx, request.NodeID, request.Media, request.Page)
}

func (s *SessionService) listMediaViewers(ctx context.Context, nodeID int64, media MediaIdentity, request PageRequest) (StreamViewerPage, error) {
	result := StreamViewerPage{NodeID: nodeID, Target: media, AsOf: time.Now().UTC()}
	current, err := s.activeNode(nodeID)
	if err != nil {
		return result, err
	}
	result.NodeUUID = current.MediaServerUUID
	if s == nil || s.runtime == nil {
		return result, NewInternalError(nodeIDString(nodeID), "session runtime reader is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	players, err := s.runtime.GetMediaPlayerList(opCtx, nodeID, toStreamTarget(media))
	if err != nil {
		return result, normalizeRuntimeReadError(err, nodeID)
	}
	players = boundedPageInput(players)
	viewers := make([]StreamViewer, 0, len(players))
	for _, player := range players {
		viewers = append(viewers, streamViewer(current, media, player))
	}
	result.Page = Paginate(viewers, request)
	return result, nil
}

func (s *SessionService) KickSession(ctx context.Context, request KickSessionRequest) (KickSessionResult, error) {
	result := KickSessionResult{NodeID: request.NodeID, Media: request.Media, Identifier: request.Identifier}
	if err := validateNodeID(request.NodeID); err != nil {
		return result, err
	}
	if err := request.Media.Validate(); err != nil {
		return result, err
	}
	if err := validateSessionIdentifier(request.Identifier); err != nil {
		return result, err
	}
	if _, err := s.activeNode(request.NodeID); err != nil {
		return result, err
	}
	if s == nil || s.fresh == nil || s.executor == nil {
		return result, NewInternalError(nodeIDString(request.NodeID), "session kick dependencies are not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	players, mediaPresent, err := s.freshPlayers(opCtx, request.NodeID, request.Media)
	if err != nil {
		return result, err
	}
	if !mediaPresent || !containsViewer(players, request.Identifier) {
		result.AlreadyDisconnected = true
		return result, nil
	}
	var targetPlayer *zlm.MediaPlayer
	for i := range players {
		if players[i].Identifier == request.Identifier {
			targetPlayer = &players[i]
			break
		}
	}
	if targetPlayer == nil || !viewerKickable(*targetPlayer) {
		return result, NewValidationError(map[string]string{"identifier": "session is not kickable"})
	}
	actionErr := s.executor.KickSession(opCtx, request.NodeID, request.Identifier)
	postPlayers, postMediaPresent, readErr := s.freshPlayers(opCtx, request.NodeID, request.Media)
	if readErr == nil && (!postMediaPresent || !containsViewer(postPlayers, request.Identifier)) {
		if actionErr != nil {
			result.AlreadyDisconnected = true
			return result, nil
		}
		result.Kicked = true
		return result, nil
	}
	result.Uncertain = true
	result.Retryable = true
	if actionErr != nil {
		return result, normalizeStreamError(actionErr, request.NodeID)
	}
	if readErr != nil {
		return result, NewInternalError(nodeIDString(request.NodeID), "session kick outcome is uncertain", readErr)
	}
	return result, NewInternalError(nodeIDString(request.NodeID), "session kick outcome is uncertain")
}

func (s *SessionService) freshPlayers(ctx context.Context, nodeID int64, media MediaIdentity) ([]zlm.MediaPlayer, bool, error) {
	players, err := s.fresh.GetMediaPlayerListFresh(ctx, nodeID, toStreamTarget(media))
	if err != nil {
		if errors.Is(err, ErrMediaNotFound) || errors.Is(err, zlm.ErrMediaNotFound) {
			return nil, false, nil
		}
		return nil, false, normalizeRuntimeReadError(err, nodeID)
	}
	return players, true, nil
}

func containsViewer(players []zlm.MediaPlayer, identifier string) bool {
	for _, player := range players {
		if player.Identifier == identifier {
			return true
		}
	}
	return false
}

func (s *SessionService) activeNode(nodeID int64) (*node.Node, error) {
	if s == nil || s.registry == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "session node registry is not configured")
	}
	current, ok := s.registry.Get(nodeID)
	if !ok || current == nil {
		return nil, NewNodeNotFoundError(nodeIDString(nodeID))
	}
	switch current.State {
	case node.StateActive:
		return current, nil
	case node.StateMaintenance:
		return nil, NewNodeMaintenanceError(nodeIDString(nodeID))
	case node.StateOffline:
		return nil, NewNodeOfflineError(nodeIDString(nodeID))
	default:
		return nil, NewInternalError(nodeIDString(nodeID), "node state is unavailable")
	}
}

func (s *SessionService) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := DefaultStreamOperationTimeout
	if s != nil && s.operationTimeout > 0 && s.operationTimeout <= DefaultStreamOperationTimeout {
		timeout = s.operationTimeout
	}
	return context.WithTimeout(nonNilContext(ctx), timeout)
}

func validateSessionFilter(filter zlm.SessionFilter) error {
	if filter.LocalPort < 0 || filter.LocalPort > 65535 {
		return NewValidationError(map[string]string{"filter.localPort": "must be between 0 and 65535"})
	}
	if strings.TrimSpace(filter.PeerIP) != "" {
		if _, err := netip.ParseAddr(strings.TrimSpace(filter.PeerIP)); err != nil {
			return NewValidationError(map[string]string{"filter.peerIp": "must be a valid IP address"})
		}
	}
	return nil
}

func validateSessionIdentifier(identifier string) error {
	if identifier == "" || strings.TrimSpace(identifier) != identifier || utf8.RuneCountInString(identifier) > MaxMediaIdentityFieldLength {
		return NewValidationError(map[string]string{"identifier": "must be a stable session identifier"})
	}
	for _, character := range identifier {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return NewValidationError(map[string]string{"identifier": "must be a stable session identifier"})
		}
	}
	return nil
}
