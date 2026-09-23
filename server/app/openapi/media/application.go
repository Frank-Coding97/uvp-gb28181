package media

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const (
	applicationMediaApp = "rtp"
	applicationSchema   = "rtmp"
	applicationVHost    = "__defaultVhost__"
	grantCleanupTimeout = time.Second
)

var ErrLiveApplicationUnavailable = errors.New("openapi live application unavailable")

// QualificationRequest is the target that a trusted qualification provider
// must bind to a ticket. A provider is the future T18 trust boundary: this
// package deliberately has no fallback that turns an active node into a
// qualified node.
type QualificationRequest struct {
	DeviceID  string
	ChannelID string
	Protocol  string
}

// QualificationTicket is an immutable, value-owned proof for one live
// application. MediaOrigin is a secure scheme plus host origin only; it is
// not a general URL prefix. ExpiresAt bounds this application window, not the
// lifetime of a successfully issued grant. The provider owns how the
// qualification ID and boot/revision proof are established.
type QualificationTicket struct {
	QualificationID string
	NodeID          int64
	NodeUUID        string
	NodeRevision    uint64
	BootNonce       string
	Protocol        string
	MediaOrigin     string
	ExpiresAt       time.Time
}

// QualificationProvider selects a node and performs a fresh qualification
// probe in Prepare, then revalidates the same ticket at each Apply boundary.
// Implementations are trusted local adapters supplied by a future T18
// provider; no active-node or ordinary scheduler implementation is provided.
type QualificationProvider interface {
	Prepare(context.Context, QualificationRequest) (QualificationTicket, error)
	Validate(context.Context, QualificationRequest, QualificationTicket) error
}

// LivePlayer is intentionally narrower than play.Service. In particular, the
// application cannot stop or otherwise clean a shared live generation.
type LivePlayer interface {
	EnsureLive(context.Context, play.Request) (*play.Result, error)
}

// GrantIssuer consumes an already-reserved admission grant. The application
// does not reserve nonce, quota, or any other durable resource.
type GrantIssuer interface {
	Issue(context.Context, playauth.OpenAPIGrantIssueRequest) (playauth.Grant, error)
	FailUnboundGrant(context.Context, int64, string) error
}

type ApplyRequest struct {
	ClientID    int64
	GrantID     string
	DeviceID    string
	ChannelID   string
	DeviceEpoch int64
	Ticket      QualificationTicket
}

// ApplicationData is the complete external media-application response. It
// intentionally contains no Result, node projection, internal host field, or
// alternate protocol URL.
type ApplicationData struct {
	AuthorizationID string    `json:"authorizationId"`
	Protocol        string    `json:"protocol"`
	URL             string    `json:"url"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type LiveApplication struct {
	provider   QualificationProvider
	player     LivePlayer
	grants     GrantIssuer
	authEnable bool
	now        func() time.Time
}

type LiveApplicationOption func(*LiveApplication)

func WithLiveApplicationClock(now func() time.Time) LiveApplicationOption {
	return func(application *LiveApplication) {
		if now != nil {
			application.now = now
		}
	}
}

func NewLiveApplication(provider QualificationProvider, player LivePlayer, grants GrantIssuer, authEnabled bool, options ...LiveApplicationOption) *LiveApplication {
	application := &LiveApplication{
		provider:   provider,
		player:     player,
		grants:     grants,
		authEnable: authEnabled,
		now:        time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(application)
		}
	}
	return application
}

// Ready reports only whether the application has its required local
// dependencies and feature flag. It is a startup/configuration check; it
// does not qualify a node or assert T18 media-runtime eligibility.
func (application *LiveApplication) Ready() bool {
	if application == nil || !application.authEnable || interfaceIsNil(application.provider) || interfaceIsNil(application.player) || interfaceIsNil(application.grants) {
		return false
	}
	if readiness, ok := application.player.(interface{ Ready() bool }); ok {
		return readiness.Ready()
	}
	return true
}

// Preflight obtains a fresh immutable qualification ticket. It never picks
// from a node registry, opens media, or creates a grant.
func (application *LiveApplication) Preflight(ctx context.Context, deviceID, channelID, protocol string) (QualificationTicket, error) {
	ctx = applicationContext(ctx)
	if ctx.Err() != nil {
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	if application == nil || !application.authEnable || interfaceIsNil(application.provider) {
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	if !validGBIDForApplication(deviceID) || !validGBIDForApplication(channelID) || !validApplicationProtocol(protocol) {
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	request := QualificationRequest{DeviceID: deviceID, ChannelID: channelID, Protocol: protocol}
	ticket, err := application.provider.Prepare(ctx, request)
	if err != nil || !validQualificationTicket(ticket, protocol, application.nowUTC()) {
		return QualificationTicket{}, ErrLiveApplicationUnavailable
	}
	return ticket, nil
}

// Apply binds a previously reserved internal grant to one already-qualified
// live generation and returns one protocol-specific token URL. It never
// performs admission, quota, nonce, stop, kick, or shared-result mutation.
func (application *LiveApplication) Apply(ctx context.Context, request ApplyRequest) (ApplicationData, error) {
	ctx = applicationContext(ctx)
	if !validApplyIdentifiers(request) {
		return ApplicationData{}, ErrLiveApplicationUnavailable
	}
	if request.DeviceEpoch <= 0 {
		return application.failApply(ctx, request)
	}
	if ctx.Err() != nil {
		return application.failApply(ctx, request)
	}
	if !validQualificationTicket(request.Ticket, request.Ticket.Protocol, application.nowUTC()) {
		return application.failApply(ctx, request)
	}
	if application == nil || !application.authEnable || interfaceIsNil(application.provider) || interfaceIsNil(application.player) || interfaceIsNil(application.grants) {
		return ApplicationData{}, ErrLiveApplicationUnavailable
	}
	target := QualificationRequest{DeviceID: request.DeviceID, ChannelID: request.ChannelID, Protocol: request.Ticket.Protocol}
	if err := application.provider.Validate(ctx, target, request.Ticket); err != nil {
		return application.failApply(ctx, request)
	}
	result, err := application.player.EnsureLive(ctx, play.Request{
		DeviceID:         request.DeviceID,
		ChannelID:        request.ChannelID,
		DeviceEpoch:      request.DeviceEpoch,
		Trigger:          "openapi-live-apply",
		RequiredNode:     request.Ticket.NodeID,
		RequiredProtocol: request.Ticket.Protocol,
		QualificationID:  request.Ticket.QualificationID,
	})
	if err != nil || result == nil || !validLiveResult(result, request.Ticket) {
		return application.failApply(ctx, request)
	}
	if err := application.provider.Validate(ctx, target, request.Ticket); err != nil || !validQualificationTicket(request.Ticket, target.Protocol, application.nowUTC()) {
		return application.failApply(ctx, request)
	}
	rawURL, ok := selectedFLVURL(result, request.Ticket.Protocol)
	if !ok {
		return application.failApply(ctx, request)
	}
	parsedURL, ok := validateFLVURL(rawURL, result, request.Ticket)
	if !ok {
		return application.failApply(ctx, request)
	}
	grant, err := application.grants.Issue(ctx, playauth.OpenAPIGrantIssueRequest{
		GrantID:         request.GrantID,
		DeviceID:        request.DeviceID,
		ChannelID:       request.ChannelID,
		NodeUUID:        request.Ticket.NodeUUID,
		BootNonce:       request.Ticket.BootNonce,
		Schema:          applicationSchema,
		VHost:           applicationVHost,
		App:             result.App,
		Stream:          result.StreamID,
		MediaGeneration: result.Generation,
		Protocol:        request.Ticket.Protocol,
	})
	if err != nil || ctx.Err() != nil || !validIssuedGrant(grant, request.GrantID, application.nowUTC()) {
		return application.failApply(ctx, request)
	}
	// Issue can take long enough for the request's qualification to expire or
	// be withdrawn. Never release its URL until this final check passes. The
	// ticket does not shorten the independently enforced fixed v3 grant TTL.
	if err := application.provider.Validate(ctx, target, request.Ticket); err != nil || ctx.Err() != nil || !validQualificationTicket(request.Ticket, target.Protocol, application.nowUTC()) {
		return application.failApply(ctx, request)
	}
	parsedURL.RawQuery = url.Values{playauth.QueryParameter: []string{grant.Token}}.Encode()
	return ApplicationData{
		AuthorizationID: grant.AuthorizationGeneration,
		Protocol:        request.Ticket.Protocol,
		URL:             parsedURL.String(),
		ExpiresAt:       grant.ExpiresAt.UTC(),
	}, nil
}

func (application *LiveApplication) failApply(parent context.Context, request ApplyRequest) (ApplicationData, error) {
	if application == nil || interfaceIsNil(application.grants) {
		return ApplicationData{}, ErrLiveApplicationUnavailable
	}
	cleanupBase := applicationContext(parent)
	cleanupBase = context.WithoutCancel(cleanupBase)
	cleanupCtx, cancel := context.WithTimeout(cleanupBase, grantCleanupTimeout)
	defer cancel()
	// The request has already passed the exact identity and ticket-shape
	// checks, so this call can only target the grant supplied by admission.
	_ = application.grants.FailUnboundGrant(cleanupCtx, request.ClientID, request.GrantID)
	return ApplicationData{}, ErrLiveApplicationUnavailable
}

func (application *LiveApplication) nowUTC() time.Time {
	if application == nil || application.now == nil {
		return time.Now().UTC()
	}
	return application.now().UTC()
}

func applicationContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflection := reflect.ValueOf(value)
	switch reflection.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflection.IsNil()
	default:
		return false
	}
}

func validApplyIdentifiers(request ApplyRequest) bool {
	return request.ClientID > 0 && validApplicationUUID(request.GrantID) && validGBIDForApplication(request.DeviceID) && validGBIDForApplication(request.ChannelID)
}

func validApplicationUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func validGBIDForApplication(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validApplicationProtocol(protocol string) bool {
	return protocol == play.QualifiedProtocolHTTPSFLV || protocol == play.QualifiedProtocolWSSFLV
}

func validQualificationTicket(ticket QualificationTicket, protocol string, now time.Time) bool {
	if !validApplicationProtocol(protocol) || ticket.Protocol != protocol || !validText(ticket.QualificationID, 128) || ticket.NodeID <= 0 || ticket.NodeRevision == 0 || !validText(ticket.NodeUUID, 64) || !validBootNonceForApplication(ticket.BootNonce) || ticket.ExpiresAt.IsZero() || !now.Before(ticket.ExpiresAt) {
		return false
	}
	_, ok := trustedOrigin(ticket.MediaOrigin, protocol)
	return ok
}

func validText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validBootNonceForApplication(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func trustedOrigin(raw, protocol string) (*url.URL, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || raw != strings.TrimSpace(raw) || parsed == nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return nil, false
	}
	expectedScheme := "https"
	if protocol == play.QualifiedProtocolWSSFLV {
		expectedScheme = "wss"
	}
	if parsed.Scheme != expectedScheme {
		return nil, false
	}
	return parsed, true
}

func validLiveResult(result *play.Result, ticket QualificationTicket) bool {
	return result != nil && result.Node != nil && result.Node.ID == ticket.NodeID && result.Node.MediaServerUUID == ticket.NodeUUID && result.Node.Revision == ticket.NodeRevision && result.App == applicationMediaApp && result.Generation != 0 && result.StreamID != ""
}

func selectedFLVURL(result *play.Result, protocol string) (string, bool) {
	if result == nil {
		return "", false
	}
	var selected *string
	switch protocol {
	case play.QualifiedProtocolHTTPSFLV:
		selected = result.URLs.HTTPSFLV
	case play.QualifiedProtocolWSSFLV:
		selected = result.URLs.WSSFLV
	default:
		return "", false
	}
	return selectedValue(selected)
}

func selectedValue(value *string) (string, bool) {
	if value == nil || *value == "" {
		return "", false
	}
	return *value, true
}

func validateFLVURL(raw string, result *play.Result, ticket QualificationTicket) (*url.URL, bool) {
	origin, ok := trustedOrigin(ticket.MediaOrigin, ticket.Protocol)
	if !ok || result == nil {
		return nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.User != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Fragment != "" || parsed.RawFragment != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Scheme != origin.Scheme || parsed.Host != origin.Host {
		return nil, false
	}
	expectedPath := "/" + url.PathEscape(result.App) + "/" + url.PathEscape(result.StreamID) + ".live.flv"
	if parsed.EscapedPath() != expectedPath {
		return nil, false
	}
	return parsed, true
}

func validIssuedGrant(grant playauth.Grant, grantID string, now time.Time) bool {
	return grant.Token != "" && grant.Token == strings.TrimSpace(grant.Token) && grant.AuthorizationGeneration == grantID && !grant.ExpiresAt.IsZero() && now.Before(grant.ExpiresAt)
}
