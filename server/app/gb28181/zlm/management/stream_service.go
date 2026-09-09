package management

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const DefaultStreamOperationTimeout = 10 * time.Second

// ErrGBPreviewRequiresPlayAuth makes the GB28181 boundary explicit: the
// management preview token is never substituted for the existing device/
// channel playauth flow. T14 can route this result to the business playback
// service without silently losing GB preview support.
var ErrGBPreviewRequiresPlayAuth = errors.New("gb28181 preview requires play authorization")

// StreamNodeRegistry is the read-only node surface required by stream and
// session management. node.Registry implements it; it deliberately exposes no
// persistence or lifecycle mutation methods.
type StreamNodeRegistry interface {
	List() []*node.Node
	Get(int64) (*node.Node, bool)
}

// StreamRuntimeReader is the typed runtime read surface. It has no arbitrary
// API name or parameter map, and all media data remains node-scoped.
type StreamRuntimeReader interface {
	GetMediaListFiltered(context.Context, int64, zlm.MediaFilter) ([]zlm.MediaInfo, error)
	GetMediaInfo(context.Context, int64, zlm.StreamTarget) (*zlm.MediaInfo, error)
	GetMediaPlayerList(context.Context, int64, zlm.StreamTarget) ([]zlm.MediaPlayer, error)
}

// FreshMediaReader is intentionally narrower than StreamRuntimeReader. Its
// methods must bypass RuntimeReader's cache and are used only for destructive
// proof and post-action readback.
type FreshMediaReader interface {
	GetMediaInfoFresh(context.Context, int64, zlm.StreamTarget) (*zlm.MediaInfo, error)
	GetMediaPlayerListFresh(context.Context, int64, zlm.StreamTarget) ([]zlm.MediaPlayer, error)
}

// MediaActionExecutor is the typed write surface shared by stream close and
// session kick. It never accepts a generic API name or a raw parameter map.
type MediaActionExecutor interface {
	CloseStream(context.Context, int64, zlm.StreamTarget, bool) (zlm.CloseStreamResult, error)
	KickSession(context.Context, int64, string) error
}

type StreamOwnership interface {
	Preflight(context.Context, OwnershipTarget) (OwnershipPreflight, error)
	Execute(context.Context, OwnershipPreflight, func(context.Context, OwnershipTarget) error) error
	ExecuteForce(context.Context, OwnershipPreflight, string, func(context.Context, OwnershipTarget) error) error
	PreflightBatch(context.Context, []OwnershipTarget) (OwnershipBatchPreflight, error)
	ExecuteBatch(context.Context, OwnershipBatchPreflight, func(context.Context, OwnershipTarget) error) error
}

type PreviewIssuer interface {
	IssueForUser(uint64, PreviewBinding) (PreviewGrant, error)
}

type PreviewURLResolver interface {
	Resolve(context.Context, *node.Node, MediaIdentity, string, string) (string, error)
}

type StreamServiceDependencies struct {
	Registry StreamNodeRegistry
	Runtime  StreamRuntimeReader
	Fresh    FreshMediaReader

	// NodeExecutor is used to build the production typed action adapter when
	// Executor is not supplied by a test or an embedding service.
	NodeExecutor     *NodeExecutor
	Executor         MediaActionExecutor
	Ownership        StreamOwnership
	SourceCloseGuard SourceCloseGuard

	PreviewIssuer     PreviewIssuer
	PreviewURLs       PreviewURLResolver
	PreviewClassifier PreviewClassifier
}

type StreamServiceOption func(*StreamService)

func WithStreamOperationTimeout(timeout time.Duration) StreamServiceOption {
	return func(service *StreamService) {
		if timeout > 0 && timeout <= DefaultStreamOperationTimeout {
			service.operationTimeout = timeout
		}
	}
}

type StreamService struct {
	registry          StreamNodeRegistry
	runtime           StreamRuntimeReader
	fresh             FreshMediaReader
	executor          MediaActionExecutor
	ownership         StreamOwnership
	previewIssuer     PreviewIssuer
	previewURLs       PreviewURLResolver
	previewClassifier PreviewClassifier
	sourceCloseGuard  SourceCloseGuard
	operationTimeout  time.Duration
}

func NewStreamService(dependencies StreamServiceDependencies, options ...StreamServiceOption) *StreamService {
	service := &StreamService{
		registry:          dependencies.Registry,
		runtime:           dependencies.Runtime,
		fresh:             dependencies.Fresh,
		executor:          dependencies.Executor,
		ownership:         dependencies.Ownership,
		sourceCloseGuard:  dependencies.SourceCloseGuard,
		previewIssuer:     dependencies.PreviewIssuer,
		previewURLs:       dependencies.PreviewURLs,
		previewClassifier: dependencies.PreviewClassifier,
		operationTimeout:  DefaultStreamOperationTimeout,
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

// NodeMediaExecutor is the sole production adapter that knows about
// *zlm.Client for T9 actions.
type NodeMediaExecutor struct {
	executor         *NodeExecutor
	sourceCloseGuard SourceCloseGuard
}

func NewNodeMediaExecutor(executor *NodeExecutor) *NodeMediaExecutor {
	return &NodeMediaExecutor{executor: executor}
}

// SetSourceCloseGuard configures the shared source-close gate for callers
// that use NodeMediaExecutor directly. StreamService also guards its typed
// executor boundary so custom MediaActionExecutor implementations are fenced.
func (e *NodeMediaExecutor) SetSourceCloseGuard(guard SourceCloseGuard) *NodeMediaExecutor {
	if e != nil {
		e.sourceCloseGuard = guard
	}
	return e
}

func (e *NodeMediaExecutor) CloseStream(ctx context.Context, nodeID int64, target zlm.StreamTarget, force bool) (zlm.CloseStreamResult, error) {
	if e == nil || e.executor == nil {
		return zlm.CloseStreamResult{}, NewInternalError(nodeIDString(nodeID), "stream action executor is not configured")
	}
	var result zlm.CloseStreamResult
	operation := func(operationCtx context.Context) error {
		return e.executor.ExecuteWrite(operationCtx, nodeID, func(clientCtx context.Context, client *zlm.Client) error {
			var err error
			result, err = client.CloseStream(clientCtx, target, force)
			return err
		})
	}
	ownershipTarget := OwnershipTarget{NodeID: nodeID, Media: MediaIdentity{Schema: target.Schema, Vhost: target.VHost, App: target.App, Stream: target.Stream}}
	err := runMutationGuard(ctx, e.sourceCloseGuard, ownershipTarget, operation)
	return result, err
}

func (e *NodeMediaExecutor) KickSession(ctx context.Context, nodeID int64, identifier string) error {
	if e == nil || e.executor == nil {
		return NewInternalError(nodeIDString(nodeID), "session action executor is not configured")
	}
	return e.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		return client.KickSession(operationCtx, identifier)
	})
}

// runtimeFreshMediaReader keeps the fresh bypass inside the T9 service files;
// it still uses T4 guard, capability gate and NodeExecutor deadline.
type runtimeFreshMediaReader struct{ reader *RuntimeReader }

func (r runtimeFreshMediaReader) GetMediaInfoFresh(ctx context.Context, nodeID int64, target zlm.StreamTarget) (*zlm.MediaInfo, error) {
	if r.reader == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "fresh runtime reader is not configured")
	}
	if err := target.Validate(); err != nil {
		return nil, NewValidationError(map[string]string{"media": "invalid stream target"})
	}
	if err := r.reader.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.reader.rejectUnsupported(nodeID, zlm.CapabilityGetMediaInfo); err != nil {
		return nil, err
	}
	var info *zlm.MediaInfo
	err := r.reader.executeRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var readErr error
		info, readErr = client.GetMediaInfoStrict(operationCtx, target.Schema, target.VHost, target.App, target.Stream)
		if errors.Is(readErr, zlm.ErrMediaNotFound) {
			return errors.Join(ErrMediaNotFound, readErr)
		}
		return readErr
	})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "fresh media detail was empty")
	}
	if err := verifyMediaInfoIdentity(nodeID, info, target); err != nil {
		return nil, err
	}
	return cloneMediaInfo(info), nil
}

func (r runtimeFreshMediaReader) GetMediaPlayerListFresh(ctx context.Context, nodeID int64, target zlm.StreamTarget) ([]zlm.MediaPlayer, error) {
	if r.reader == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "fresh runtime reader is not configured")
	}
	if err := target.Validate(); err != nil {
		return nil, NewValidationError(map[string]string{"media": "invalid stream target"})
	}
	if err := r.reader.guardRead(ctx, nodeID); err != nil {
		return nil, err
	}
	if err := r.reader.rejectUnsupported(nodeID, zlm.CapabilityGetMediaPlayerList); err != nil {
		return nil, err
	}
	var players []zlm.MediaPlayer
	err := r.reader.executeRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var readErr error
		players, readErr = client.GetMediaPlayerList(operationCtx, target.Schema, target.VHost, target.App, target.Stream)
		if errors.Is(readErr, zlm.ErrMediaNotFound) {
			return errors.Join(ErrMediaNotFound, readErr)
		}
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return cloneMediaPlayers(players), nil
}

type StreamListRequest struct {
	Filter       StreamFilter `json:"filter"`
	OriginType   *int         `json:"originType,omitempty"`
	RecordingMP4 *bool        `json:"recordingMp4,omitempty"`
	RecordingHLS *bool        `json:"recordingHls,omitempty"`
	Page         PageRequest  `json:"page"`
}

type StreamTrack struct {
	CodecID       int      `json:"codecId"`
	CodecIDName   string   `json:"codecIdName"`
	Ready         bool     `json:"ready"`
	CodecType     int      `json:"codecType"`
	Frames        int64    `json:"frames"`
	Duration      float64  `json:"duration"`
	SampleRate    int      `json:"sampleRate"`
	Channels      int      `json:"channels"`
	SampleBit     int      `json:"sampleBit"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	FPS           float64  `json:"fps"`
	KeyFrames     int64    `json:"keyFrames"`
	GOPSize       int      `json:"gopSize"`
	GOPIntervalMS int      `json:"gopIntervalMs"`
	Loss          *float64 `json:"loss,omitempty"`
}

// StreamOwnershipView is the browser-safe ownership summary attached to a
// stream read. It deliberately omits ledger keys, user identifiers, reasons,
// fingerprints and other internal evidence; destructive endpoints keep the
// full snapshot private to their confirmation flow.
type StreamOwnershipView struct {
	Status        OwnershipStatus         `json:"status"`
	Present       bool                    `json:"present"`
	PresenceKnown bool                    `json:"presenceKnown"`
	Sources       []StreamOwnershipSource `json:"sources,omitempty"`
	Impacts       []StreamOwnershipImpact `json:"impacts,omitempty"`
}

type StreamOwnershipSource struct {
	Type       OwnershipType       `json:"type"`
	Confidence OwnershipConfidence `json:"confidence"`
}

type StreamOwnershipImpact struct {
	Type  OwnershipType `json:"type"`
	Count int           `json:"count"`
}

type StreamListItem struct {
	NodeID           int64               `json:"nodeId"`
	NodeUUID         string              `json:"nodeUuid"`
	Media            MediaIdentity       `json:"media"`
	Online           bool                `json:"online"`
	AliveSecond      uint64              `json:"aliveSecond"`
	BytesSpeed       uint64              `json:"bytesSpeed"`
	TotalBytes       uint64              `json:"totalBytes"`
	ReaderCount      int                 `json:"readerCount"`
	TotalReaderCount int                 `json:"totalReaderCount"`
	OriginType       int                 `json:"originType"`
	OriginTypeName   string              `json:"originTypeName,omitempty"`
	RecordingMP4     bool                `json:"recordingMp4"`
	RecordingHLS     bool                `json:"recordingHls"`
	TrackCount       int                 `json:"trackCount"`
	Tracks           []StreamTrack       `json:"tracks,omitempty"`
	Ownership        StreamOwnershipView `json:"ownership"`
}

type StreamDetail struct {
	StreamListItem
	CreateStamp  uint64 `json:"createStamp"`
	CurrentStamp uint64 `json:"currentStamp"`
}

type StreamNodeError struct {
	NodeID    int64               `json:"nodeId"`
	Code      ManagementErrorCode `json:"code"`
	Message   string              `json:"message"`
	Retryable bool                `json:"retryable"`
}

type StreamListResult struct {
	Page[StreamListItem]
	Partial bool              `json:"partial"`
	AsOf    time.Time         `json:"asOf"`
	Errors  []StreamNodeError `json:"errors,omitempty"`
}

type StreamViewer struct {
	NodeID     int64         `json:"nodeId"`
	NodeUUID   string        `json:"nodeUuid"`
	Media      MediaIdentity `json:"media"`
	Identifier string        `json:"identifier"`
	PeerIP     string        `json:"peerIp"`
	PeerPort   int           `json:"peerPort"`
	LocalIP    string        `json:"localIp"`
	LocalPort  int           `json:"localPort"`
	TypeID     string        `json:"typeId"`
	Kickable   bool          `json:"kickable"`
}

type StreamViewerPage struct {
	Page[StreamViewer]
	NodeID   int64         `json:"nodeId"`
	NodeUUID string        `json:"nodeUuid"`
	Target   MediaIdentity `json:"target"`
	AsOf     time.Time     `json:"asOf"`
}

func (s *StreamService) ListStreams(ctx context.Context, request StreamListRequest) (StreamListResult, error) {
	result := StreamListResult{AsOf: time.Now().UTC(), Errors: make([]StreamNodeError, 0)}
	if err := request.Filter.validate(); err != nil {
		return result, err
	}
	if request.OriginType != nil && *request.OriginType < 0 {
		return result, NewValidationError(map[string]string{"originType": "must not be negative"})
	}
	if s == nil || s.registry == nil || s.runtime == nil {
		return result, NewInternalError("", "stream service dependencies are not configured")
	}
	nodes := s.registry.List()
	if request.Filter.NodeID != 0 {
		found := false
		for _, current := range nodes {
			if current != nil && current.ID == request.Filter.NodeID {
				found = true
				break
			}
		}
		if !found {
			return result, NewNodeNotFoundError(nodeIDString(request.Filter.NodeID))
		}
		nodes = filterNodesByID(nodes, request.Filter.NodeID)
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	items := make([]StreamListItem, 0)
	for _, current := range nodes {
		if current == nil {
			continue
		}
		if current.State != node.StateActive {
			if request.Filter.NodeID != 0 {
				return result, nodeStateError(current)
			}
			continue
		}
		media, err := s.runtime.GetMediaListFiltered(opCtx, current.ID, zlm.MediaFilter{
			Schema: request.Filter.Schema, VHost: request.Filter.Vhost,
			App: request.Filter.App, Stream: request.Filter.Stream,
		})
		if err != nil {
			result.Partial = true
			result.Errors = append(result.Errors, streamNodeError(current.ID, err))
			continue
		}
		for _, info := range media {
			item, itemErr := streamListItem(current, &info)
			if itemErr != nil {
				return result, itemErr
			}
			ownership, ownershipErr := s.readOwnershipView(opCtx, OwnershipTarget{NodeID: current.ID, Media: item.Media})
			if ownershipErr != nil {
				result.Partial = true
				result.Errors = append(result.Errors, streamNodeError(current.ID, ownershipErr))
			}
			item.Ownership = ownership
			if request.OriginType != nil && item.OriginType != *request.OriginType {
				continue
			}
			if request.RecordingMP4 != nil && item.RecordingMP4 != *request.RecordingMP4 {
				continue
			}
			if request.RecordingHLS != nil && item.RecordingHLS != *request.RecordingHLS {
				continue
			}
			items = append(items, item)
		}
	}
	sortStreamListItems(items)
	page := Paginate(items, request.Page)
	result.Page = page
	sort.Slice(result.Errors, func(i, j int) bool { return result.Errors[i].NodeID < result.Errors[j].NodeID })
	return result, nil
}

func (s *StreamService) GetStreamDetail(ctx context.Context, nodeID int64, media MediaIdentity) (*StreamDetail, error) {
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}
	if err := media.Validate(); err != nil {
		return nil, err
	}
	current, err := s.activeNode(nodeID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.runtime == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "stream runtime reader is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	info, err := s.runtime.GetMediaInfo(opCtx, nodeID, toStreamTarget(media))
	if err != nil {
		return nil, normalizeRuntimeReadError(err, nodeID)
	}
	if info == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "media detail was empty")
	}
	if err := verifyMediaInfoIdentity(nodeID, info, toStreamTarget(media)); err != nil {
		return nil, err
	}
	item, err := streamListItem(current, info)
	if err != nil {
		return nil, err
	}
	ownership, err := s.readOwnershipView(opCtx, OwnershipTarget{NodeID: nodeID, Media: item.Media})
	if err != nil {
		return nil, err
	}
	item.Ownership = ownership
	return &StreamDetail{StreamListItem: item, CreateStamp: info.CreateStamp, CurrentStamp: info.CurrentStamp}, nil
}

func (s *StreamService) ListStreamViewers(ctx context.Context, nodeID int64, media MediaIdentity, request PageRequest) (StreamViewerPage, error) {
	result := StreamViewerPage{NodeID: nodeID, Target: media, AsOf: time.Now().UTC()}
	if err := validateNodeID(nodeID); err != nil {
		return result, err
	}
	if err := media.Validate(); err != nil {
		return result, err
	}
	current, err := s.activeNode(nodeID)
	if err != nil {
		return result, err
	}
	result.NodeUUID = current.MediaServerUUID
	if s == nil || s.runtime == nil {
		return result, NewInternalError(nodeIDString(nodeID), "stream runtime reader is not configured")
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
	page := Paginate(viewers, request)
	result.Page = page
	return result, nil
}

type PreviewGrantRequest struct {
	NodeID       int64         `json:"nodeId"`
	Media        MediaIdentity `json:"media"`
	Protocol     string        `json:"protocol"`
	ClientIP     string        `json:"clientIp,omitempty"`
	BindClientIP bool          `json:"bindClientIp,omitempty"`
}

type PreviewGrantResponse struct {
	NodeID     int64         `json:"nodeId"`
	NodeUUID   string        `json:"nodeUuid"`
	Media      MediaIdentity `json:"media"`
	Protocol   string        `json:"protocol"`
	HookSchema string        `json:"hookSchema"`
	Token      string        `json:"token"`
	ExpiresAt  time.Time     `json:"expiresAt"`
	JTIHash    string        `json:"jtiHash"`
	URL        string        `json:"url"`
}

func (s *StreamService) IssuePreviewGrant(ctx context.Context, actorUserID uint64, request PreviewGrantRequest) (*PreviewGrantResponse, error) {
	if err := validateActor(actorUserID); err != nil {
		return nil, err
	}
	if err := validateNodeID(request.NodeID); err != nil {
		return nil, err
	}
	if err := request.Media.Validate(); err != nil {
		return nil, err
	}
	protocol, hookSchema, ok := previewHookSchema(request.Protocol)
	if !ok {
		return nil, ErrPreviewUnsupported
	}
	current, err := s.activeNode(request.NodeID)
	if err != nil {
		return nil, err
	}
	if s == nil || s.runtime == nil || s.previewIssuer == nil || s.previewURLs == nil || s.previewClassifier == nil {
		return nil, ErrPreviewUnsupported
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	info, err := s.runtime.GetMediaInfo(opCtx, request.NodeID, toStreamTarget(request.Media))
	if err != nil {
		return nil, normalizeRuntimeReadError(err, request.NodeID)
	}
	if info == nil {
		return nil, NewInternalError(nodeIDString(request.NodeID), "media detail was empty")
	}
	if err := verifyMediaInfoIdentity(request.NodeID, info, toStreamTarget(request.Media)); err != nil {
		return nil, err
	}

	sourceResource := PreviewResource{NodeUUID: current.MediaServerUUID, VHost: request.Media.Vhost, Schema: request.Media.Schema, App: request.Media.App, Stream: request.Media.Stream}
	class, classifyErr := s.previewClassifier.Classify(opCtx, sourceResource)
	if classifyErr != nil {
		return nil, ErrPreviewUnsupported
	}
	if class == PreviewResourceGB {
		return nil, errors.Join(ErrPreviewUnsupported, ErrGBPreviewRequiresPlayAuth)
	}
	if class != PreviewResourceNonGBPreviewable {
		return nil, ErrPreviewUnsupported
	}
	hookMedia := request.Media
	hookMedia.Schema = hookSchema
	hookResource := PreviewResource{NodeUUID: current.MediaServerUUID, VHost: hookMedia.Vhost, Schema: hookMedia.Schema, App: hookMedia.App, Stream: hookMedia.Stream}
	hookClass, classifyErr := s.previewClassifier.Classify(opCtx, hookResource)
	if classifyErr != nil || hookClass != PreviewResourceNonGBPreviewable {
		return nil, ErrPreviewUnsupported
	}
	grant, err := s.previewIssuer.IssueForUser(actorUserID, PreviewBinding{
		NodeUUID: current.MediaServerUUID, VHost: hookMedia.Vhost, Schema: hookMedia.Schema,
		App: hookMedia.App, Stream: hookMedia.Stream, ClientIP: request.ClientIP, BindClientIP: request.BindClientIP,
	})
	if err != nil {
		return nil, ErrPreviewUnsupported
	}
	previewURL, err := s.previewURLs.Resolve(opCtx, current, hookMedia, protocol, grant.Token)
	if err != nil {
		return nil, ErrPreviewUnsupported
	}
	return &PreviewGrantResponse{
		NodeID: request.NodeID, NodeUUID: current.MediaServerUUID, Media: request.Media,
		Protocol: protocol, HookSchema: hookSchema, Token: grant.Token, ExpiresAt: grant.ExpiresAt,
		JTIHash: grant.JTIHash, URL: previewURL,
	}, nil
}

type CloseStreamRequest struct {
	Target      OwnershipTarget `json:"target"`
	Fingerprint string          `json:"fingerprint"`
	Force       bool            `json:"force,omitempty"`
}

type ForceCloseStreamRequest struct {
	Target      OwnershipTarget `json:"target"`
	Fingerprint string          `json:"fingerprint"`
	Reason      string          `json:"reason"`
}

type StreamClosePreview struct {
	Target       OwnershipTarget     `json:"target"`
	Snapshot     StreamOwnershipView `json:"snapshot"`
	Fingerprint  string              `json:"fingerprint"`
	FreshPresent bool                `json:"freshPresent"`
	snapshot     OwnershipSnapshot
}

type StreamCloseResult struct {
	Target        OwnershipTarget `json:"target"`
	Closed        bool            `json:"closed"`
	AlreadyAbsent bool            `json:"alreadyAbsent"`
	Force         bool            `json:"force"`
	Uncertain     bool            `json:"uncertain"`
	Retryable     bool            `json:"retryable"`
}

type StreamCloseBatchResult struct {
	Results       []StreamCloseResult `json:"results"`
	Closed        int                 `json:"closed"`
	AlreadyAbsent int                 `json:"alreadyAbsent"`
	Partial       bool                `json:"partial"`
	Uncertain     bool                `json:"uncertain"`
}

func (s *StreamService) PreflightCloseStream(ctx context.Context, target OwnershipTarget) (StreamClosePreview, error) {
	if err := target.Validate(); err != nil {
		return StreamClosePreview{}, err
	}
	if s == nil || s.ownership == nil || s.fresh == nil {
		return StreamClosePreview{}, NewInternalError(nodeIDString(target.NodeID), "stream close dependencies are not configured")
	}
	if _, err := s.activeNode(target.NodeID); err != nil {
		return StreamClosePreview{}, err
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	freshPresent, err := s.freshTargetPresent(opCtx, target)
	if err != nil && !isMediaNotFound(err) {
		return StreamClosePreview{}, err
	}
	if isMediaNotFound(err) {
		freshPresent = false
	}
	preflight, err := s.ownership.Preflight(opCtx, target)
	if err != nil {
		return StreamClosePreview{}, normalizeStreamError(err, target.NodeID)
	}
	if preflight.Target != target || preflight.Snapshot.Target != target {
		return StreamClosePreview{}, NewInternalError(nodeIDString(target.NodeID), "ownership target did not match requested media")
	}
	return StreamClosePreview{
		Target: preflight.Target, Snapshot: safeOwnershipView(preflight.Snapshot),
		Fingerprint: preflight.Fingerprint, FreshPresent: freshPresent, snapshot: preflight.Snapshot,
	}, nil
}

func (s *StreamService) CloseStream(ctx context.Context, request CloseStreamRequest) (StreamCloseResult, error) {
	if err := validateStreamCloseRequest(request); err != nil {
		return StreamCloseResult{Target: request.Target}, err
	}
	if request.Force {
		return StreamCloseResult{Target: request.Target}, NewValidationError(map[string]string{"force": "must use the force-close operation"})
	}
	preview, err := s.PreflightCloseStream(ctx, request.Target)
	if err != nil {
		return StreamCloseResult{Target: request.Target}, err
	}
	if !preview.FreshPresent {
		return StreamCloseResult{Target: request.Target, AlreadyAbsent: true}, nil
	}
	if ownershipEvidenceLacksSchema(preview.snapshot) {
		return StreamCloseResult{Target: request.Target}, ownershipConflict(request.Target, preview.snapshot, "management provenance does not prove schema ownership")
	}
	if request.Fingerprint != preview.Fingerprint {
		return StreamCloseResult{Target: request.Target}, ownershipConflict(request.Target, preview.snapshot, "ownership fingerprint changed; reconfirm required")
	}
	return s.executeStreamClose(ctx, preview, false, "")
}

func (s *StreamService) ForceCloseStream(ctx context.Context, request ForceCloseStreamRequest) (StreamCloseResult, error) {
	if err := validateForceStreamCloseRequest(request); err != nil {
		return StreamCloseResult{Target: request.Target, Force: true}, err
	}
	preview, err := s.PreflightCloseStream(ctx, request.Target)
	if err != nil {
		return StreamCloseResult{Target: request.Target, Force: true}, err
	}
	if !preview.FreshPresent {
		return StreamCloseResult{Target: request.Target, Force: true, AlreadyAbsent: true}, nil
	}
	if request.Fingerprint != preview.Fingerprint {
		return StreamCloseResult{Target: request.Target, Force: true}, ownershipConflict(request.Target, preview.snapshot, "ownership fingerprint changed; reconfirm required")
	}
	return s.executeStreamClose(ctx, preview, true, request.Reason)
}

func (s *StreamService) executeStreamClose(ctx context.Context, preview StreamClosePreview, force bool, reason string) (StreamCloseResult, error) {
	result := StreamCloseResult{Target: preview.Target, Force: force}
	if s == nil || s.executor == nil || s.ownership == nil || s.fresh == nil {
		return result, NewInternalError(nodeIDString(preview.Target.NodeID), "stream close dependencies are not configured")
	}
	preflight := OwnershipPreflight{Target: preview.Target, Snapshot: preview.snapshot, Fingerprint: preview.Fingerprint}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	var actionStarted bool
	var actionAbsent bool
	var closeErr error
	action := func(actionCtx context.Context, target OwnershipTarget) error {
		present, err := s.freshTargetPresent(actionCtx, target)
		if err != nil {
			if errors.Is(err, ErrMediaNotFound) {
				actionAbsent = true
				return nil
			}
			return err
		}
		if !present {
			actionAbsent = true
			return nil
		}
		return runMutationGuard(actionCtx, s.sourceCloseGuard, target, func(operationCtx context.Context) error {
			actionStarted = true
			_, closeErr = s.executor.CloseStream(operationCtx, target.NodeID, toStreamTarget(target.Media), force)
			return closeErr
		})
	}
	if force {
		closeErr = s.ownership.ExecuteForce(opCtx, preflight, reason, action)
	} else {
		closeErr = s.ownership.Execute(opCtx, preflight, action)
	}
	if actionAbsent {
		return StreamCloseResult{Target: preview.Target, AlreadyAbsent: true, Force: force}, nil
	}
	if closeErr != nil && !actionStarted {
		return result, normalizeStreamError(closeErr, preview.Target.NodeID)
	}
	if closeErr != nil && !isMediaNotFound(closeErr) {
		result.Uncertain = actionStarted
		result.Retryable = actionStarted
		return result, normalizeStreamError(closeErr, preview.Target.NodeID)
	}
	if !actionStarted {
		return result, NewInternalError(nodeIDString(preview.Target.NodeID), "stream close action was not invoked")
	}
	present, readErr := s.freshTargetPresent(opCtx, preview.Target)
	if (readErr == nil && !present) || errors.Is(readErr, ErrMediaNotFound) {
		return StreamCloseResult{Target: preview.Target, Closed: true, Force: force}, nil
	}
	result.Uncertain = true
	result.Retryable = true
	return result, NewInternalError(nodeIDString(preview.Target.NodeID), "media close outcome is uncertain", readErr)
}

func (s *StreamService) PreflightCloseStreams(ctx context.Context, targets []OwnershipTarget) (OwnershipBatchPreflight, error) {
	if len(targets) == 0 {
		return OwnershipBatchPreflight{}, NewValidationError(map[string]string{"targets": "must not be empty"})
	}
	if s == nil || s.ownership == nil || s.fresh == nil {
		return OwnershipBatchPreflight{}, NewInternalError("", "batch close dependencies are not configured")
	}
	for _, target := range targets {
		if err := target.Validate(); err != nil {
			return OwnershipBatchPreflight{}, err
		}
		if _, err := s.activeNode(target.NodeID); err != nil {
			return OwnershipBatchPreflight{}, err
		}
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	if _, err := s.freshTargetPresence(opCtx, targets); err != nil {
		return OwnershipBatchPreflight{}, err
	}
	preflight, err := s.ownership.PreflightBatch(opCtx, targets)
	if err != nil {
		return OwnershipBatchPreflight{}, normalizeStreamError(err, targets[0].NodeID)
	}
	return preflight, nil
}

func (s *StreamService) CloseStreams(ctx context.Context, preflight OwnershipBatchPreflight) (StreamCloseBatchResult, error) {
	result := StreamCloseBatchResult{Results: make([]StreamCloseResult, 0, len(preflight.Targets))}
	if len(preflight.Targets) == 0 || safeFingerprint(preflight.Fingerprint) == "" {
		return result, NewValidationError(map[string]string{"preflight": "is required"})
	}
	if len(preflight.Snapshots) != len(preflight.Targets) {
		return result, NewValidationError(map[string]string{"preflight": "must include every target snapshot"})
	}
	targets := make(map[string]struct{}, len(preflight.Targets))
	for _, target := range preflight.Targets {
		if err := target.Validate(); err != nil {
			return result, err
		}
		key := ownershipTargetKey(target)
		if _, exists := targets[key]; exists {
			return result, NewValidationError(map[string]string{"preflight": "must not contain duplicate targets"})
		}
		targets[key] = struct{}{}
	}
	snapshots := make(map[string]struct{}, len(preflight.Snapshots))
	for _, snapshot := range preflight.Snapshots {
		if err := snapshot.Target.Validate(); err != nil {
			return result, err
		}
		key := ownershipTargetKey(snapshot.Target)
		if _, exists := targets[key]; !exists {
			return result, NewValidationError(map[string]string{"preflight": "snapshot target does not match requested targets"})
		}
		if _, exists := snapshots[key]; exists {
			return result, NewValidationError(map[string]string{"preflight": "must not contain duplicate snapshots"})
		}
		snapshots[key] = struct{}{}
		if ownershipEvidenceLacksSchema(snapshot) {
			return result, ownershipConflict(snapshot.Target, snapshot, "management provenance does not prove schema ownership")
		}
	}
	if s == nil || s.ownership == nil || s.executor == nil || s.fresh == nil {
		return result, NewInternalError("", "batch close dependencies are not configured")
	}
	for _, target := range preflight.Targets {
		if _, err := s.activeNode(target.NodeID); err != nil {
			return result, err
		}
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	if _, err := s.freshTargetPresence(opCtx, preflight.Targets); err != nil {
		return result, err
	}
	action := func(actionCtx context.Context, target OwnershipTarget) error {
		item := StreamCloseResult{Target: target}
		present, err := s.freshTargetPresent(actionCtx, target)
		if err != nil {
			if errors.Is(err, ErrMediaNotFound) {
				item.AlreadyAbsent = true
				result.AlreadyAbsent++
				result.Results = append(result.Results, item)
				return nil
			}
			return err
		}
		if !present {
			item.AlreadyAbsent = true
			result.AlreadyAbsent++
			result.Results = append(result.Results, item)
			return nil
		}
		var actionStarted bool
		err = runMutationGuard(actionCtx, s.sourceCloseGuard, target, func(operationCtx context.Context) error {
			actionStarted = true
			_, closeErr := s.executor.CloseStream(operationCtx, target.NodeID, toStreamTarget(target.Media), false)
			return closeErr
		})
		if err != nil && !isMediaNotFound(err) {
			item.Uncertain = actionStarted
			item.Retryable = actionStarted
			result.Uncertain = result.Uncertain || actionStarted
			result.Results = append(result.Results, item)
			return err
		}
		present, readErr := s.freshTargetPresent(actionCtx, target)
		if (readErr == nil && !present) || errors.Is(readErr, ErrMediaNotFound) {
			item.Closed = true
			result.Closed++
			result.Results = append(result.Results, item)
			return nil
		}
		item.Uncertain = true
		item.Retryable = true
		result.Uncertain = true
		result.Results = append(result.Results, item)
		return NewInternalError(nodeIDString(target.NodeID), "media close outcome is uncertain", readErr)
	}
	err := s.ownership.ExecuteBatch(opCtx, preflight, action)
	if err != nil {
		result.Partial = len(result.Results) > 0
		return result, normalizeStreamError(err, firstTargetNodeID(preflight.Targets))
	}
	return result, nil
}

func (s *StreamService) freshTargetPresence(ctx context.Context, targets []OwnershipTarget) (map[string]bool, error) {
	present := make(map[string]bool, len(targets))
	for _, target := range targets {
		isPresent, err := s.freshTargetPresent(ctx, target)
		if err != nil {
			if errors.Is(err, ErrMediaNotFound) {
				present[ownershipTargetKey(target)] = false
				continue
			}
			return nil, err
		}
		present[ownershipTargetKey(target)] = isPresent
	}
	return present, nil
}

func (s *StreamService) freshTargetPresent(ctx context.Context, target OwnershipTarget) (bool, error) {
	if s == nil || s.fresh == nil {
		return false, NewInternalError(nodeIDString(target.NodeID), "fresh media reader is not configured")
	}
	info, err := s.fresh.GetMediaInfoFresh(ctx, target.NodeID, toStreamTarget(target.Media))
	if err != nil {
		return false, normalizeRuntimeReadError(err, target.NodeID)
	}
	if info == nil {
		return false, NewInternalError(nodeIDString(target.NodeID), "fresh media detail was empty")
	}
	if err := verifyMediaInfoIdentity(target.NodeID, info, toStreamTarget(target.Media)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *StreamService) activeNode(nodeID int64) (*node.Node, error) {
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}
	if s == nil || s.registry == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "stream node registry is not configured")
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

func (s *StreamService) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := DefaultStreamOperationTimeout
	if s != nil && s.operationTimeout > 0 && s.operationTimeout <= DefaultStreamOperationTimeout {
		timeout = s.operationTimeout
	}
	return context.WithTimeout(nonNilContext(ctx), timeout)
}

func streamListItem(current *node.Node, info *zlm.MediaInfo) (StreamListItem, error) {
	if current == nil || info == nil {
		return StreamListItem{}, NewInternalError(nodeIDString(nodeIDFromNode(current)), "media snapshot is unavailable")
	}
	target := zlm.StreamTarget{Schema: info.Schema, VHost: info.VHost, App: info.App, Stream: info.Stream}
	if err := target.Validate(); err != nil {
		return StreamListItem{}, NewInternalError(nodeIDString(current.ID), "media identity is invalid", err)
	}
	tracks := cloneStreamTracks(info.Tracks)
	return StreamListItem{
		NodeID: current.ID, NodeUUID: current.MediaServerUUID,
		Media:  MediaIdentity{Schema: info.Schema, Vhost: info.VHost, App: info.App, Stream: info.Stream},
		Online: info.Online, AliveSecond: info.AliveSecond, BytesSpeed: info.BytesSpeed,
		TotalBytes: info.TotalBytes, ReaderCount: info.ReaderCount, TotalReaderCount: info.TotalReaderCount,
		OriginType: info.OriginType, OriginTypeName: safeRuntimeLabel(info.OriginTypeStr),
		RecordingMP4: info.IsRecordingMP4, RecordingHLS: info.IsRecordingHLS,
		TrackCount: len(tracks), Tracks: tracks,
	}, nil
}

func verifyMediaInfoIdentity(nodeID int64, info *zlm.MediaInfo, target zlm.StreamTarget) error {
	if info == nil {
		return NewInternalError(nodeIDString(nodeID), "media snapshot is unavailable")
	}
	if err := target.Validate(); err != nil {
		return NewValidationError(map[string]string{"media": "invalid stream target"})
	}
	actual := zlm.StreamTarget{Schema: info.Schema, VHost: info.VHost, App: info.App, Stream: info.Stream}
	if err := actual.Validate(); err != nil || actual != target {
		return NewInternalError(nodeIDString(nodeID), "ZLM media identity did not match the requested target")
	}
	return nil
}

func (s *StreamService) readOwnershipView(ctx context.Context, target OwnershipTarget) (StreamOwnershipView, error) {
	view := unknownOwnershipView()
	if s == nil || s.ownership == nil {
		return view, nil
	}
	preflight, err := s.ownership.Preflight(ctx, target)
	if err != nil {
		return view, normalizeStreamError(err, target.NodeID)
	}
	if preflight.Target != target || preflight.Snapshot.Target != target {
		return view, NewInternalError(nodeIDString(target.NodeID), "ownership target did not match requested media")
	}
	return safeOwnershipView(preflight.Snapshot), nil
}

func unknownOwnershipView() StreamOwnershipView {
	return StreamOwnershipView{Status: OwnershipStatusUnknown}
}

func safeOwnershipView(snapshot OwnershipSnapshot) StreamOwnershipView {
	view := StreamOwnershipView{
		Status: safeOwnershipStatus(snapshot.Status), Present: snapshot.Present, PresenceKnown: snapshot.PresenceKnown,
		Sources: make([]StreamOwnershipSource, 0), Impacts: make([]StreamOwnershipImpact, 0),
	}

	sourceSet := make(map[string]StreamOwnershipSource)
	impactCounts := make(map[OwnershipType]int)
	for _, evidence := range snapshot.Owners {
		ownershipType := safeOwnershipType(evidence.Type)
		confidence := safeOwnershipConfidence(evidence.Confidence)
		sourceKey := string(ownershipType) + ":" + string(confidence)
		sourceSet[sourceKey] = StreamOwnershipSource{Type: ownershipType, Confidence: confidence}
		impactCounts[ownershipType]++
	}
	if len(snapshot.Owners) == 0 && len(snapshot.Impacts) > 0 {
		impactCounts[OwnershipTypeUnknown] = len(snapshot.Impacts)
	}
	for _, source := range sourceSet {
		view.Sources = append(view.Sources, source)
	}
	sort.Slice(view.Sources, func(i, j int) bool {
		if view.Sources[i].Type != view.Sources[j].Type {
			return view.Sources[i].Type < view.Sources[j].Type
		}
		return view.Sources[i].Confidence < view.Sources[j].Confidence
	})
	for ownershipType, count := range impactCounts {
		if count > 0 {
			view.Impacts = append(view.Impacts, StreamOwnershipImpact{Type: ownershipType, Count: count})
		}
	}
	sort.Slice(view.Impacts, func(i, j int) bool { return view.Impacts[i].Type < view.Impacts[j].Type })
	return view
}

func safeOwnershipStatus(value OwnershipStatus) OwnershipStatus {
	switch value {
	case OwnershipStatusAbsent, OwnershipStatusManaged, OwnershipStatusOwned,
		OwnershipStatusUnknown, OwnershipStatusConflicted:
		return value
	default:
		return OwnershipStatusUnknown
	}
}

func safeOwnershipType(value OwnershipType) OwnershipType {
	switch value {
	case OwnershipTypeRealtimePlayback, OwnershipTypeDevicePlayback, OwnershipTypeTalk,
		OwnershipTypeCascade, OwnershipTypeRecordingPlan, OwnershipTypeContinuousRecording,
		OwnershipTypeRecordingSession, OwnershipTypeManaged:
		return value
	default:
		return OwnershipTypeUnknown
	}
}

func safeOwnershipConfidence(value OwnershipConfidence) OwnershipConfidence {
	if value == OwnershipConfidenceProven {
		return OwnershipConfidenceProven
	}
	return OwnershipConfidenceUncertain
}

func cloneStreamTracks(tracks []zlm.MediaTrack) []StreamTrack {
	if len(tracks) == 0 {
		return []StreamTrack{}
	}
	result := make([]StreamTrack, len(tracks))
	for i, track := range tracks {
		result[i] = StreamTrack{
			CodecID: track.CodecID, CodecIDName: safeRuntimeLabel(track.CodecIDName), Ready: track.Ready,
			CodecType: track.CodecType, Frames: track.Frames, Duration: track.Duration,
			SampleRate: track.SampleRate, Channels: track.Channels, SampleBit: track.SampleBit,
			Width: track.Width, Height: track.Height, FPS: track.FPS, KeyFrames: track.KeyFrames,
			GOPSize: track.GOPSize, GOPIntervalMS: track.GOPIntervalMS,
		}
		if track.Loss != nil && *track.Loss >= 0 {
			loss := *track.Loss
			result[i].Loss = &loss
		}
	}
	return result
}

func streamViewer(current *node.Node, media MediaIdentity, player zlm.MediaPlayer) StreamViewer {
	return StreamViewer{
		NodeID: current.ID, NodeUUID: current.MediaServerUUID, Media: media,
		Identifier: safeRuntimeLabel(player.Identifier), PeerIP: safeRuntimeLabel(player.PeerIP), PeerPort: player.PeerPort,
		LocalIP: player.LocalIP, LocalPort: player.LocalPort, TypeID: safeRuntimeLabel(player.TypeID),
		Kickable: viewerKickable(player),
	}
}

func viewerKickable(player zlm.MediaPlayer) bool {
	return strings.TrimSpace(player.Identifier) != "" && !strings.Contains(strings.ToLower(player.TypeID), "udp")
}

func sortStreamListItems(items []StreamListItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.NodeID != right.NodeID {
			return left.NodeID < right.NodeID
		}
		for _, pair := range [][2]string{{left.Media.Schema, right.Media.Schema}, {left.Media.Vhost, right.Media.Vhost}, {left.Media.App, right.Media.App}, {left.Media.Stream, right.Media.Stream}} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
}

func toStreamTarget(media MediaIdentity) zlm.StreamTarget {
	return zlm.StreamTarget{Schema: media.Schema, VHost: media.Vhost, App: media.App, Stream: media.Stream}
}

func ownershipTargetKey(target OwnershipTarget) string {
	return strconv.FormatInt(target.NodeID, 10) + ":" + streamTargetKey(toStreamTarget(target.Media))
}

func firstTargetNodeID(targets []OwnershipTarget) int64 {
	if len(targets) == 0 {
		return 0
	}
	return targets[0].NodeID
}

func nodeIDFromNode(current *node.Node) int64 {
	if current == nil {
		return 0
	}
	return current.ID
}

func nodeStateError(current *node.Node) error {
	if current == nil {
		return NewInternalError("", "node snapshot is unavailable")
	}
	switch current.State {
	case node.StateMaintenance:
		return NewNodeMaintenanceError(nodeIDString(current.ID))
	case node.StateOffline:
		return NewNodeOfflineError(nodeIDString(current.ID))
	default:
		return NewInternalError(nodeIDString(current.ID), "node state is unavailable")
	}
}

func streamNodeError(nodeID int64, err error) StreamNodeError {
	normalized := normalizeStreamError(err, nodeID)
	if managementErr, ok := AsManagementError(normalized); ok {
		return StreamNodeError{NodeID: nodeID, Code: managementErr.Code, Message: managementErr.Message, Retryable: managementErr.Retryable}
	}
	return StreamNodeError{NodeID: nodeID, Code: CodeInternal, Message: "internal management error"}
}

func normalizeStreamError(err error, nodeID int64) error {
	if err == nil {
		return nil
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) && validationErr != nil {
		return err
	}
	if _, ok := AsManagementError(err); ok {
		return err
	}
	return NormalizeError(err, nodeIDString(nodeID))
}

func normalizeRuntimeReadError(err error, nodeID int64) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrMediaNotFound) {
		return err
	}
	if errors.Is(err, zlm.ErrMediaNotFound) {
		return errors.Join(ErrMediaNotFound, err)
	}
	return normalizeStreamError(err, nodeID)
}

func validateStreamCloseRequest(request CloseStreamRequest) error {
	if err := request.Target.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "is required"})
	}
	if safeFingerprint(request.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "must be a SHA-256 fingerprint"})
	}
	return nil
}

func validateForceStreamCloseRequest(request ForceCloseStreamRequest) error {
	if err := request.Target.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Fingerprint) == "" || safeFingerprint(request.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "must be a SHA-256 fingerprint"})
	}
	if strings.TrimSpace(request.Reason) == "" {
		return NewValidationError(map[string]string{"reason": "is required"})
	}
	return nil
}

func isMediaNotFound(err error) bool {
	return errors.Is(err, ErrMediaNotFound) || errors.Is(err, zlm.ErrMediaNotFound)
}

func ownershipEvidenceLacksSchema(snapshot OwnershipSnapshot) bool {
	for _, owner := range snapshot.Owners {
		// T5's current ledger identity has no schema field. T6 labels this
		// evidence as management-ledger provenance, so T9 must not turn an
		// app/stream-only match into an exact protocol ownership proof.
		if owner.Type == OwnershipTypeManaged && strings.EqualFold(strings.TrimSpace(owner.Reason), "management ledger provenance") {
			return true
		}
	}
	return false
}

func previewHookSchema(protocol string) (string, string, bool) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case string(PreviewProtocolHTTPSFLV), string(PreviewProtocolWSSFLV), string(PreviewProtocolRTMPS):
		return protocol, "rtmp", true
	case string(PreviewProtocolHTTPSFMP4), string(PreviewProtocolWSSFMP4):
		return protocol, "fmp4", true
	case string(PreviewProtocolHTTPSSTS), string(PreviewProtocolWSSSTS):
		return protocol, "ts", true
	case string(PreviewProtocolHTTPSHLS):
		return protocol, "hls", true
	case string(PreviewProtocolRTSPS):
		return protocol, "rtsp", true
	case string(PreviewProtocolWebRTCS):
		return protocol, "", false
	default:
		return protocol, "", false
	}
}
