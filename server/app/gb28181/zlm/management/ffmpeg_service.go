package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

const (
	// ResourceTypeFFmpegSource is the ledger identity for an FFmpeg source.
	// The value is deliberately stable because it is also used by the T6
	// managed-resource ownership adapter.
	ResourceTypeFFmpegSource = "ffmpeg_source"
	ffmpegLedgerApp          = "ffmpeg"
	maxFFmpegURLLength       = 4096
	maxFFmpegTimeoutMS       = 5 * 60 * 1000
)

// FFmpegTemplateRegistry is the only command-template input accepted by the
// management service. It exposes registration, never the shell command
// itself, so a caller cannot smuggle arbitrary command text through this
// boundary.
type FFmpegTemplateRegistry interface {
	IsRegistered(string) bool
}

// FFmpegTemplateSet is a small immutable-by-convention allow-list useful for
// wiring configuration snapshots and tests. The map contains keys only.
type FFmpegTemplateSet map[string]struct{}

func NewFFmpegTemplateSet(keys ...string) FFmpegTemplateSet {
	set := make(FFmpegTemplateSet, len(keys))
	for _, key := range keys {
		if validateFFmpegTemplateKey(key) == nil {
			set[key] = struct{}{}
		}
	}
	return set
}

func (s FFmpegTemplateSet) IsRegistered(key string) bool {
	_, ok := s[key]
	return ok
}

// FFmpegSourceCreateRequest contains only typed source/target options. Actor
// identity is deliberately not part of this request: callers must provide it
// as a trusted method argument obtained from the authenticated context.
type FFmpegSourceCreateRequest struct {
	TemplateKey string `json:"templateKey"`
	SrcURL      string `json:"srcUrl"`
	DstURL      string `json:"dstUrl"`
	TimeoutMS   int    `json:"timeoutMs"`
	EnableHLS   bool   `json:"enableHls"`
	EnableMP4   bool   `json:"enableMp4"`
}

// FFmpegCreateRequest is a shorter compatibility spelling.
type FFmpegCreateRequest = FFmpegSourceCreateRequest

// FFmpegSourceDeleteRequest binds a delete to one node and one ZLM key. It
// intentionally has no Force field; force deletion is a separate method and
// request type.
type FFmpegSourceDeleteRequest struct {
	NodeID int64  `json:"nodeId"`
	Key    string `json:"key"`
}

// FFmpegURLView is the only URL representation returned by this service.
// Fingerprint is a SHA-256 digest of the exact input; Summary contains only a
// scheme and host/port and never a path, query, fragment or userinfo.
type FFmpegURLView struct {
	Summary     string `json:"summary"`
	Fingerprint string `json:"fingerprint"`
}

// FFmpegSourceView is a browser-safe view of one FFmpeg source. In
// particular, it has no raw ZLM cmd field and no raw source/target URL.
type FFmpegSourceView struct {
	NodeID      int64         `json:"nodeId"`
	Key         string        `json:"key"`
	TemplateKey string        `json:"templateKey"`
	SrcURL      FFmpegURLView `json:"srcUrl"`
	DstURL      FFmpegURLView `json:"dstUrl"`
	CreatedBy   uint64        `json:"createdBy,omitempty"`
	Managed     bool          `json:"managed"`
}

// FFmpegSourcePage is the bounded list contract used by management adapters.
// ZLM's list endpoint has no native pagination, so the service applies the
// shared MaxResponseItems cap before returning this page.
type FFmpegSourcePage = Page[FFmpegSourceView]

// FFmpegSourceDTO is a descriptive alias used by HTTP adapters.
type FFmpegSourceDTO = FFmpegSourceView

// FFmpegDeleteResult makes successful deletion and already-absent deletion
// distinct while keeping both idempotent outcomes safe for callers.
type FFmpegDeleteResult struct {
	NodeID          int64  `json:"nodeId"`
	Key             string `json:"key"`
	Released        bool   `json:"released"`
	AlreadyReleased bool   `json:"alreadyReleased"`
	Idempotent      bool   `json:"idempotent"`
}

// FFmpegClient is the narrow typed adapter required by this service. T14 can
// implement it with NodeExecutor and zlm.Client; arbitrary API names/maps are
// not part of the interface.
type FFmpegClient interface {
	AddFFmpegSource(context.Context, int64, zlm.FFmpegSourceRequest) (*zlm.FFmpegSourceResult, error)
	ListFFmpegSources(context.Context, int64) ([]zlm.FFmpegSourceInfo, error)
	DeleteFFmpegSource(context.Context, int64, string) (*zlm.ProxyDeleteResult, error)
}

// FFmpegResourceLedger is the mutation subset of the T5 repository needed by
// FFmpeg. Keeping it local prevents the service from gaining unrelated DB
// operations.
type FFmpegResourceLedger interface {
	Register(context.Context, repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error)
	Find(context.Context, repo.ManagedResourceIdentity) (*gbmodels.GbZLMManagedResource, error)
	Tombstone(context.Context, repo.ManagedResourceIdentity, time.Time) (*gbmodels.GbZLMManagedResource, error)
}

// FFmpegCapabilityState is intentionally separate from the runtime-client's
// named API set. T14 adapters may know ingress support from a node profile;
// unknown means a typed operation may be attempted once and classified from
// its response.
type FFmpegCapabilityState string

const (
	FFmpegCapabilitySupported   FFmpegCapabilityState = "supported"
	FFmpegCapabilityUnsupported FFmpegCapabilityState = "unsupported"
	FFmpegCapabilityUnknown     FFmpegCapabilityState = "unknown"
)

type FFmpegCapabilityReader interface {
	FFmpegCapability(context.Context, int64) (FFmpegCapabilityState, error)
}

type FFmpegDependencies struct {
	Client     FFmpegClient
	Templates  FFmpegTemplateRegistry
	Ledger     FFmpegResourceLedger
	Ownership  *OwnershipResolver
	Capability FFmpegCapabilityReader
}

type FFmpegService struct {
	client     FFmpegClient
	templates  FFmpegTemplateRegistry
	ledger     FFmpegResourceLedger
	ownership  *OwnershipResolver
	capability FFmpegCapabilityReader
	now        func() time.Time
}

func NewFFmpegService(dependencies FFmpegDependencies) *FFmpegService {
	return &FFmpegService{
		client:     dependencies.Client,
		templates:  dependencies.Templates,
		ledger:     dependencies.Ledger,
		ownership:  dependencies.Ownership,
		capability: dependencies.Capability,
		now:        time.Now,
	}
}

// FFmpegNodeClientAdapter binds the typed service interface to T4's
// NodeExecutor. The node executor remains responsible for state, access,
// timeout and error normalization.
type FFmpegNodeClientAdapter struct {
	executor *NodeExecutor
}

func NewFFmpegNodeClientAdapter(executor *NodeExecutor) *FFmpegNodeClientAdapter {
	return &FFmpegNodeClientAdapter{executor: executor}
}

func (a *FFmpegNodeClientAdapter) AddFFmpegSource(ctx context.Context, nodeID int64, request zlm.FFmpegSourceRequest) (*zlm.FFmpegSourceResult, error) {
	if a == nil || a.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "FFmpeg client adapter is not configured")
	}
	var result *zlm.FFmpegSourceResult
	err := a.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.AddFFmpegSource(operationCtx, request)
		return err
	})
	return result, err
}

func (a *FFmpegNodeClientAdapter) ListFFmpegSources(ctx context.Context, nodeID int64) ([]zlm.FFmpegSourceInfo, error) {
	if a == nil || a.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "FFmpeg client adapter is not configured")
	}
	var result []zlm.FFmpegSourceInfo
	err := a.executor.ExecuteRead(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.ListFFmpegSources(operationCtx)
		return err
	})
	return result, err
}

func (a *FFmpegNodeClientAdapter) DeleteFFmpegSource(ctx context.Context, nodeID int64, key string) (*zlm.ProxyDeleteResult, error) {
	if a == nil || a.executor == nil {
		return nil, NewInternalError(nodeIDString(nodeID), "FFmpeg client adapter is not configured")
	}
	var result *zlm.ProxyDeleteResult
	err := a.executor.ExecuteWrite(ctx, nodeID, func(operationCtx context.Context, client *zlm.Client) error {
		var err error
		result, err = client.DeleteFFmpegSourceWithResult(operationCtx, key)
		return err
	})
	return result, err
}

// Create adds an FFmpeg source and writes provenance only after ZLM returns a
// non-empty key. actorUserID is trusted server-side identity, not request JSON.
func (s *FFmpegService) Create(ctx context.Context, nodeID int64, actorUserID uint64, request FFmpegSourceCreateRequest) (FFmpegSourceView, error) {
	if err := validateFFmpegCreateRequest(nodeID, actorUserID, request); err != nil {
		return FFmpegSourceView{}, err
	}
	if err := s.requireConfigured(nodeID, true); err != nil {
		return FFmpegSourceView{}, err
	}
	if !s.templates.IsRegistered(request.TemplateKey) {
		return FFmpegSourceView{}, NewValidationError(map[string]string{"templateKey": "is not a registered template"})
	}
	if err := s.checkCapability(ctx, nodeID, "addFFmpegSource"); err != nil {
		return FFmpegSourceView{}, err
	}
	created, err := s.client.AddFFmpegSource(ctx, nodeID, zlm.FFmpegSourceRequest{
		SrcURL: request.SrcURL, DstURL: request.DstURL, TimeoutMS: request.TimeoutMS,
		FFmpegCmdKey: request.TemplateKey, EnableHLS: request.EnableHLS, EnableMP4: request.EnableMP4,
	})
	if err != nil {
		return FFmpegSourceView{}, normalizeT11FFmpegError(err, nodeID, "addFFmpegSource")
	}
	if created == nil || strings.TrimSpace(created.Key) == "" {
		return FFmpegSourceView{}, NewInternalError(nodeIDString(nodeID), "FFmpeg creation returned no resource key")
	}
	key := strings.TrimSpace(created.Key)
	if err := validateFFmpegResourceKey(key); err != nil {
		if rollbackErr := s.compensateCreate(ctx, nodeID, key); rollbackErr != nil {
			return FFmpegSourceView{}, NewInternalError(nodeIDString(nodeID), "FFmpeg creation returned an invalid resource key; rollback could not be confirmed and orphan state is uncertain", rollbackErr)
		}
		return FFmpegSourceView{}, NewInternalError(nodeIDString(nodeID), "FFmpeg creation returned an invalid resource key", err)
	}
	srcView := ffmpegURLView(request.SrcURL)
	dstView := ffmpegURLView(request.DstURL)
	identity := ffmpegLedgerIdentity(nodeID, key)
	_, err = s.ledger.Register(ctx, repo.ManagedResourceRegistration{
		Identity:    identity,
		Fingerprint: ffmpegIdentityFingerprint(request, nodeID),
		Summary:     fmt.Sprintf("template=%s src=%s dst=%s", request.TemplateKey, srcView.Summary, dstView.Summary),
		CreatedBy:   actorUserID,
		ObservedAt:  s.clock(),
	})
	if err != nil {
		if rollbackErr := s.compensateCreate(ctx, nodeID, key); rollbackErr != nil {
			return FFmpegSourceView{}, NewInternalError(nodeIDString(nodeID), "FFmpeg source was created but provenance registration failed; rollback could not be confirmed and orphan state is uncertain", errors.Join(err, rollbackErr))
		}
		return FFmpegSourceView{}, normalizeT11FFmpegError(err, nodeID, "registerFFmpegSource")
	}
	return FFmpegSourceView{
		NodeID: nodeID, Key: key, TemplateKey: request.TemplateKey,
		SrcURL: srcView, DstURL: dstView, CreatedBy: actorUserID, Managed: true,
	}, nil
}

// CreateSource is the descriptive alias used by controllers.
func (s *FFmpegService) CreateSource(ctx context.Context, nodeID int64, actorUserID uint64, request FFmpegSourceCreateRequest) (FFmpegSourceView, error) {
	return s.Create(ctx, nodeID, actorUserID, request)
}

// ListPage returns one bounded page. Callers that need truncation metadata
// should use this method rather than the compatibility List wrapper.
func (s *FFmpegService) ListPage(ctx context.Context, nodeID int64, request PageRequest) (FFmpegSourcePage, error) {
	if nodeID <= 0 {
		return FFmpegSourcePage{}, NewValidationError(map[string]string{"nodeId": "must be positive"})
	}
	if err := s.requireConfigured(nodeID, false); err != nil {
		return FFmpegSourcePage{}, err
	}
	if err := s.checkCapability(ctx, nodeID, "listFFmpegSource"); err != nil {
		return FFmpegSourcePage{}, err
	}
	items, err := s.client.ListFFmpegSources(ctx, nodeID)
	if err != nil {
		return FFmpegSourcePage{}, normalizeT11FFmpegError(err, nodeID, "listFFmpegSource")
	}
	views := make([]FFmpegSourceView, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" || validateFFmpegResourceKey(key) != nil {
			// Untrusted list data must never become an error or a raw DTO field.
			continue
		}
		view := FFmpegSourceView{
			NodeID: nodeID, Key: key, TemplateKey: safeFFmpegTemplateKey(item.FFmpegCmdKey),
			SrcURL: ffmpegURLView(item.SrcURL), DstURL: ffmpegURLView(item.DstURL),
		}
		if s.ledger != nil {
			row, findErr := s.ledger.Find(ctx, ffmpegLedgerIdentity(nodeID, key))
			if findErr == nil && row != nil && row.TombstonedAt == nil {
				view.Managed, view.CreatedBy = true, row.CreatedBy
			}
		}
		views = append(views, view)
	}
	return Paginate(views, request), nil
}

// List is a compatibility wrapper for existing callers. It is still bounded
// by the shared response cap and returns the first normalized page only; new
// callers should use ListPage to retain pagination metadata.
func (s *FFmpegService) List(ctx context.Context, nodeID int64) ([]FFmpegSourceView, error) {
	page, err := s.ListPage(ctx, nodeID, PageRequest{Page: 1, PageSize: MaxPageSize})
	if err != nil {
		return nil, err
	}
	return page.List, nil
}

func (s *FFmpegService) ListSources(ctx context.Context, nodeID int64) ([]FFmpegSourceView, error) {
	return s.List(ctx, nodeID)
}

func (s *FFmpegService) ListSourcesPage(ctx context.Context, nodeID int64, request PageRequest) (FFmpegSourcePage, error) {
	return s.ListPage(ctx, nodeID, request)
}

// PreflightDelete performs the first ownership read. The returned token must
// be retained by the confirmation adapter and supplied to DeleteWithPreflight.
func (s *FFmpegService) PreflightDelete(ctx context.Context, nodeID int64, key string) (OwnershipPreflight, error) {
	if err := validateFFmpegDelete(nodeID, key); err != nil {
		return OwnershipPreflight{}, err
	}
	if err := s.requireConfigured(nodeID, false); err != nil {
		return OwnershipPreflight{}, err
	}
	if err := s.checkCapability(ctx, nodeID, "listFFmpegSource"); err != nil {
		return OwnershipPreflight{}, err
	}
	if _, err := s.client.ListFFmpegSources(ctx, nodeID); err != nil {
		return OwnershipPreflight{}, normalizeT11FFmpegError(err, nodeID, "listFFmpegSource")
	}
	if s.ownership == nil {
		return OwnershipPreflight{}, NewInternalError(nodeIDString(nodeID), "FFmpeg ownership resolver is not configured")
	}
	return s.ownership.Preflight(ctx, ffmpegOwnershipTarget(nodeID, key))
}

// Delete executes a fresh preflight followed by the typed ZLM delete. It is
// intentionally not a force path and never accepts a bool to select force.
func (s *FFmpegService) Delete(ctx context.Context, nodeID int64, key string) (FFmpegDeleteResult, error) {
	preflight, err := s.PreflightDelete(ctx, nodeID, key)
	if err != nil {
		return FFmpegDeleteResult{}, err
	}
	alreadyReleased, err := s.ffmpegAlreadyReleased(ctx, nodeID, key)
	if err != nil {
		return FFmpegDeleteResult{}, err
	}
	if alreadyReleased {
		return FFmpegDeleteResult{NodeID: nodeID, Key: key, Released: true, AlreadyReleased: true, Idempotent: true}, nil
	}
	return s.DeleteWithPreflight(ctx, nodeID, key, preflight)
}

func (s *FFmpegService) ffmpegAlreadyReleased(ctx context.Context, nodeID int64, key string) (bool, error) {
	items, err := s.client.ListFFmpegSources(ctx, nodeID)
	if err != nil {
		return false, normalizeT11FFmpegError(err, nodeID, "listFFmpegSource")
	}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == key {
			return false, nil
		}
	}
	row, err := s.ledger.Find(ctx, ffmpegLedgerIdentity(nodeID, key))
	if errors.Is(err, repo.ErrManagedResourceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, normalizeT11FFmpegError(err, nodeID, "findFFmpegSource")
	}
	return row != nil && row.TombstonedAt != nil, nil
}

func (s *FFmpegService) DeleteWithPreflight(ctx context.Context, nodeID int64, key string, preflight OwnershipPreflight) (FFmpegDeleteResult, error) {
	if err := validateFFmpegDelete(nodeID, key); err != nil {
		return FFmpegDeleteResult{}, err
	}
	if err := s.requireConfigured(nodeID, false); err != nil {
		return FFmpegDeleteResult{}, err
	}
	if s.ownership == nil {
		return FFmpegDeleteResult{}, NewInternalError(nodeIDString(nodeID), "FFmpeg ownership resolver is not configured")
	}
	if preflight.Target != ffmpegOwnershipTarget(nodeID, key) {
		return FFmpegDeleteResult{}, NewValidationError(map[string]string{"preflight": "does not match the resource"})
	}
	var deleted *zlm.ProxyDeleteResult
	err := s.ownership.Execute(ctx, preflight, func(operationCtx context.Context, _ OwnershipTarget) error {
		var callErr error
		deleted, callErr = s.client.DeleteFFmpegSource(operationCtx, nodeID, key)
		if callErr != nil {
			return normalizeT11FFmpegError(callErr, nodeID, "delFFmpegSource")
		}
		if deleted == nil {
			return NewInternalError(nodeIDString(nodeID), "FFmpeg delete returned no result")
		}
		if _, tombstoneErr := s.ledger.Tombstone(operationCtx, ffmpegLedgerIdentity(nodeID, key), s.clock()); tombstoneErr != nil {
			return normalizeT11FFmpegError(tombstoneErr, nodeID, "tombstoneFFmpegSource")
		}
		return nil
	})
	if err != nil {
		return FFmpegDeleteResult{}, err
	}
	return FFmpegDeleteResult{NodeID: nodeID, Key: key, Released: true, AlreadyReleased: !deleted.Hit, Idempotent: !deleted.Hit}, nil
}

func (s *FFmpegService) compensateCreate(ctx context.Context, nodeID int64, key string) error {
	result, err := s.client.DeleteFFmpegSource(ctx, nodeID, key)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("FFmpeg rollback returned no result")
	}
	return nil
}

// NewFFmpegPresenceReader adapts the typed list client to T6's presence
// boundary. It never mutates the ZLM resource or the ledger.
func NewFFmpegPresenceReader(client FFmpegClient) MediaPresenceReader {
	return ffmpegPresenceReader{client: client}
}

type ffmpegPresenceReader struct{ client FFmpegClient }

func (r ffmpegPresenceReader) IsPresent(ctx context.Context, target OwnershipTarget) (bool, error) {
	if r.client == nil {
		return false, errors.New("FFmpeg presence client is not configured")
	}
	items, err := r.client.ListFFmpegSources(ctx, target.NodeID)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.Key) == target.Media.Stream {
			return true, nil
		}
	}
	return false, nil
}

func (s *FFmpegService) requireConfigured(nodeID int64, write bool) error {
	if s == nil || s.client == nil {
		return NewInternalError(nodeIDString(nodeID), "FFmpeg service is not configured")
	}
	if s.ledger == nil {
		return NewInternalError(nodeIDString(nodeID), "FFmpeg resource ledger is not configured")
	}
	if write && s.templates == nil {
		return NewInternalError(nodeIDString(nodeID), "FFmpeg template registry is not configured")
	}
	return nil
}

func (s *FFmpegService) checkCapability(ctx context.Context, nodeID int64, capability string) error {
	if s == nil || s.capability == nil {
		return nil
	}
	state, err := s.capability.FFmpegCapability(ctx, nodeID)
	if err != nil {
		if state == FFmpegCapabilityUnsupported {
			return NewUnsupportedCapabilityError(nodeIDString(nodeID), capability)
		}
		// Unknown capability is deliberately a controlled typed attempt.
		if state == FFmpegCapabilityUnknown || state == "" {
			return nil
		}
		return normalizeT11FFmpegError(err, nodeID, capability)
	}
	if state == FFmpegCapabilityUnsupported {
		return NewUnsupportedCapabilityError(nodeIDString(nodeID), capability)
	}
	return nil
}

func validateFFmpegCreateRequest(nodeID int64, actorUserID uint64, request FFmpegSourceCreateRequest) error {
	fields := make(map[string]string)
	if nodeID <= 0 {
		fields["nodeId"] = "must be positive"
	}
	if actorUserID == 0 {
		fields["actorUserId"] = "must be positive"
	}
	if err := validateFFmpegTemplateKey(request.TemplateKey); err != nil {
		fields["templateKey"] = "is invalid"
	}
	if err := validateFFmpegURL("srcUrl", request.SrcURL); err != nil {
		fields["srcUrl"] = "is invalid"
	}
	if err := validateFFmpegURL("dstUrl", request.DstURL); err != nil {
		fields["dstUrl"] = "is invalid"
	}
	if request.TimeoutMS <= 0 || request.TimeoutMS > maxFFmpegTimeoutMS {
		fields["timeoutMs"] = "must be within the supported range"
	}
	if len(fields) != 0 {
		return NewValidationError(fields)
	}
	return nil
}

func validateFFmpegDelete(nodeID int64, key string) error {
	fields := make(map[string]string)
	if nodeID <= 0 {
		fields["nodeId"] = "must be positive"
	}
	if validateFFmpegResourceKey(key) != nil {
		fields["key"] = "is invalid"
	}
	if len(fields) != 0 {
		return NewValidationError(fields)
	}
	return nil
}

var ffmpegTemplateKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var ffmpegResourceKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)

func validateFFmpegTemplateKey(key string) error {
	if key == "" || key != strings.TrimSpace(key) || !ffmpegTemplateKeyPattern.MatchString(key) {
		return errors.New("invalid FFmpeg template key")
	}
	return nil
}

func validateFFmpegResourceKey(key string) error {
	if key == "" || key != strings.TrimSpace(key) || !ffmpegResourceKeyPattern.MatchString(key) {
		return errors.New("invalid FFmpeg resource key")
	}
	return nil
}

func safeFFmpegTemplateKey(key string) string {
	if validateFFmpegTemplateKey(key) != nil {
		return ""
	}
	return key
}

func validateFFmpegURL(field, raw string) error {
	_ = field
	if raw == "" || raw != strings.TrimSpace(raw) || len(raw) > maxFFmpegURLLength {
		return errors.New("URL is invalid")
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("URL contains unsafe characters")
		}
		switch r {
		case ';', '|', '`', '$', '(', ')', '{', '}', '<', '>', '\\', '"', '\'':
			return errors.New("URL contains unsafe characters")
		}
	}
	decoded, err := url.QueryUnescape(raw)
	if err == nil {
		for _, r := range decoded {
			if unicode.IsControl(r) {
				return errors.New("URL contains unsafe characters")
			}
		}
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return errors.New("URL must include a supported scheme and host")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "rtmp", "rtmps", "rtsp", "rtsps", "srt", "udp", "tcp", "ws", "wss":
		return nil
	default:
		return errors.New("URL scheme is unsupported")
	}
}

func ffmpegURLView(raw string) FFmpegURLView {
	digest := sha256.Sum256([]byte(raw))
	view := FFmpegURLView{Fingerprint: hex.EncodeToString(digest[:]), Summary: "[redacted-url]"}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return view
	}
	// parsed.Host excludes userinfo. Never append Path, RawQuery or Fragment.
	view.Summary = strings.ToLower(parsed.Scheme) + "://" + parsed.Host
	return view
}

func ffmpegIdentityFingerprint(request FFmpegSourceCreateRequest, nodeID int64) string {
	return repo.FingerprintManagedResourceParts(
		nodeIDString(nodeID), ResourceTypeFFmpegSource, request.TemplateKey,
		"ffmpeg", "__defaultVhost__", ffmpegLedgerApp,
		request.SrcURL, request.DstURL, fmt.Sprint(request.TimeoutMS),
		fmt.Sprint(request.EnableHLS), fmt.Sprint(request.EnableMP4),
	)
}

func ffmpegLedgerIdentity(nodeID int64, key string) repo.ManagedResourceIdentity {
	return repo.ManagedResourceIdentity{
		NodeID: nodeID, ResourceType: ResourceTypeFFmpegSource, ResourceKey: key,
		Schema: "ffmpeg", Vhost: "__defaultVhost__",
		App: ffmpegLedgerApp, Stream: key,
	}
}

func ffmpegOwnershipTarget(nodeID int64, key string) OwnershipTarget {
	return OwnershipTarget{NodeID: nodeID, Media: MediaIdentity{Schema: "ffmpeg", Vhost: "__defaultVhost__", App: ffmpegLedgerApp, Stream: key}}
}

func (s *FFmpegService) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func normalizeT11FFmpegError(err error, nodeID int64, capability string) error {
	if err == nil {
		return nil
	}
	if managementErr, ok := AsManagementError(err); ok {
		return managementErr
	}
	if t11IngressUnsupported(err) {
		return NewUnsupportedCapabilityError(nodeIDString(nodeID), capability, err)
	}
	if errors.Is(err, zlm.ErrIngressInvalidRequest) {
		return NewValidationError(map[string]string{"request": "is invalid"})
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUpstreamTimeout) {
		return NewUpstreamTimeoutError(nodeIDString(nodeID), err)
	}
	return NewInternalError(nodeIDString(nodeID), "ZLM FFmpeg operation failed", err)
}

func t11IngressUnsupported(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, zlm.ErrCapabilityUnsupported) || errors.Is(err, ErrUnsupportedCapability) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"code=-404", "code=404", "api not found", "api does not exist", "unsupported api", "api not support"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
