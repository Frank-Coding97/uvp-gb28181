package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

const (
	// ProxyResourceTypePull and ProxyResourceTypePush are the only resource
	// types this service can register. They are also the ledger partition used
	// when marking a resource absent.
	ProxyResourceTypePull = "pull_proxy"
	ProxyResourceTypePush = "push_proxy"

	DefaultProxyRetryCount = 3
	MaxProxyRetryCount     = 10
	DefaultProxyTimeoutSec = 5
	MaxProxyTimeoutSec     = 30

	// The executor normally supplies the same bound. This service keeps a
	// bound as well so an injected executor cannot turn a management request
	// into an unbounded operation.
	DefaultProxyOperationTimeout = 10 * time.Second
)

// ProxyKind identifies the typed ZLM proxy endpoint. It is deliberately not
// an arbitrary endpoint/API name.
type ProxyKind string

const (
	ProxyKindPull ProxyKind = ProxyResourceTypePull
	ProxyKindPush ProxyKind = ProxyResourceTypePush
)

// PullProxyRequest is the browser-safe request for a pull proxy. The actor is
// intentionally absent: callers must provide the authenticated actor as the
// separate argument to CreatePullProxy.
type PullProxyRequest struct {
	NodeID     int64         `json:"nodeId"`
	Media      MediaIdentity `json:"media"`
	SourceURL  string        `json:"sourceUrl"`
	RetryCount int           `json:"retryCount,omitempty"`
	RTPType    int           `json:"rtpType,omitempty"`
	TimeoutSec float64       `json:"timeoutSec,omitempty"`
}

// PushProxyRequest describes a local media source and a third-party target.
// The local MediaIdentity is complete and the target URL is never returned by
// this package in raw form.
type PushProxyRequest struct {
	NodeID     int64         `json:"nodeId"`
	Media      MediaIdentity `json:"media"`
	TargetURL  string        `json:"targetUrl"`
	RetryCount int           `json:"retryCount,omitempty"`
	RTPType    int           `json:"rtpType,omitempty"`
	TimeoutSec float64       `json:"timeoutSec,omitempty"`
}

// ProxyDeleteRequest binds deletion to one complete media identity and an
// optional ownership confirmation fingerprint. There is intentionally no
// force field; force operations belong to a separate high-risk service/API.
type ProxyDeleteRequest struct {
	NodeID      int64         `json:"nodeId"`
	Key         string        `json:"key"`
	Media       MediaIdentity `json:"media"`
	Fingerprint string        `json:"fingerprint,omitempty"`
}

// DeleteProxyRequest is a descriptive compatibility alias for controllers.
type DeleteProxyRequest = ProxyDeleteRequest

// ProxyURLSummary is the only URL shape allowed across the management
// boundary. Display contains scheme and host/port only; Fingerprint is a
// one-way SHA-256 digest of the complete internal URL.
type ProxyURLSummary struct {
	Scheme            string `json:"scheme"`
	Host              string `json:"host"`
	Port              int    `json:"port,omitempty"`
	Fingerprint       string `json:"fingerprint"`
	HasUserInfo       bool   `json:"hasUserInfo"`
	HasSensitiveQuery bool   `json:"hasSensitiveQuery"`
	Display           string `json:"display"`
}

// ProxyView is a safe browser/readback representation of a ZLM proxy. It is
// intentionally not an alias of zlm.StreamProxyInfo because that internal
// DTO contains the complete source/target URL.
type ProxyView struct {
	NodeID                int64               `json:"nodeId"`
	Kind                  ProxyKind           `json:"kind"`
	Key                   string              `json:"key"`
	Media                 MediaIdentity       `json:"media"`
	Online                bool                `json:"online"`
	Status                int                 `json:"status"`
	StatusText            string              `json:"statusText,omitempty"`
	LiveSecs              int64               `json:"liveSecs"`
	RePullCount           int                 `json:"rePullCount"`
	RePublishCount        int                 `json:"rePublishCount"`
	TotalReaderCount      int                 `json:"totalReaderCount"`
	BytesSpeed            uint64              `json:"bytesSpeed"`
	TotalBytes            uint64              `json:"totalBytes"`
	Source                *ProxyURLSummary    `json:"source,omitempty"`
	Target                *ProxyURLSummary    `json:"target,omitempty"`
	Capability            zlm.CapabilityState `json:"capability"`
	Managed               bool                `json:"managed"`
	CreatedBy             uint64              `json:"createdBy,omitempty"`
	ProvenanceFingerprint string              `json:"provenanceFingerprint,omitempty"`
	ProvenanceSummary     string              `json:"provenanceSummary,omitempty"`
}

// ProxyPage is the bounded list result used by both proxy types. Capability
// remains explicit so unknown probes are not presented as supported.
type ProxyPage struct {
	Page[ProxyView]
	Capability zlm.CapabilityState `json:"capability"`
}

// ProxyListResult is a descriptive alias for HTTP adapters.
type ProxyListResult = ProxyPage

type ProxyDeletePreview struct {
	NodeID        int64           `json:"nodeId"`
	Kind          ProxyKind       `json:"kind"`
	Key           string          `json:"key"`
	Target        OwnershipTarget `json:"target"`
	Fingerprint   string          `json:"fingerprint"`
	Status        OwnershipStatus `json:"status"`
	Present       bool            `json:"present"`
	PresenceKnown bool            `json:"presenceKnown"`
	Impacts       []Impact        `json:"impacts,omitempty"`
}

type ProxyDeleteView struct {
	NodeID        int64     `json:"nodeId"`
	Kind          ProxyKind `json:"kind"`
	Key           string    `json:"key"`
	Removed       bool      `json:"removed"`
	AlreadyAbsent bool      `json:"alreadyAbsent"`
	Tombstoned    bool      `json:"tombstoned"`
}

// ProxyExecutor is the narrow typed adapter over T3's zlm.Client. It has no
// generic API name, query map or shell command surface.
type ProxyExecutor interface {
	AddPull(context.Context, int64, zlm.StreamProxyRequest) (*zlm.ProxyCreateResult, error)
	AddPush(context.Context, int64, zlm.StreamPusherProxyRequest) (*zlm.ProxyCreateResult, error)
	ListPull(context.Context, int64, ...string) ([]zlm.StreamProxyInfo, error)
	ListPush(context.Context, int64, ...string) ([]zlm.StreamPusherProxyInfo, error)
	DeletePull(context.Context, int64, string) (*zlm.ProxyDeleteResult, error)
	DeletePush(context.Context, int64, string) (*zlm.ProxyDeleteResult, error)
}

// ProxyCapabilityReader is intentionally the profile-only T4 surface needed
// by this service. A failed probe means unknown and permits one controlled
// typed attempt; an explicitly unsupported API is rejected with 422.
type ProxyCapabilityReader interface {
	GetCapabilityProfile(context.Context, int64) (zlm.CapabilityProfile, error)
}

// ProxyLedger is the narrow T5 provenance surface. The ledger stores only
// safe identity/summary/fingerprint data; it never receives a raw URL.
type ProxyLedger interface {
	Register(context.Context, repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error)
	List(context.Context, repo.ManagedResourceFilter) ([]gbmodels.GbZLMManagedResource, error)
	Tombstone(context.Context, repo.ManagedResourceIdentity, time.Time) (*gbmodels.GbZLMManagedResource, error)
}

// ProxyOwnership is the narrow T6 preflight surface. Execute performs the
// resolver's second read and must reject changed ownership before action.
type ProxyOwnership interface {
	Preflight(context.Context, OwnershipTarget) (OwnershipPreflight, error)
	Execute(context.Context, OwnershipPreflight, func(context.Context, OwnershipTarget) error) error
}

type ProxyDependencies struct {
	Executor   ProxyExecutor
	Ledger     ProxyLedger
	Ownership  ProxyOwnership
	Capability ProxyCapabilityReader
}

type ProxyServiceOption func(*ProxyService)

func WithProxyOperationTimeout(timeout time.Duration) ProxyServiceOption {
	return func(service *ProxyService) {
		if timeout <= 0 {
			return
		}
		if timeout > DefaultProxyOperationTimeout {
			timeout = DefaultProxyOperationTimeout
		}
		service.operationTimeout = timeout
	}
}

func WithProxyClock(clock func() time.Time) ProxyServiceOption {
	return func(service *ProxyService) {
		if clock != nil {
			service.now = clock
		}
	}
}

type ProxyService struct {
	executor         ProxyExecutor
	ledger           ProxyLedger
	ownership        ProxyOwnership
	capability       ProxyCapabilityReader
	operationTimeout time.Duration
	now              func() time.Time
}

func NewProxyService(dependencies ProxyDependencies, options ...ProxyServiceOption) *ProxyService {
	service := &ProxyService{
		executor:         dependencies.Executor,
		ledger:           dependencies.Ledger,
		ownership:        dependencies.Ownership,
		capability:       dependencies.Capability,
		operationTimeout: DefaultProxyOperationTimeout,
		now:              time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// IngressService is kept as a domain-noun alias for future ingress
// controllers; it does not add a second implementation.
type IngressService = ProxyService

// NodeProxyExecutor adapts the concrete T4 NodeExecutor and T3 typed client
// to the narrow ProxyExecutor interface. It is the only production adapter
// in this file that knows about *zlm.Client.
type NodeProxyExecutor struct {
	executor *NodeExecutor
}

func NewNodeProxyExecutor(executor *NodeExecutor) *NodeProxyExecutor {
	return &NodeProxyExecutor{executor: executor}
}

func (e *NodeProxyExecutor) AddPull(ctx context.Context, nodeID int64, request zlm.StreamProxyRequest) (*zlm.ProxyCreateResult, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result *zlm.ProxyCreateResult
	err := e.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.AddStreamProxy(operationCtx, request)
		return err
	})
	return result, err
}

func (e *NodeProxyExecutor) AddPush(ctx context.Context, nodeID int64, request zlm.StreamPusherProxyRequest) (*zlm.ProxyCreateResult, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result *zlm.ProxyCreateResult
	err := e.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.AddStreamPusherProxy(operationCtx, request)
		return err
	})
	return result, err
}

func (e *NodeProxyExecutor) ListPull(ctx context.Context, nodeID int64, key ...string) ([]zlm.StreamProxyInfo, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result []zlm.StreamProxyInfo
	err := e.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.ListStreamProxy(operationCtx, key...)
		return err
	})
	return result, err
}

func (e *NodeProxyExecutor) ListPush(ctx context.Context, nodeID int64, key ...string) ([]zlm.StreamPusherProxyInfo, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result []zlm.StreamPusherProxyInfo
	err := e.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.ListStreamPusherProxy(operationCtx, key...)
		return err
	})
	return result, err
}

func (e *NodeProxyExecutor) DeletePull(ctx context.Context, nodeID int64, key string) (*zlm.ProxyDeleteResult, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result *zlm.ProxyDeleteResult
	err := e.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.DeleteStreamProxyWithResult(operationCtx, key)
		return err
	})
	return result, err
}

func (e *NodeProxyExecutor) DeletePush(ctx context.Context, nodeID int64, key string) (*zlm.ProxyDeleteResult, error) {
	if e == nil || e.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy executor is not configured")
	}
	var result *zlm.ProxyDeleteResult
	err := e.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.DeleteStreamPusherProxyWithResult(operationCtx, key)
		return err
	})
	return result, err
}

func (s *ProxyService) CreatePullProxy(ctx context.Context, actorUserID uint64, request PullProxyRequest) (*ProxyView, error) {
	if err := validateActor(actorUserID); err != nil {
		return nil, err
	}
	parsed, options, err := validatePullProxyRequest(request)
	if err != nil {
		return nil, err
	}
	if err := s.requireExecutorAndLedger(request.NodeID); err != nil {
		return nil, err
	}
	capability, err := s.checkCapability(ctx, request.NodeID, "addStreamProxy")
	if err != nil {
		return nil, err
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	result, err := s.executor.AddPull(opCtx, request.NodeID, zlm.StreamProxyRequest{
		VHost: request.Media.Vhost, App: request.Media.App, Stream: request.Media.Stream,
		URL: request.SourceURL, RetryCount: options.retryCount, RTPType: options.rtpType, TimeoutSec: options.timeoutSec,
	})
	if err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	if result == nil || strings.TrimSpace(result.Key) == "" {
		return nil, NewInternalError(nodeIDString(request.NodeID), "ZLM proxy creation returned no stable key")
	}
	key := strings.TrimSpace(result.Key)
	if err := validateProxyKey(key); err != nil {
		return nil, NewInternalError(nodeIDString(request.NodeID), "ZLM proxy creation returned an invalid key", err)
	}
	summary := summarizeProxyURL(request.SourceURL, parsed)
	identity := proxyLedgerIdentity(ProxyKindPull, request.NodeID, key, request.Media)
	fingerprint := proxyLedgerFingerprint(ProxyKindPull, identity, request.Media.Schema, request.SourceURL)
	registered, err := s.ledger.Register(opCtx, repo.ManagedResourceRegistration{
		Identity: identity, CreatedBy: actorUserID, Fingerprint: fingerprint,
		Summary: "pull source " + summary.Display,
	})
	if err != nil || registered == nil {
		return nil, s.compensateCreateFailure(ctx, ProxyKindPull, request.NodeID, key, err)
	}
	return &ProxyView{
		NodeID: request.NodeID, Kind: ProxyKindPull, Key: key, Media: request.Media,
		Online: true, StatusText: "created", Source: &summary, Capability: capability,
		Managed: true, CreatedBy: actorUserID, ProvenanceFingerprint: fingerprint,
		ProvenanceSummary: "pull source " + summary.Display,
	}, nil
}

func (s *ProxyService) CreatePushProxy(ctx context.Context, actorUserID uint64, request PushProxyRequest) (*ProxyView, error) {
	if err := validateActor(actorUserID); err != nil {
		return nil, err
	}
	parsed, schema, options, err := validatePushProxyRequest(request)
	if err != nil {
		return nil, err
	}
	if err := s.requireExecutorAndLedger(request.NodeID); err != nil {
		return nil, err
	}
	capability, err := s.checkCapability(ctx, request.NodeID, "addStreamPusherProxy")
	if err != nil {
		return nil, err
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	result, err := s.executor.AddPush(opCtx, request.NodeID, zlm.StreamPusherProxyRequest{
		Schema: schema, VHost: request.Media.Vhost, App: request.Media.App, Stream: request.Media.Stream,
		DstURL: request.TargetURL, RetryCount: options.retryCount, RTPType: options.rtpType, TimeoutSec: options.timeoutSec,
	})
	if err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	if result == nil || strings.TrimSpace(result.Key) == "" {
		return nil, NewInternalError(nodeIDString(request.NodeID), "ZLM proxy creation returned no stable key")
	}
	key := strings.TrimSpace(result.Key)
	if err := validateProxyKey(key); err != nil {
		return nil, NewInternalError(nodeIDString(request.NodeID), "ZLM proxy creation returned an invalid key", err)
	}
	summary := summarizeProxyURL(request.TargetURL, parsed)
	identity := proxyLedgerIdentity(ProxyKindPush, request.NodeID, key, request.Media)
	fingerprint := proxyLedgerFingerprint(ProxyKindPush, identity, request.Media.Schema, request.TargetURL)
	registered, err := s.ledger.Register(opCtx, repo.ManagedResourceRegistration{
		Identity: identity, CreatedBy: actorUserID, Fingerprint: fingerprint,
		Summary: "push target " + summary.Display,
	})
	if err != nil || registered == nil {
		return nil, s.compensateCreateFailure(ctx, ProxyKindPush, request.NodeID, key, err)
	}
	return &ProxyView{
		NodeID: request.NodeID, Kind: ProxyKindPush, Key: key, Media: request.Media,
		Online: true, StatusText: "created", Target: &summary, Capability: capability,
		Managed: true, CreatedBy: actorUserID, ProvenanceFingerprint: fingerprint,
		ProvenanceSummary: "push target " + summary.Display,
	}, nil
}

// compensateCreateFailure prevents a successful ZLM create followed by a
// ledger failure from being reported as success. Cleanup is best effort but
// remains bounded and uses the typed delete operation. A failed/unknown
// cleanup is surfaced as an explicit uncertain state for operators; no fake
// ledger row is written and no raw key or URL is placed in the error.
func (s *ProxyService) compensateCreateFailure(parent context.Context, kind ProxyKind, nodeID int64, key string, cause error) error {
	cleanupBase := context.WithoutCancel(nonNilContext(parent))
	cleanupCtx, cancel := s.operationContext(cleanupBase)
	defer cancel()
	var result *zlm.ProxyDeleteResult
	var err error
	if kind == ProxyKindPull {
		result, err = s.executor.DeletePull(cleanupCtx, nodeID, key)
	} else {
		result, err = s.executor.DeletePush(cleanupCtx, nodeID, key)
	}
	if err == nil && result != nil && strings.TrimSpace(result.Key) == key {
		return NewInternalError(nodeIDString(nodeID), "proxy creation was rolled back because provenance registration failed", cause)
	}
	return NewInternalError(nodeIDString(nodeID), "proxy creation state is uncertain after provenance registration and cleanup failure", cause)
}

// AddPullProxy and AddPushProxy are concise aliases used by ingress adapters.
func (s *ProxyService) AddPullProxy(ctx context.Context, actorUserID uint64, request PullProxyRequest) (*ProxyView, error) {
	return s.CreatePullProxy(ctx, actorUserID, request)
}

func (s *ProxyService) AddPushProxy(ctx context.Context, actorUserID uint64, request PushProxyRequest) (*ProxyView, error) {
	return s.CreatePushProxy(ctx, actorUserID, request)
}

func (s *ProxyService) ListPullProxies(ctx context.Context, nodeID int64, requests ...PageRequest) (ProxyPage, error) {
	return s.listProxies(ctx, ProxyKindPull, nodeID, requests)
}

func (s *ProxyService) ListPushProxies(ctx context.Context, nodeID int64, requests ...PageRequest) (ProxyPage, error) {
	return s.listProxies(ctx, ProxyKindPush, nodeID, requests)
}

func (s *ProxyService) ListPullProxy(ctx context.Context, nodeID int64, requests ...PageRequest) (ProxyPage, error) {
	return s.ListPullProxies(ctx, nodeID, requests...)
}

func (s *ProxyService) ListPushProxy(ctx context.Context, nodeID int64, requests ...PageRequest) (ProxyPage, error) {
	return s.ListPushProxies(ctx, nodeID, requests...)
}

func (s *ProxyService) listProxies(ctx context.Context, kind ProxyKind, nodeID int64, requests []PageRequest) (ProxyPage, error) {
	page := ProxyPage{Page: Page[ProxyView]{List: make([]ProxyView, 0), Page: 1, PageSize: DefaultPageSize}, Capability: zlm.CapabilityUnknown}
	if err := validateNodeID(nodeID); err != nil {
		return page, err
	}
	pageRequest, err := onePageRequest(requests)
	if err != nil {
		return page, err
	}
	capability, err := s.checkCapability(ctx, nodeID, proxyListAPI(kind))
	if err != nil {
		return page, err
	}
	if s == nil || s.executor == nil {
		return page, NewInternalError(nodeIDString(nodeID), "proxy service is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	items, err := s.listRaw(opCtx, kind, nodeID)
	if err != nil {
		return page, normalizeProxyError(err, nodeID)
	}
	rows, err := s.ledgerRows(opCtx, nodeID, kind)
	if err != nil {
		return page, normalizeProxyError(err, nodeID)
	}
	views := make([]ProxyView, 0, len(items))
	for _, item := range items {
		view, err := proxyViewFromRaw(kind, nodeID, item)
		if err != nil {
			return page, NewInternalError(nodeIDString(nodeID), "ZLM proxy list returned an invalid item", err)
		}
		applyLedgerProvenance(&view, rows)
		view.Capability = capability
		views = append(views, view)
	}
	result := Paginate(views, pageRequest)
	return ProxyPage{Page: result, Capability: capability}, nil
}

func (s *ProxyService) GetPullProxy(ctx context.Context, nodeID int64, key string) (*ProxyView, error) {
	return s.getProxy(ctx, ProxyKindPull, nodeID, key)
}

func (s *ProxyService) GetPushProxy(ctx context.Context, nodeID int64, key string) (*ProxyView, error) {
	return s.getProxy(ctx, ProxyKindPush, nodeID, key)
}

func (s *ProxyService) ReadPullProxy(ctx context.Context, nodeID int64, key string) (*ProxyView, error) {
	return s.GetPullProxy(ctx, nodeID, key)
}

func (s *ProxyService) ReadPushProxy(ctx context.Context, nodeID int64, key string) (*ProxyView, error) {
	return s.GetPushProxy(ctx, nodeID, key)
}

func (s *ProxyService) getProxy(ctx context.Context, kind ProxyKind, nodeID int64, key string) (*ProxyView, error) {
	if err := validateNodeID(nodeID); err != nil {
		return nil, err
	}
	if err := validateProxyKey(key); err != nil {
		return nil, NewValidationError(map[string]string{"key": "is invalid"})
	}
	capability, err := s.checkCapability(ctx, nodeID, proxyListAPI(kind))
	if err != nil {
		return nil, err
	}
	if s == nil || s.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "proxy service is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	items, err := s.listRawWithKey(opCtx, kind, nodeID, key)
	if err != nil {
		return nil, normalizeProxyError(err, nodeID)
	}
	rows, err := s.ledgerRows(opCtx, nodeID, kind)
	if err != nil {
		return nil, normalizeProxyError(err, nodeID)
	}
	for _, item := range items {
		view, err := proxyViewFromRaw(kind, nodeID, item)
		if err != nil {
			return nil, NewInternalError(nodeIDString(nodeID), "ZLM proxy list returned an invalid item", err)
		}
		if view.Key != key {
			continue
		}
		applyLedgerProvenance(&view, rows)
		view.Capability = capability
		return &view, nil
	}
	// ZLM is the existence source of truth. A stale ledger row must not turn a
	// missing ZLM resource into an online response.
	return nil, nil
}

func (s *ProxyService) PreviewDeletePullProxy(ctx context.Context, request ProxyDeleteRequest) (ProxyDeletePreview, error) {
	return s.previewDelete(ctx, ProxyKindPull, request)
}

func (s *ProxyService) PreviewDeletePushProxy(ctx context.Context, request ProxyDeleteRequest) (ProxyDeletePreview, error) {
	return s.previewDelete(ctx, ProxyKindPush, request)
}

func (s *ProxyService) previewDelete(ctx context.Context, kind ProxyKind, request ProxyDeleteRequest) (ProxyDeletePreview, error) {
	preview := ProxyDeletePreview{NodeID: request.NodeID, Kind: kind, Key: request.Key, Target: OwnershipTarget{NodeID: request.NodeID, Media: request.Media}}
	if err := validateDeleteRequest(request); err != nil {
		return preview, err
	}
	if s == nil || s.ledger == nil || s.ownership == nil {
		return preview, NewInternalError(nodeIDString(request.NodeID), "proxy ownership preflight is not configured")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	ledgerRow, err := s.activeProxyLedgerRow(opCtx, kind, request)
	if err != nil {
		return preview, normalizeProxyError(err, request.NodeID)
	}
	preflight, err := s.ownership.Preflight(opCtx, preview.Target)
	if err != nil {
		return preview, normalizeProxyError(err, request.NodeID)
	}
	preview.Fingerprint = proxyDeleteFingerprint(kind, request, preflight, ledgerRow)
	preview.Status = preflight.Snapshot.Status
	preview.Present = preflight.Snapshot.Present
	preview.PresenceKnown = preflight.Snapshot.PresenceKnown
	preview.Impacts = safeImpacts(preflight.Snapshot.Impacts)
	return preview, nil
}

func (s *ProxyService) DeletePullProxy(ctx context.Context, actorUserID uint64, request ProxyDeleteRequest) (*ProxyDeleteView, error) {
	return s.deleteProxy(ctx, actorUserID, ProxyKindPull, request)
}

func (s *ProxyService) DeletePushProxy(ctx context.Context, actorUserID uint64, request ProxyDeleteRequest) (*ProxyDeleteView, error) {
	return s.deleteProxy(ctx, actorUserID, ProxyKindPush, request)
}

func (s *ProxyService) DeletePull(ctx context.Context, actorUserID uint64, request ProxyDeleteRequest) (*ProxyDeleteView, error) {
	return s.DeletePullProxy(ctx, actorUserID, request)
}

func (s *ProxyService) DeletePush(ctx context.Context, actorUserID uint64, request ProxyDeleteRequest) (*ProxyDeleteView, error) {
	return s.DeletePushProxy(ctx, actorUserID, request)
}

func (s *ProxyService) deleteProxy(ctx context.Context, actorUserID uint64, kind ProxyKind, request ProxyDeleteRequest) (*ProxyDeleteView, error) {
	if err := validateActor(actorUserID); err != nil {
		return nil, err
	}
	if err := validateDeleteRequest(request); err != nil {
		return nil, err
	}
	if s == nil || s.executor == nil || s.ledger == nil || s.ownership == nil {
		return nil, NewInternalError(nodeIDString(request.NodeID), "proxy deletion dependencies are not configured")
	}
	_, err := s.checkCapability(ctx, request.NodeID, proxyDeleteAPI(kind))
	if err != nil {
		return nil, err
	}
	target := OwnershipTarget{NodeID: request.NodeID, Media: request.Media}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	ledgerRow, err := s.activeProxyLedgerRow(opCtx, kind, request)
	if err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	preflight, err := s.ownership.Preflight(opCtx, target)
	if err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	confirmationFingerprint := proxyDeleteFingerprint(kind, request, preflight, ledgerRow)
	if request.Fingerprint != "" && safeFingerprint(request.Fingerprint) != confirmationFingerprint {
		return nil, ownershipConflict(target, preflight.Snapshot, "ownership fingerprint changed; reconfirm required")
	}

	var deleteResult *zlm.ProxyDeleteResult
	err = s.ownership.Execute(opCtx, preflight, func(actionCtx context.Context, actionTarget OwnershipTarget) error {
		if actionTarget != target {
			return ownershipConflict(target, preflight.Snapshot, "ownership target changed; reconfirm required")
		}
		currentLedgerRow, ledgerErr := s.activeProxyLedgerRow(actionCtx, kind, request)
		if ledgerErr != nil {
			return ledgerErr
		}
		if !sameProxyLedgerProvenance(ledgerRow, currentLedgerRow) {
			return ownershipConflict(target, preflight.Snapshot, "proxy provenance changed; reconfirm required")
		}
		var actionErr error
		if kind == ProxyKindPull {
			deleteResult, actionErr = s.executor.DeletePull(actionCtx, request.NodeID, request.Key)
		} else {
			deleteResult, actionErr = s.executor.DeletePush(actionCtx, request.NodeID, request.Key)
		}
		if actionErr != nil {
			return actionErr
		}
		if deleteResult == nil {
			return errors.New("ZLM proxy delete returned no result")
		}
		if strings.TrimSpace(deleteResult.Key) != request.Key {
			return errors.New("ZLM proxy delete returned a different key")
		}
		return nil
	})
	if err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	if deleteResult == nil {
		return nil, NewInternalError(nodeIDString(request.NodeID), "ZLM proxy delete returned no result")
	}
	identity := proxyLedgerIdentity(kind, request.NodeID, request.Key, request.Media)
	if _, err := s.ledger.Tombstone(opCtx, identity, s.clock()); err != nil {
		return nil, normalizeProxyError(err, request.NodeID)
	}
	return &ProxyDeleteView{
		NodeID: request.NodeID, Kind: kind, Key: request.Key,
		Removed: deleteResult.Hit, AlreadyAbsent: !deleteResult.Hit, Tombstoned: true,
	}, nil
}

type proxyRequestOptions struct {
	retryCount int
	rtpType    int
	timeoutSec float64
}

var (
	pullProxySchemes = map[string]struct{}{
		"rtsp": {}, "rtsps": {}, "rtmp": {}, "rtmps": {}, "http": {}, "https": {},
	}
	pushProxySchemes = map[string]struct{}{
		"rtsp": {}, "rtsps": {}, "rtmp": {}, "rtmps": {},
	}
)

func validatePullProxyRequest(request PullProxyRequest) (*url.URL, proxyRequestOptions, error) {
	if err := validateNodeID(request.NodeID); err != nil {
		return nil, proxyRequestOptions{}, err
	}
	if err := request.Media.Validate(); err != nil {
		return nil, proxyRequestOptions{}, err
	}
	parsed, err := parseProxyURL(request.SourceURL, pullProxySchemes)
	if err != nil {
		return nil, proxyRequestOptions{}, NewValidationError(map[string]string{"sourceUrl": "must be a supported URL"})
	}
	options, err := normalizeProxyOptions(request.RetryCount, request.RTPType, request.TimeoutSec)
	if err != nil {
		return nil, proxyRequestOptions{}, err
	}
	return parsed, options, nil
}

func validatePushProxyRequest(request PushProxyRequest) (*url.URL, string, proxyRequestOptions, error) {
	if err := validateNodeID(request.NodeID); err != nil {
		return nil, "", proxyRequestOptions{}, err
	}
	if err := request.Media.Validate(); err != nil {
		return nil, "", proxyRequestOptions{}, err
	}
	schema := strings.ToLower(request.Media.Schema)
	if _, ok := pushProxySchemes[schema]; !ok {
		return nil, "", proxyRequestOptions{}, NewValidationError(map[string]string{"media.schema": "must be a supported push protocol"})
	}
	parsed, err := parseProxyURL(request.TargetURL, pushProxySchemes)
	if err != nil || !sameProxyProtocol(schema, parsed.Scheme) {
		return nil, "", proxyRequestOptions{}, NewValidationError(map[string]string{"targetUrl": "must match the local push protocol"})
	}
	options, err := normalizeProxyOptions(request.RetryCount, request.RTPType, request.TimeoutSec)
	if err != nil {
		return nil, "", proxyRequestOptions{}, err
	}
	return parsed, schema, options, nil
}

func normalizeProxyOptions(retryCount, rtpType int, timeoutSec float64) (proxyRequestOptions, error) {
	if retryCount < 0 || retryCount > MaxProxyRetryCount {
		return proxyRequestOptions{}, NewValidationError(map[string]string{"retryCount": "is outside the supported range"})
	}
	if rtpType < 0 || rtpType > 2 {
		return proxyRequestOptions{}, NewValidationError(map[string]string{"rtpType": "is outside the supported range"})
	}
	if math.IsNaN(timeoutSec) || math.IsInf(timeoutSec, 0) || timeoutSec < 0 || timeoutSec > MaxProxyTimeoutSec {
		return proxyRequestOptions{}, NewValidationError(map[string]string{"timeoutSec": "is outside the supported range"})
	}
	if retryCount == 0 {
		retryCount = DefaultProxyRetryCount
	}
	if timeoutSec == 0 {
		timeoutSec = DefaultProxyTimeoutSec
	}
	if timeoutSec < 0.1 {
		return proxyRequestOptions{}, NewValidationError(map[string]string{"timeoutSec": "must be at least 0.1 seconds"})
	}
	return proxyRequestOptions{retryCount: retryCount, rtpType: rtpType, timeoutSec: timeoutSec}, nil
}

func parseProxyURL(raw string, allowed map[string]struct{}) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) != raw {
		return nil, errors.New("invalid proxy URL")
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return nil, errors.New("invalid proxy URL")
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid proxy URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if _, ok := allowed[scheme]; !ok || parsed.Hostname() == "" {
		return nil, errors.New("unsupported proxy URL scheme")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, errors.New("invalid proxy URL port")
		}
	}
	parsed.Scheme = scheme
	return parsed, nil
}

func sameProxyProtocol(schema, targetScheme string) bool {
	base := func(value string) string {
		value = strings.ToLower(value)
		return strings.TrimSuffix(value, "s")
	}
	return base(schema) == base(targetScheme)
}

func summarizeProxyURL(raw string, parsed *url.URL) ProxyURLSummary {
	host := parsed.Hostname()
	port := 0
	if parsed.Port() != "" {
		port, _ = strconv.Atoi(parsed.Port())
	}
	hostPort := host
	if port != 0 {
		hostPort = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return ProxyURLSummary{
		Scheme: parsed.Scheme, Host: host, Port: port,
		Fingerprint: fingerprintProxyURL(raw), HasUserInfo: parsed.User != nil,
		HasSensitiveQuery: proxyURLHasSensitiveQuery(parsed), Display: parsed.Scheme + "://" + hostPort,
	}
}

func fingerprintProxyURL(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func proxyURLHasSensitiveQuery(parsed *url.URL) bool {
	if parsed == nil || parsed.RawQuery == "" {
		return false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return true
	}
	for key := range values {
		lower := strings.ToLower(key)
		for _, marker := range []string{"token", "secret", "password", "passwd", "authorization", "credential", "signature", "sig", "api_key", "apikey", "access_key"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

func (s *ProxyService) checkCapability(ctx context.Context, nodeID int64, api string) (zlm.CapabilityState, error) {
	if s == nil || s.capability == nil {
		return zlm.CapabilityUnknown, nil
	}
	probeCtx, cancel := s.operationContext(ctx)
	defer cancel()
	profile, probeErr := s.capability.GetCapabilityProfile(probeCtx, nodeID)
	state := profile.Status(api)
	if state == zlm.CapabilityUnsupported || errors.Is(probeErr, zlm.ErrCapabilityUnsupported) {
		return zlm.CapabilityUnknown, NewUnsupportedCapabilityError(nodeIDString(nodeID), api, probeErr)
	}
	if state != zlm.CapabilitySupported {
		return zlm.CapabilityUnknown, nil
	}
	return zlm.CapabilitySupported, nil
}

func (s *ProxyService) requireExecutorAndLedger(nodeID int64) error {
	if s == nil || s.executor == nil || s.ledger == nil {
		return NewInternalError(nodeIDString(nodeID), "proxy service dependencies are not configured")
	}
	return nil
}

func (s *ProxyService) listRaw(ctx context.Context, kind ProxyKind, nodeID int64) ([]zlm.StreamProxyInfo, error) {
	if kind == ProxyKindPull {
		return s.executor.ListPull(ctx, nodeID)
	}
	items, err := s.executor.ListPush(ctx, nodeID)
	return items, err
}

func (s *ProxyService) listRawWithKey(ctx context.Context, kind ProxyKind, nodeID int64, key string) ([]zlm.StreamProxyInfo, error) {
	if kind == ProxyKindPull {
		return s.executor.ListPull(ctx, nodeID, key)
	}
	items, err := s.executor.ListPush(ctx, nodeID, key)
	return items, err
}

func (s *ProxyService) ledgerRows(ctx context.Context, nodeID int64, kind ProxyKind) (map[string]gbmodels.GbZLMManagedResource, error) {
	rows := make(map[string]gbmodels.GbZLMManagedResource)
	if s == nil || s.ledger == nil {
		return rows, nil
	}
	items, err := s.ledger.List(ctx, repo.ManagedResourceFilter{NodeID: nodeID, ResourceType: string(kind)})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.TombstonedAt != nil || item.NodeID != nodeID || item.ResourceType != string(kind) {
			continue
		}
		if validateProxyKey(item.ResourceKey) != nil {
			continue
		}
		rows[item.ResourceKey] = item
	}
	return rows, nil
}

// activeProxyLedgerRow is the exact provenance gate for ordinary deletion.
// T6 ownership is intentionally stream-scoped because several business
// owners can share one stream; the ledger row additionally binds kind, key,
// app and stream so another proxy on the same stream cannot authorize this
// request.
func (s *ProxyService) activeProxyLedgerRow(ctx context.Context, kind ProxyKind, request ProxyDeleteRequest) (gbmodels.GbZLMManagedResource, error) {
	rows, err := s.ledger.List(ctx, repo.ManagedResourceFilter{NodeID: request.NodeID, ResourceType: string(kind)})
	if err != nil {
		return gbmodels.GbZLMManagedResource{}, err
	}
	for _, row := range rows {
		if row.TombstonedAt != nil || row.NodeID != request.NodeID || row.ResourceType != string(kind) {
			continue
		}
		if row.ResourceKey != request.Key || row.App != request.Media.App || row.Stream != request.Media.Stream {
			continue
		}
		return row, nil
	}
	return gbmodels.GbZLMManagedResource{}, NewOwnershipConflictError(nodeIDString(request.NodeID), "proxy provenance is not registered", ErrOwnershipConflict)
}

func sameProxyLedgerProvenance(left, right gbmodels.GbZLMManagedResource) bool {
	return left.NodeID == right.NodeID && left.ResourceType == right.ResourceType &&
		left.ResourceKey == right.ResourceKey && left.App == right.App && left.Stream == right.Stream &&
		left.IdentityFingerprint == right.IdentityFingerprint && left.TombstonedAt == nil && right.TombstonedAt == nil
}

func proxyDeleteFingerprint(kind ProxyKind, request ProxyDeleteRequest, preflight OwnershipPreflight, ledgerRow gbmodels.GbZLMManagedResource) string {
	return repo.FingerprintManagedResourceParts(
		string(kind), nodeIDString(request.NodeID), request.Key,
		request.Media.Schema, request.Media.Vhost, request.Media.App, request.Media.Stream,
		preflight.Fingerprint, ledgerRow.IdentityFingerprint,
	)
}

func proxyViewFromRaw(kind ProxyKind, nodeID int64, item zlm.StreamProxyInfo) (ProxyView, error) {
	key := strings.TrimSpace(item.Key)
	if err := validateProxyKey(key); err != nil {
		return ProxyView{}, err
	}
	view := ProxyView{
		NodeID: nodeID, Kind: kind, Key: key, Online: true, Status: item.Status,
		StatusText: safeProxyStatusText(item.StatusStr), LiveSecs: maxInt64(item.LiveSecs),
		RePullCount: maxInt(item.RePullCount), RePublishCount: maxInt(item.RePublishCount),
		TotalReaderCount: maxInt(item.TotalReaderCount), BytesSpeed: item.BytesSpeed, TotalBytes: item.TotalBytes,
		Media: safeProxyMedia(item.Src), Capability: zlm.CapabilityUnknown,
	}
	if item.URL != "" {
		parsed, err := parseProxyURL(item.URL, pullProxySchemes)
		if err != nil {
			// An upstream URL is never echoed. Keep the live item visible while
			// omitting a summary that cannot be safely parsed.
			return view, nil
		}
		summary := summarizeProxyURL(item.URL, parsed)
		if kind == ProxyKindPull {
			view.Source = &summary
		} else {
			view.Target = &summary
		}
	}
	return view, nil
}

func applyLedgerProvenance(view *ProxyView, rows map[string]gbmodels.GbZLMManagedResource) {
	if view == nil {
		return
	}
	row, ok := rows[view.Key]
	if !ok || row.App != view.Media.App || row.Stream != view.Media.Stream {
		return
	}
	view.Managed = true
	view.CreatedBy = row.CreatedBy
	view.ProvenanceFingerprint = safeFingerprint(row.IdentityFingerprint)
	view.ProvenanceSummary = repo.RedactManagedResourceSummary(row.Summary)
}

func safeProxyMedia(tuple *zlm.ProxyMediaTuple) MediaIdentity {
	if tuple == nil {
		return MediaIdentity{}
	}
	return MediaIdentity{
		Vhost:  safeProxyIdentityPart(tuple.VHost),
		App:    safeProxyIdentityPart(tuple.App),
		Stream: safeProxyIdentityPart(tuple.Stream),
	}
}

func safeProxyIdentityPart(value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(value), "://") || containsSensitiveProxyMarker(value) {
		return "[redacted]"
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "[redacted]"
		}
	}
	if utf8.RuneCountInString(value) > MaxMediaIdentityFieldLength {
		runes := []rune(value)
		return string(runes[:MaxMediaIdentityFieldLength])
	}
	return value
}

func safeProxyStatusText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(value), "://") || containsSensitiveProxyMarker(value) {
		return "[redacted]"
	}
	return sanitizeErrorText(value)
}

func safeImpacts(impacts []Impact) []Impact {
	if len(impacts) == 0 {
		return nil
	}
	copy := make([]Impact, 0, len(impacts))
	for _, impact := range impacts {
		media := impact.MediaIdentity
		if media != nil {
			cloned := *media
			cloned.Schema = safeProxyIdentityPart(cloned.Schema)
			cloned.Vhost = safeProxyIdentityPart(cloned.Vhost)
			cloned.App = safeProxyIdentityPart(cloned.App)
			cloned.Stream = safeProxyIdentityPart(cloned.Stream)
			media = &cloned
		}
		copy = append(copy, Impact{
			ResourceType: safeProxyIdentityPart(impact.ResourceType), ResourceKey: safeProxyIdentityPart(impact.ResourceKey),
			Owner: safeProxyIdentityPart(impact.Owner), Reason: safeProxyStatusText(impact.Reason), MediaIdentity: media,
		})
	}
	return copy
}

func proxyLedgerIdentity(kind ProxyKind, nodeID int64, key string, media MediaIdentity) repo.ManagedResourceIdentity {
	return repo.ManagedResourceIdentity{NodeID: nodeID, ResourceType: string(kind), ResourceKey: key, App: media.App, Stream: media.Stream}
}

func proxyLedgerFingerprint(kind ProxyKind, identity repo.ManagedResourceIdentity, schema, rawURL string) string {
	return repo.FingerprintManagedResourceParts(string(kind), strconv.FormatInt(identity.NodeID, 10), identity.ResourceKey, schema, identity.App, identity.Stream, rawURL)
}

func safeFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return strings.ToLower(value)
}

func validateDeleteRequest(request ProxyDeleteRequest) error {
	if err := validateNodeID(request.NodeID); err != nil {
		return err
	}
	if err := request.Media.Validate(); err != nil {
		return err
	}
	if err := validateProxyKey(request.Key); err != nil {
		return NewValidationError(map[string]string{"key": "must be a stable resource key"})
	}
	if request.Fingerprint != "" && safeFingerprint(request.Fingerprint) == "" {
		return NewValidationError(map[string]string{"fingerprint": "must be a SHA-256 fingerprint"})
	}
	return nil
}

func validateActor(actorUserID uint64) error {
	if actorUserID == 0 {
		return NewValidationError(map[string]string{"actorUserId": "is required"})
	}
	return nil
}

func validateNodeID(nodeID int64) error {
	if nodeID <= 0 {
		return NewValidationError(map[string]string{"nodeId": "must be positive"})
	}
	return nil
}

func validateProxyKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || trimmed != key || utf8.RuneCountInString(trimmed) > MaxMediaIdentityFieldLength {
		return errors.New("invalid proxy key")
	}
	if strings.Contains(strings.ToLower(trimmed), "://") || containsSensitiveProxyMarker(trimmed) {
		return errors.New("invalid proxy key")
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return errors.New("invalid proxy key")
		}
	}
	return nil
}

func containsSensitiveProxyMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"token=", "secret=", "password=", "authorization="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func proxyListAPI(kind ProxyKind) string {
	if kind == ProxyKindPull {
		return "listStreamProxy"
	}
	return "listStreamPusherProxy"
}

func proxyDeleteAPI(kind ProxyKind) string {
	if kind == ProxyKindPull {
		return "delStreamProxy"
	}
	return "delStreamPusherProxy"
}

func onePageRequest(requests []PageRequest) (PageRequest, error) {
	if len(requests) > 1 {
		return PageRequest{}, NewValidationError(map[string]string{"page": "only one page request is allowed"})
	}
	if len(requests) == 1 {
		return requests[0], nil
	}
	return PageRequest{}, nil
}

func (s *ProxyService) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := DefaultProxyOperationTimeout
	if s != nil && s.operationTimeout > 0 && s.operationTimeout <= DefaultProxyOperationTimeout {
		timeout = s.operationTimeout
	}
	return context.WithTimeout(nonNilContext(ctx), timeout)
}

func (s *ProxyService) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now()
	}
	return s.now()
}

func normalizeProxyError(err error, nodeID int64) error {
	if err == nil {
		return nil
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) && validationErr != nil {
		return validationErr
	}
	if managementErr, ok := AsManagementError(err); ok {
		return managementErr
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrUpstreamTimeout):
		return NewUpstreamTimeoutError(nodeIDString(nodeID), err)
	case errors.Is(err, zlm.ErrCapabilityUnsupported), errors.Is(err, ErrUnsupportedCapability):
		return NewUnsupportedCapabilityError(nodeIDString(nodeID), "", err)
	case errors.Is(err, ErrNodeOffline):
		return NewNodeOfflineError(nodeIDString(nodeID), err)
	default:
		return NewInternalError(nodeIDString(nodeID), "ZLM proxy operation failed", err)
	}
}

func maxInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func maxInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
