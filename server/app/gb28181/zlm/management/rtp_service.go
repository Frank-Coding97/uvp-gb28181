package management

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/repo"
)

const ResourceTypeRTPServer = "rtp_server"

// RTPServerCreateRequest is the complete management-side open request. No
// vhost/app default is supplied: callers must make both identities explicit.
type RTPServerCreateRequest struct {
	VHost     string `json:"vhost"`
	App       string `json:"app"`
	Stream    string `json:"stream"`
	Port      int    `json:"port"`
	TCPMode   int    `json:"tcpMode"`
	SSRC      string `json:"ssrc,omitempty"`
	OnlyTrack int    `json:"onlyTrack"`
	LocalIP   string `json:"localIp,omitempty"`
	Reuse     bool   `json:"reuse"`
}

type RTPCreateRequest = RTPServerCreateRequest

// RTPServerCloseRequest deliberately has no Force field. ForceClose accepts a
// separate request and route so a normal close cannot be upgraded by JSON.
type RTPServerCloseRequest struct {
	NodeID int64  `json:"nodeId"`
	VHost  string `json:"vhost"`
	App    string `json:"app"`
	Stream string `json:"stream"`
}

// RTPServerForceCloseRequest is intentionally distinct from the ordinary
// close request and requires a human-readable reason.
type RTPServerForceCloseRequest struct {
	NodeID int64  `json:"nodeId"`
	VHost  string `json:"vhost"`
	App    string `json:"app"`
	Stream string `json:"stream"`
	Reason string `json:"reason"`
}

// RTPServerView is the safe DTO returned to management callers. All fields
// originate from typed data; no raw ZLM response, secret or command text is
// retained.
type RTPServerView struct {
	NodeID    int64  `json:"nodeId"`
	Key       string `json:"key"`
	VHost     string `json:"vhost"`
	App       string `json:"app"`
	Stream    string `json:"stream"`
	SSRC      string `json:"ssrc"`
	Port      int    `json:"port"`
	TCPMode   int    `json:"tcpMode"`
	OnlyTrack int    `json:"onlyTrack"`
	Released  bool   `json:"released"`
	Managed   bool   `json:"managed"`
	CreatedBy uint64 `json:"createdBy,omitempty"`
}

// RTPServerPage is the bounded list contract used by management adapters.
// The underlying ZLM endpoint has no native pagination, so the shared
// MaxResponseItems cap is applied before a page is returned.
type RTPServerPage = Page[RTPServerView]

type RTPServerDTO = RTPServerView

// RTPServerOpenResult and RTPServerCloseResult are adapter results. They are
// deliberately separate from zlm.Client's raw methods so T14 can supply the
// missing vhost/app/local_ip/reuse fields without changing that client here.
type RTPServerOpenResult struct {
	Key  string
	Port int
}

type RTPServerCloseResult struct {
	Stream   string
	Hit      bool
	Released bool
}

func rtpReleaseConfirmed(result *RTPServerCloseResult) bool {
	return result != nil && (result.Hit || result.Released)
}

// RTPServerCloseView preserves idempotence for an already released port.
type RTPServerCloseView struct {
	NodeID          int64  `json:"nodeId"`
	Stream          string `json:"stream"`
	Released        bool   `json:"released"`
	AlreadyReleased bool   `json:"alreadyReleased"`
	Idempotent      bool   `json:"idempotent"`
	Hit             bool   `json:"hit"`
}

// RTPClient is the T11 adapter seam. Implementations must bind every call to
// nodeID and must pass only the typed request; arbitrary endpoint/maps are not
// accepted by this service.
type RTPClient interface {
	ListRtpServers(context.Context, int64) ([]zlm.RtpServerInfo, error)
	OpenRtpServer(context.Context, int64, RTPServerCreateRequest) (*RTPServerOpenResult, error)
	CloseRtpServer(context.Context, int64, string, string, string) (*RTPServerCloseResult, error)
}

// RTPClientAdapter is the explicit seam T14 can implement with a node-bound
// typed client. The current T3 client does not carry vhost/app/local_ip/reuse
// through openRtpServer, so this adapter remains management-local.
type RTPClientAdapter = RTPClient

type RTPResourceLedger interface {
	Register(context.Context, repo.ManagedResourceRegistration) (*gbmodels.GbZLMManagedResource, error)
	Find(context.Context, repo.ManagedResourceIdentity) (*gbmodels.GbZLMManagedResource, error)
	Tombstone(context.Context, repo.ManagedResourceIdentity, time.Time) (*gbmodels.GbZLMManagedResource, error)
}

type RTPCapabilityState string

const (
	RTPCapabilitySupported   RTPCapabilityState = "supported"
	RTPCapabilityUnsupported RTPCapabilityState = "unsupported"
	RTPCapabilityUnknown     RTPCapabilityState = "unknown"
)

type RTPCapabilityReader interface {
	RTPCapability(context.Context, int64) (RTPCapabilityState, error)
}

type RTPDependencies struct {
	Client     RTPClient
	Ledger     RTPResourceLedger
	Ownership  *OwnershipResolver
	Capability RTPCapabilityReader
}

type RTPService struct {
	client     RTPClient
	ledger     RTPResourceLedger
	ownership  *OwnershipResolver
	capability RTPCapabilityReader
	now        func() time.Time
}

// RtpService is retained for callers that use the protocol acronym as a
// normal Go word.
type RtpService = RTPService

func NewRTPService(dependencies RTPDependencies) *RTPService {
	return &RTPService{
		client: dependencies.Client, ledger: dependencies.Ledger,
		ownership: dependencies.Ownership, capability: dependencies.Capability,
		now: time.Now,
	}
}

func NewRtpService(dependencies RTPDependencies) *RTPService {
	return NewRTPService(dependencies)
}

// Create opens an RTP receiver and registers provenance only after ZLM has
// returned a valid actual port. actorUserID is trusted server identity and is
// not accepted through RTPServerCreateRequest JSON.
func (s *RTPService) Create(ctx context.Context, nodeID int64, actorUserID uint64, request RTPServerCreateRequest) (RTPServerView, error) {
	if err := validateRTPCreateRequest(nodeID, actorUserID, request); err != nil {
		return RTPServerView{}, err
	}
	if err := s.requireConfigured(nodeID, true); err != nil {
		return RTPServerView{}, err
	}
	if err := s.checkCapability(ctx, nodeID, "openRtpServer"); err != nil {
		return RTPServerView{}, err
	}
	items, err := s.client.ListRtpServers(ctx, nodeID)
	if err != nil {
		return RTPServerView{}, normalizeT11RTPError(err, nodeID, "listRtpServer")
	}
	if err := duplicateRTPMapping(nodeID, request, items); err != nil {
		return RTPServerView{}, err
	}
	opened, err := s.client.OpenRtpServer(ctx, nodeID, request)
	if err != nil {
		return RTPServerView{}, normalizeT11RTPError(err, nodeID, "openRtpServer")
	}
	if opened == nil {
		if rollbackErr := s.compensateCreate(ctx, nodeID, request); rollbackErr != nil {
			return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned no result; rollback could not be confirmed and orphan state is uncertain", rollbackErr)
		}
		return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned no result")
	}
	actualPort := opened.Port
	if actualPort == 0 && request.Port > 0 {
		// Older ZLM builds may omit the port when an explicit port was used. It
		// is safe to retain that caller-selected port; port=0 never uses this
		// fallback and therefore cannot invent an allocation.
		actualPort = request.Port
	}
	if actualPort <= 0 || actualPort > 65535 {
		if rollbackErr := s.compensateCreate(ctx, nodeID, request); rollbackErr != nil {
			return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned an invalid port; rollback could not be confirmed and orphan state is uncertain", rollbackErr)
		}
		return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned an invalid port")
	}
	key := strings.TrimSpace(opened.Key)
	if key == "" {
		key = request.Stream
	}
	if validateRTPResourceKey(key) != nil {
		if rollbackErr := s.compensateCreate(ctx, nodeID, request); rollbackErr != nil {
			return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned an invalid resource key; rollback could not be confirmed and orphan state is uncertain", rollbackErr)
		}
		return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP creation returned an invalid resource key")
	}
	identity := rtpLedgerIdentity(nodeID, request.App, request.Stream)
	_, err = s.ledger.Register(ctx, repo.ManagedResourceRegistration{
		Identity:    identity,
		Fingerprint: rtpIdentityFingerprint(request, actualPort),
		Summary:     fmt.Sprintf("vhost=%s app=%s stream=%s port=%d tcp_mode=%d only_track=%d", request.VHost, request.App, request.Stream, actualPort, request.TCPMode, request.OnlyTrack),
		CreatedBy:   actorUserID, ObservedAt: s.clock(),
	})
	if err != nil {
		if rollbackErr := s.compensateCreate(ctx, nodeID, request); rollbackErr != nil {
			return RTPServerView{}, NewInternalError(nodeIDString(nodeID), "RTP server was created but provenance registration failed; rollback could not be confirmed and orphan state is uncertain", errors.Join(err, rollbackErr))
		}
		return RTPServerView{}, normalizeT11RTPError(err, nodeID, "registerRTPServer")
	}
	return RTPServerView{
		NodeID: nodeID, Key: key, VHost: request.VHost, App: request.App, Stream: request.Stream,
		SSRC: request.SSRC, Port: actualPort, TCPMode: request.TCPMode, OnlyTrack: request.OnlyTrack,
		Managed: true, CreatedBy: actorUserID,
	}, nil
}

func (s *RTPService) CreateServer(ctx context.Context, nodeID int64, actorUserID uint64, request RTPServerCreateRequest) (RTPServerView, error) {
	return s.Create(ctx, nodeID, actorUserID, request)
}

func (s *RTPService) compensateCreate(ctx context.Context, nodeID int64, request RTPServerCreateRequest) error {
	result, err := s.client.CloseRtpServer(ctx, nodeID, request.VHost, request.App, request.Stream)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("RTP rollback returned no result")
	}
	if !rtpReleaseConfirmed(result) {
		return errors.New("RTP rollback release could not be confirmed")
	}
	return nil
}

// ListPage returns one bounded page. Callers that need truncation metadata
// should use this method rather than the compatibility List wrapper.
func (s *RTPService) ListPage(ctx context.Context, nodeID int64, request PageRequest) (RTPServerPage, error) {
	if nodeID <= 0 {
		return RTPServerPage{}, NewValidationError(map[string]string{"nodeId": "must be positive"})
	}
	if err := s.requireConfigured(nodeID, false); err != nil {
		return RTPServerPage{}, err
	}
	if err := s.checkCapability(ctx, nodeID, "listRtpServer"); err != nil {
		return RTPServerPage{}, err
	}
	items, err := s.client.ListRtpServers(ctx, nodeID)
	if err != nil {
		return RTPServerPage{}, normalizeT11RTPError(err, nodeID, "listRtpServer")
	}
	views := make([]RTPServerView, 0, len(items))
	for _, item := range items {
		stream := strings.TrimSpace(item.StreamID)
		if stream == "" || validateRTPResourceKey(stream) != nil {
			continue
		}
		key := strings.TrimSpace(item.Key)
		if key == "" || validateRTPResourceKey(key) != nil {
			key = stream
		}
		view := RTPServerView{
			NodeID: nodeID, Key: key, VHost: safeRTPText(item.VHost), App: safeRTPText(item.App), Stream: stream,
			SSRC: safeRTPText(item.SSRC), Port: item.Port, TCPMode: item.TCPMode, OnlyTrack: item.OnlyTrack, Released: item.Released,
		}
		if s.ledger != nil && view.App != "" {
			row, findErr := s.ledger.Find(ctx, rtpLedgerIdentity(nodeID, view.App, stream))
			if findErr == nil && row != nil && row.TombstonedAt == nil {
				view.Managed, view.CreatedBy = true, row.CreatedBy
			}
		}
		views = append(views, view)
	}
	return Paginate(views, request), nil
}

// List is a compatibility wrapper for existing callers. It remains bounded
// by the shared response cap and returns only the first normalized page; new
// callers should use ListPage to retain pagination metadata.
func (s *RTPService) List(ctx context.Context, nodeID int64) ([]RTPServerView, error) {
	page, err := s.ListPage(ctx, nodeID, PageRequest{Page: 1, PageSize: MaxPageSize})
	if err != nil {
		return nil, err
	}
	return page.List, nil
}

func (s *RTPService) ListServers(ctx context.Context, nodeID int64) ([]RTPServerView, error) {
	return s.List(ctx, nodeID)
}

func (s *RTPService) ListServersPage(ctx context.Context, nodeID int64, request PageRequest) (RTPServerPage, error) {
	return s.ListPage(ctx, nodeID, request)
}

func (s *RTPService) PreflightClose(ctx context.Context, request RTPServerCloseRequest) (OwnershipPreflight, error) {
	if err := request.validate(); err != nil {
		return OwnershipPreflight{}, err
	}
	if err := s.requireConfigured(request.NodeID, true); err != nil {
		return OwnershipPreflight{}, err
	}
	if err := s.checkCapability(ctx, request.NodeID, "listRtpServer"); err != nil {
		return OwnershipPreflight{}, err
	}
	if _, err := s.client.ListRtpServers(ctx, request.NodeID); err != nil {
		return OwnershipPreflight{}, normalizeT11RTPError(err, request.NodeID, "listRtpServer")
	}
	if s.ownership == nil {
		return OwnershipPreflight{}, NewInternalError(nodeIDString(request.NodeID), "RTP ownership resolver is not configured")
	}
	return s.ownership.Preflight(ctx, rtpOwnershipTarget(request))
}

func (s *RTPService) Close(ctx context.Context, request RTPServerCloseRequest) (RTPServerCloseView, error) {
	if err := request.validate(); err != nil {
		return RTPServerCloseView{}, err
	}
	if err := s.requireConfigured(request.NodeID, true); err != nil {
		return RTPServerCloseView{}, err
	}
	if err := s.checkCapability(ctx, request.NodeID, "closeRtpServer"); err != nil {
		return RTPServerCloseView{}, err
	}
	items, err := s.client.ListRtpServers(ctx, request.NodeID)
	if err != nil {
		return RTPServerCloseView{}, normalizeT11RTPError(err, request.NodeID, "listRtpServer")
	}
	row, rowFound, err := s.findRTPLedger(ctx, request)
	if err != nil {
		return RTPServerCloseView{}, normalizeT11RTPError(err, request.NodeID, "findRTPServer")
	}
	if rowFound && row.TombstonedAt != nil && !rtpListContains(items, request) {
		return RTPServerCloseView{NodeID: request.NodeID, Stream: request.Stream, Released: true, AlreadyReleased: true, Idempotent: true}, nil
	}
	preflight, err := s.PreflightClose(ctx, request)
	if err != nil {
		return RTPServerCloseView{}, err
	}
	return s.closeWithPreflight(ctx, request, preflight, false, "")
}

func (s *RTPService) CloseWithPreflight(ctx context.Context, request RTPServerCloseRequest, preflight OwnershipPreflight) (RTPServerCloseView, error) {
	if err := request.validate(); err != nil {
		return RTPServerCloseView{}, err
	}
	if err := s.requireConfigured(request.NodeID, true); err != nil {
		return RTPServerCloseView{}, err
	}
	return s.closeWithPreflight(ctx, request, preflight, false, "")
}

// ForceClose is independent from Close and cannot be selected by a boolean in
// RTPServerCloseRequest. It still runs a T6 preflight/recheck, then applies
// ExecuteForce with a mandatory reason.
func (s *RTPService) ForceClose(ctx context.Context, request RTPServerForceCloseRequest) (RTPServerCloseView, error) {
	if err := request.validate(); err != nil {
		return RTPServerCloseView{}, err
	}
	if strings.TrimSpace(request.Reason) == "" {
		return RTPServerCloseView{}, NewValidationError(map[string]string{"reason": "is required"})
	}
	if err := s.requireConfigured(request.NodeID, true); err != nil {
		return RTPServerCloseView{}, err
	}
	if err := s.checkCapability(ctx, request.NodeID, "closeRtpServer"); err != nil {
		return RTPServerCloseView{}, err
	}
	items, err := s.client.ListRtpServers(ctx, request.NodeID)
	if err != nil {
		return RTPServerCloseView{}, normalizeT11RTPError(err, request.NodeID, "listRtpServer")
	}
	closeRequest := RTPServerCloseRequest{NodeID: request.NodeID, VHost: request.VHost, App: request.App, Stream: request.Stream}
	row, rowFound, err := s.findRTPLedger(ctx, closeRequest)
	if err != nil {
		return RTPServerCloseView{}, normalizeT11RTPError(err, request.NodeID, "findRTPServer")
	}
	if rowFound && row.TombstonedAt != nil && !rtpListContains(items, closeRequest) {
		return RTPServerCloseView{NodeID: request.NodeID, Stream: request.Stream, Released: true, AlreadyReleased: true, Idempotent: true}, nil
	}
	preflight, err := s.PreflightClose(ctx, closeRequest)
	if err != nil {
		return RTPServerCloseView{}, err
	}
	return s.closeWithPreflight(ctx, closeRequest, preflight, true, request.Reason)
}

func (s *RTPService) ForceCloseServer(ctx context.Context, request RTPServerForceCloseRequest) (RTPServerCloseView, error) {
	return s.ForceClose(ctx, request)
}

func (s *RTPService) closeWithPreflight(ctx context.Context, request RTPServerCloseRequest, preflight OwnershipPreflight, force bool, reason string) (RTPServerCloseView, error) {
	if s.ownership == nil {
		return RTPServerCloseView{}, NewInternalError(nodeIDString(request.NodeID), "RTP ownership resolver is not configured")
	}
	target := rtpOwnershipTarget(request)
	if preflight.Target != target {
		return RTPServerCloseView{}, NewValidationError(map[string]string{"preflight": "does not match the resource"})
	}
	var closed *RTPServerCloseResult
	action := func(operationCtx context.Context, _ OwnershipTarget) error {
		var err error
		closed, err = s.client.CloseRtpServer(operationCtx, request.NodeID, request.VHost, request.App, request.Stream)
		if err != nil {
			return normalizeT11RTPError(err, request.NodeID, "closeRtpServer")
		}
		if closed == nil {
			return NewInternalError(nodeIDString(request.NodeID), "RTP close returned no result")
		}
		if !rtpReleaseConfirmed(closed) {
			return NewInternalError(nodeIDString(request.NodeID), "RTP release could not be confirmed; ledger tombstone was not written")
		}
		identity := rtpLedgerIdentity(request.NodeID, request.App, request.Stream)
		_, tombstoneErr := s.ledger.Tombstone(operationCtx, identity, s.clock())
		if tombstoneErr != nil && !errors.Is(tombstoneErr, repo.ErrManagedResourceNotFound) {
			return normalizeT11RTPError(tombstoneErr, request.NodeID, "tombstoneRTPServer")
		}
		return nil
	}
	var err error
	if force {
		err = s.ownership.ExecuteForce(ctx, preflight, reason, action)
	} else {
		err = s.ownership.Execute(ctx, preflight, action)
	}
	if err != nil {
		return RTPServerCloseView{}, err
	}
	return RTPServerCloseView{
		NodeID: request.NodeID, Stream: request.Stream, Released: true,
		AlreadyReleased: !closed.Hit && !closed.Released, Idempotent: !closed.Hit && !closed.Released, Hit: closed.Hit,
	}, nil
}

// NewRTPPresenceReader adapts typed RTP list results to T6's read-only
// presence boundary. It treats only an exact vhost/app/stream match as live.
func NewRTPPresenceReader(client RTPClient) MediaPresenceReader {
	return rtpPresenceReader{client: client}
}

type rtpPresenceReader struct{ client RTPClient }

func (r rtpPresenceReader) IsPresent(ctx context.Context, target OwnershipTarget) (bool, error) {
	if r.client == nil {
		return false, errors.New("RTP presence client is not configured")
	}
	items, err := r.client.ListRtpServers(ctx, target.NodeID)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if !item.Released && item.VHost == target.Media.Vhost && item.App == target.Media.App && item.StreamID == target.Media.Stream {
			return true, nil
		}
	}
	return false, nil
}

func (s *RTPService) requireConfigured(nodeID int64, write bool) error {
	if s == nil || s.client == nil {
		return NewInternalError(nodeIDString(nodeID), "RTP service is not configured")
	}
	if write && s.ledger == nil {
		return NewInternalError(nodeIDString(nodeID), "RTP resource ledger is not configured")
	}
	return nil
}

func (s *RTPService) checkCapability(ctx context.Context, nodeID int64, capability string) error {
	if s == nil || s.capability == nil {
		return nil
	}
	state, err := s.capability.RTPCapability(ctx, nodeID)
	if state == RTPCapabilityUnsupported {
		return NewUnsupportedCapabilityError(nodeIDString(nodeID), capability)
	}
	if err != nil && state != RTPCapabilityUnknown && state != "" {
		return normalizeT11RTPError(err, nodeID, capability)
	}
	return nil
}

func (request RTPServerCloseRequest) validate() error {
	return validateRTPMediaTarget(request.NodeID, request.VHost, request.App, request.Stream)
}

func (request RTPServerForceCloseRequest) validate() error {
	return validateRTPMediaTarget(request.NodeID, request.VHost, request.App, request.Stream)
}

func validateRTPCreateRequest(nodeID int64, actorUserID uint64, request RTPServerCreateRequest) error {
	fields := make(map[string]string)
	if nodeID <= 0 {
		fields["nodeId"] = "must be positive"
	}
	if actorUserID == 0 {
		fields["actorUserId"] = "must be positive"
	}
	if err := validateRTPField("vhost", request.VHost); err != nil {
		fields["vhost"] = "is invalid"
	}
	if err := validateRTPField("app", request.App); err != nil {
		fields["app"] = "is invalid"
	}
	if err := validateRTPField("stream", request.Stream); err != nil {
		fields["stream"] = "is invalid"
	}
	if request.Port < 0 || request.Port > 65535 {
		fields["port"] = "must be 0..65535"
	}
	if request.TCPMode < 0 || request.TCPMode > 2 {
		fields["tcpMode"] = "must be 0, 1 or 2"
	}
	if request.OnlyTrack < 0 || request.OnlyTrack > 2 {
		fields["onlyTrack"] = "must be 0, 1 or 2"
	}
	if request.SSRC != "" && !rtpSSRCPattern.MatchString(request.SSRC) {
		fields["ssrc"] = "must contain decimal digits only"
	}
	if request.LocalIP != "" {
		if net.ParseIP(request.LocalIP) == nil {
			fields["localIp"] = "must be a valid IP address"
		}
	}
	if request.Reuse && request.Port == 0 {
		fields["reuse"] = "requires an explicit port"
	}
	if len(fields) != 0 {
		return NewValidationError(fields)
	}
	return nil
}

func validateRTPMediaTarget(nodeID int64, vhost, app, stream string) error {
	fields := make(map[string]string)
	if nodeID <= 0 {
		fields["nodeId"] = "must be positive"
	}
	for name, value := range map[string]string{"vhost": vhost, "app": app, "stream": stream} {
		if validateRTPField(name, value) != nil {
			fields[name] = "is invalid"
		}
	}
	if len(fields) != 0 {
		return NewValidationError(fields)
	}
	return nil
}

func validateRTPField(name, value string) error {
	_ = name
	if value == "" || value != strings.TrimSpace(value) || len([]rune(value)) > MaxMediaIdentityFieldLength {
		return errors.New("identity field is invalid")
	}
	for _, r := range value {
		if r == 0 || r == '\n' || r == '\r' || r == '\t' {
			return errors.New("identity field contains control characters")
		}
	}
	return nil
}

var rtpSSRCPattern = regexp.MustCompile(`^[0-9]{1,32}$`)
var rtpResourceKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)

func validateRTPResourceKey(value string) error {
	if value == "" || value != strings.TrimSpace(value) || !rtpResourceKeyPattern.MatchString(value) {
		return errors.New("resource key is invalid")
	}
	return nil
}

func duplicateRTPMapping(nodeID int64, request RTPServerCreateRequest, items []zlm.RtpServerInfo) error {
	for _, item := range items {
		if item.Released {
			continue
		}
		if item.StreamID == request.Stream {
			return NewOwnershipConflictError(nodeIDString(nodeID), "RTP stream mapping already exists").WithImpacts([]Impact{{
				ResourceType: ResourceTypeRTPServer, ResourceKey: safeRTPText(item.Key),
				MediaIdentity: &MediaIdentity{Schema: "rtp", Vhost: safeRTPText(item.VHost), App: safeRTPText(item.App), Stream: safeRTPText(item.StreamID)},
			}})
		}
		if request.Port > 0 && item.Port == request.Port {
			return NewOwnershipConflictError(nodeIDString(nodeID), "RTP port mapping already exists").WithImpacts([]Impact{{
				ResourceType: ResourceTypeRTPServer, ResourceKey: safeRTPText(item.Key),
				MediaIdentity: &MediaIdentity{Schema: "rtp", Vhost: safeRTPText(item.VHost), App: safeRTPText(item.App), Stream: safeRTPText(item.StreamID)},
			}})
		}
	}
	return nil
}

func safeRTPText(value string) string {
	value = strings.TrimSpace(value)
	for _, r := range value {
		if r == 0 || r == '\n' || r == '\r' || r == '\t' {
			return ""
		}
	}
	return value
}

func rtpListContains(items []zlm.RtpServerInfo, request RTPServerCloseRequest) bool {
	for _, item := range items {
		if !item.Released && item.VHost == request.VHost && item.App == request.App && item.StreamID == request.Stream {
			return true
		}
	}
	return false
}

func (s *RTPService) findRTPLedger(ctx context.Context, request RTPServerCloseRequest) (*gbmodels.GbZLMManagedResource, bool, error) {
	row, err := s.ledger.Find(ctx, rtpLedgerIdentity(request.NodeID, request.App, request.Stream))
	if errors.Is(err, repo.ErrManagedResourceNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row, row != nil, nil
}

func rtpLedgerIdentity(nodeID int64, app, stream string) repo.ManagedResourceIdentity {
	return repo.ManagedResourceIdentity{NodeID: nodeID, ResourceType: ResourceTypeRTPServer, ResourceKey: stream, App: app, Stream: stream}
}

func rtpOwnershipTarget(request RTPServerCloseRequest) OwnershipTarget {
	return OwnershipTarget{NodeID: request.NodeID, Media: MediaIdentity{Schema: "rtp", Vhost: request.VHost, App: request.App, Stream: request.Stream}}
}

func rtpIdentityFingerprint(request RTPServerCreateRequest, actualPort int) string {
	return repo.FingerprintManagedResourceParts(
		request.VHost, request.App, request.Stream, request.SSRC,
		strconv.Itoa(actualPort), strconv.Itoa(request.TCPMode), strconv.Itoa(request.OnlyTrack),
		request.LocalIP, strconv.FormatBool(request.Reuse),
	)
}

func (s *RTPService) clock() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func normalizeT11RTPError(err error, nodeID int64, capability string) error {
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
	return NewInternalError(nodeIDString(nodeID), "ZLM RTP operation failed", err)
}
