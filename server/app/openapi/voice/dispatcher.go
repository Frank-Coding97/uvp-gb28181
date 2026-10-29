package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/openapi/adapters"
	"uvplatform.com/uvp-gb28181/app/openapi/catalog/runtime"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
)

const (
	ScopeTalkCreate      = "device:talk:create"
	ScopeTalkRead        = "device:talk:read"
	ScopeTalkEnd         = "device:talk:end"
	ScopeTalkMedia       = "device:talk:media"
	ScopeBroadcastCreate = "device:broadcast:create"
	ScopeBroadcastRead   = "device:broadcast:read"
	ScopeBroadcastEnd    = "device:broadcast:end"
	ScopeBroadcastMedia  = "device:broadcast:media"

	TalkCreateAdapterKey      = "device.voice-talk-create.v1"
	TalkReadAdapterKey        = "device.voice-talk-read.v1"
	TalkEndAdapterKey         = "device.voice-talk-end.v1"
	TalkMediaAdapterKey       = "device.voice-talk-media.v1"
	BroadcastCreateAdapterKey = "device.voice-broadcast-create.v1"
	BroadcastReadAdapterKey   = "device.voice-broadcast-read.v1"
	BroadcastEndAdapterKey    = "device.voice-broadcast-end.v1"
	BroadcastMediaAdapterKey  = "device.voice-broadcast-media.v1"
)

type Target struct {
	Device  gbmodels.GbDevice
	Channel gbmodels.GbChannel
}

type TargetResolver interface {
	Resolve(context.Context, resource.DepartmentScope, string, string) (Target, error)
}

type TalkService interface {
	Create(context.Context, talk.CreateRequest) (*talk.CreateResult, error)
	GetForOpenAPIClient(context.Context, int64, string) (*gbmodels.GbTalkSession, error)
	CleanupForOpenAPIClient(context.Context, int64, string, gbmodels.TalkSessionState, string) error
	PrepareUplinkForOpenAPIClient(context.Context, int64, string) (*talk.UplinkTarget, error)
}

type Request struct {
	ClientID       int64
	IdempotencyKey string
	ResourceScope  resource.DepartmentScope
	DeviceID       string
	ChannelID      string
	SessionID      string
}

type MediaResource struct {
	Path        string           `json:"path"`
	Protocol    string           `json:"protocol"`
	ContentType string           `json:"contentType"`
	ICEServers  []talk.ICEServer `json:"iceServers,omitempty"`
}

type SessionView struct {
	SessionID string                   `json:"sessionId"`
	Mode      gbmodels.TalkSessionMode `json:"mode"`
	State     string                   `json:"state"`
	ExpiresAt time.Time                `json:"expiresAt"`
	StartedAt *time.Time               `json:"startedAt,omitempty"`
	EndedAt   *time.Time               `json:"endedAt,omitempty"`
	Media     *MediaResource           `json:"media,omitempty"`
}

type MediaAnswer struct {
	SDP string `json:"sdp"`
}

type Dispatcher struct {
	targets TargetResolver
	talk    TalkService
}

func NewDispatcher(targets TargetResolver, service TalkService) *Dispatcher {
	return &Dispatcher{targets: targets, talk: service}
}

func NewGormTargetResolver(db *gorm.DB) TargetResolver {
	return gormTargetResolver{db: db}
}

type gormTargetResolver struct{ db *gorm.DB }

func (resolver gormTargetResolver) Resolve(ctx context.Context, scope resource.DepartmentScope, deviceID, channelID string) (Target, error) {
	if resolver.db == nil {
		return Target{}, adapters.ErrUnavailable
	}
	departmentIDs, err := resource.ResolveDepartmentIDs(ctx, resolver.db, scope)
	if err != nil {
		if errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return Target{}, adapters.ErrInvalidRequest
		}
		return Target{}, adapters.ErrUnavailable
	}
	var devices []gbmodels.GbDevice
	result := resolver.db.WithContext(ctx).Where("device_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL", deviceID, departmentIDs).
		Order("id").Limit(2).Find(&devices)
	if result.Error != nil {
		return Target{}, adapters.ErrUnavailable
	}
	if len(devices) != 1 {
		return Target{}, adapters.ErrNotFound
	}
	device := devices[0]
	var channels []gbmodels.GbChannel
	result = resolver.db.WithContext(ctx).Where("device_id = ? AND channel_id = ? AND owner_dept_id = ? AND owner_dept_id IN ? AND deleted_at IS NULL",
		device.DeviceID, channelID, device.OwnerDeptID, departmentIDs).Order("id").Limit(2).Find(&channels)
	if result.Error != nil {
		return Target{}, adapters.ErrUnavailable
	}
	if len(channels) != 1 {
		return Target{}, adapters.ErrNotFound
	}
	return Target{Device: device, Channel: channels[0]}, nil
}

func (dispatcher *Dispatcher) Ready() bool {
	return dispatcher != nil && dispatcher.targets != nil && dispatcher.talk != nil
}

func (dispatcher *Dispatcher) Handle(ctx context.Context, scope string, request Request) (any, error) {
	if !dispatcher.Ready() {
		return nil, adapters.ErrUnavailable
	}
	if ctx == nil || request.ClientID <= 0 || !validDepartmentScope(request.ResourceScope) {
		return nil, adapters.ErrInvalidRequest
	}
	mode, action, ok := scopeDefinition(scope)
	if !ok {
		return nil, adapters.ErrUnavailable
	}
	if action == "create" {
		return dispatcher.create(ctx, mode, request)
	}
	if !validSessionRequest(request) {
		return nil, adapters.ErrInvalidRequest
	}
	target, err := dispatcher.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	session, err := dispatcher.talk.GetForOpenAPIClient(ctx, request.ClientID, request.SessionID)
	if err != nil {
		return nil, mapTalkError(err)
	}
	if !ownsTarget(session, target, mode) {
		return nil, adapters.ErrNotFound
	}
	if action == "read" {
		return sessionView(session, target), nil
	}
	if !session.State.IsTerminal() {
		if err := dispatcher.talk.CleanupForOpenAPIClient(ctx, request.ClientID, request.SessionID, gbmodels.TalkSessionEnded, "OpenAPI client ended voice session"); err != nil {
			latest, getErr := dispatcher.talk.GetForOpenAPIClient(ctx, request.ClientID, request.SessionID)
			if getErr != nil || latest == nil || !latest.State.IsTerminal() {
				return nil, mapTalkError(err)
			}
			session = latest
		} else {
			session, err = dispatcher.talk.GetForOpenAPIClient(ctx, request.ClientID, request.SessionID)
			if err != nil {
				return nil, mapTalkError(err)
			}
			if session == nil {
				return nil, adapters.ErrNotFound
			}
		}
	}
	return sessionView(session, target), nil
}

// PrepareMedia returns a one-use upstream target only to the in-process media
// proxy. Callers must never serialize the returned URL or expose it to clients.
func (dispatcher *Dispatcher) PrepareMedia(ctx context.Context, scope string, request Request) (*talk.UplinkTarget, error) {
	if !dispatcher.Ready() {
		return nil, adapters.ErrUnavailable
	}
	if ctx == nil || request.ClientID <= 0 || !validDepartmentScope(request.ResourceScope) || !validSessionRequest(request) {
		return nil, adapters.ErrInvalidRequest
	}
	mode, ok := modeForMediaScope(scope)
	if !ok {
		return nil, adapters.ErrUnavailable
	}
	target, err := dispatcher.resolveTarget(ctx, request)
	if err != nil {
		return nil, err
	}
	session, err := dispatcher.talk.GetForOpenAPIClient(ctx, request.ClientID, request.SessionID)
	if err != nil {
		return nil, mapTalkError(err)
	}
	if !ownsTarget(session, target, mode) || session.State.IsTerminal() {
		return nil, adapters.ErrNotFound
	}
	media, err := dispatcher.talk.PrepareUplinkForOpenAPIClient(ctx, request.ClientID, request.SessionID)
	if err != nil {
		return nil, mapTalkError(err)
	}
	if media == nil || strings.TrimSpace(media.URL) == "" {
		return nil, adapters.ErrUnavailable
	}
	return media, nil
}

// ProxyMedia accepts an offer only after resolving the device and verifying
// that the authenticated OpenAPI client owns the matching live session. The
// one-use upstream URL and Location header remain inside the server process.
func (dispatcher *Dispatcher) ProxyMedia(ctx context.Context, scope string, request Request, offer []byte, client *http.Client) (MediaAnswer, error) {
	if client == nil || !validSDP(offer) {
		return MediaAnswer{}, adapters.ErrInvalidRequest
	}
	target, err := dispatcher.PrepareMedia(ctx, scope, request)
	if err != nil {
		return MediaAnswer{}, err
	}
	answer, err := talk.ProxyUplink(ctx, client, target, offer)
	if err != nil {
		return MediaAnswer{}, adapters.ErrUnavailable
	}
	switch {
	case answer.StatusCode >= http.StatusOK && answer.StatusCode < http.StatusMultipleChoices:
		if !validSDP(answer.Body) {
			return MediaAnswer{}, adapters.ErrUnavailable
		}
		return MediaAnswer{SDP: string(answer.Body)}, nil
	case answer.StatusCode >= http.StatusBadRequest && answer.StatusCode < http.StatusInternalServerError:
		return MediaAnswer{}, adapters.ErrInvalidRequest
	default:
		return MediaAnswer{}, adapters.ErrUnavailable
	}
}

func validSDP(value []byte) bool {
	if len(value) == 0 || len(value) > 60_000 || !utf8.Valid(value) {
		return false
	}
	trimmed := strings.TrimSpace(string(value))
	return strings.HasPrefix(trimmed, "v=0\r\n") || strings.HasPrefix(trimmed, "v=0\n") || trimmed == "v=0"
}

func (dispatcher *Dispatcher) create(ctx context.Context, mode gbmodels.TalkSessionMode, request Request) (SessionView, error) {
	if request.SessionID != "" || !validIdempotencyKey(request.IdempotencyKey) {
		return SessionView{}, adapters.ErrInvalidRequest
	}
	target, err := dispatcher.resolveTarget(ctx, request)
	if err != nil {
		return SessionView{}, err
	}
	fingerprint := requestFingerprint(request, target, mode)
	result, err := dispatcher.talk.Create(ctx, talk.CreateRequest{
		Channel: &target.Channel, Device: &target.Device, Mode: mode,
		OpenAPIClientID: request.ClientID, OpenAPIIdempotencyKey: request.IdempotencyKey, OpenAPIRequestHash: fingerprint,
	})
	if err != nil {
		return SessionView{}, mapTalkError(err)
	}
	if result == nil || strings.TrimSpace(result.SessionID) == "" {
		return SessionView{}, adapters.ErrUnavailable
	}
	return SessionView{SessionID: result.SessionID, Mode: result.Mode, State: result.State, ExpiresAt: result.ExpiresAt,
		Media: mediaResource(result.SessionID, result.Mode, request, result.Uplink)}, nil
}

func (dispatcher *Dispatcher) resolveTarget(ctx context.Context, request Request) (Target, error) {
	target, err := dispatcher.targets.Resolve(ctx, request.ResourceScope, request.DeviceID, request.ChannelID)
	if err != nil {
		if errors.Is(err, adapters.ErrNotFound) || errors.Is(err, resource.ErrResourceNotFound) {
			return Target{}, adapters.ErrNotFound
		}
		if errors.Is(err, adapters.ErrInvalidRequest) || errors.Is(err, resource.ErrInvalidDepartmentScope) {
			return Target{}, adapters.ErrInvalidRequest
		}
		return Target{}, adapters.ErrUnavailable
	}
	if target.Device.ID == 0 || target.Channel.ID == 0 || target.Device.OwnerDeptID == 0 || target.Channel.OwnerDeptID != target.Device.OwnerDeptID ||
		target.Device.DeviceID != request.DeviceID || target.Channel.DeviceID != target.Device.DeviceID || target.Channel.ChannelID != request.ChannelID {
		return Target{}, adapters.ErrNotFound
	}
	return target, nil
}

func validDepartmentScope(scope resource.DepartmentScope) bool {
	return scope.OwnerDeptID > 0 && (scope.DataScope == resource.DataScopeDepartment || scope.DataScope == resource.DataScopeDepartmentAndChildren)
}

func validIdempotencyKey(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for i := range value {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func validSessionRequest(request Request) bool {
	return strings.TrimSpace(request.SessionID) != "" && request.IdempotencyKey == ""
}

func scopeDefinition(scope string) (gbmodels.TalkSessionMode, string, bool) {
	switch scope {
	case ScopeTalkCreate:
		return gbmodels.TalkSessionModeTalk, "create", true
	case ScopeTalkRead:
		return gbmodels.TalkSessionModeTalk, "read", true
	case ScopeTalkEnd:
		return gbmodels.TalkSessionModeTalk, "end", true
	case ScopeBroadcastCreate:
		return gbmodels.TalkSessionModeBroadcast, "create", true
	case ScopeBroadcastRead:
		return gbmodels.TalkSessionModeBroadcast, "read", true
	case ScopeBroadcastEnd:
		return gbmodels.TalkSessionModeBroadcast, "end", true
	default:
		return "", "", false
	}
}

func modeForMediaScope(scope string) (gbmodels.TalkSessionMode, bool) {
	switch scope {
	case ScopeTalkCreate, ScopeTalkRead, ScopeTalkEnd, ScopeTalkMedia:
		return gbmodels.TalkSessionModeTalk, true
	case ScopeBroadcastCreate, ScopeBroadcastRead, ScopeBroadcastEnd, ScopeBroadcastMedia:
		return gbmodels.TalkSessionModeBroadcast, true
	default:
		return "", false
	}
}

func requestFingerprint(request Request, target Target, mode gbmodels.TalkSessionMode) string {
	value := fmt.Sprintf("%d\x00%d\x00%d\x00%s\x00%s\x00%d\x00%d\x00%s", request.ClientID,
		request.ResourceScope.OwnerDeptID, request.ResourceScope.DataScope, target.Device.DeviceID,
		target.Channel.ChannelID, target.Device.ID, target.Channel.ID, mode)
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func ownsTarget(session *gbmodels.GbTalkSession, target Target, mode gbmodels.TalkSessionMode) bool {
	return session != nil && session.OpenAPIClientID > 0 && session.Mode == mode && session.ChannelID == target.Channel.ID &&
		session.DeviceID == target.Device.DeviceID && session.TargetID == target.Channel.ChannelID
}

func sessionView(session *gbmodels.GbTalkSession, target Target) SessionView {
	view := SessionView{SessionID: session.SessionID, Mode: session.Mode, State: string(session.State), ExpiresAt: session.ExpiresAt,
		StartedAt: session.StartedAt, EndedAt: session.EndedAt}
	if !session.State.IsTerminal() {
		view.Media = mediaResource(session.SessionID, session.Mode, Request{DeviceID: target.Device.DeviceID, ChannelID: target.Channel.ChannelID}, talk.UplinkDescriptor{
			Protocol: "whip", ContentType: "application/sdp",
		})
	}
	return view
}

func mediaResource(sessionID string, mode gbmodels.TalkSessionMode, request Request, descriptor talk.UplinkDescriptor) *MediaResource {
	collection := "talk-sessions"
	if mode == gbmodels.TalkSessionModeBroadcast {
		collection = "broadcast-sessions"
	}
	path := "/openapi/v1/devices/" + url.PathEscape(request.DeviceID) + "/channels/" + url.PathEscape(request.ChannelID) + "/" + collection + "/" + url.PathEscape(sessionID) + "/media"
	return &MediaResource{Path: path, Protocol: descriptor.Protocol, ContentType: descriptor.ContentType, ICEServers: descriptor.ICEServers}
}

func mapTalkError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, talk.ErrTalkSessionNotFound):
		return adapters.ErrNotFound
	case errors.Is(err, talk.ErrInvalidOpenAPIIdentity), errors.Is(err, talk.ErrInvalidTalkSessionMode):
		return adapters.ErrInvalidRequest
	case errors.Is(err, talk.ErrOpenAPIIdempotencyConflict), errors.Is(err, talk.ErrLeaseConflict), errors.Is(err, talk.ErrResourceConflict):
		return adapters.ErrConflict
	case errors.Is(err, talk.ErrTalkTargetOffline):
		return adapters.ErrDeviceOffline
	case errors.Is(err, talk.ErrTalkNodeUnavailable), errors.Is(err, talk.ErrSecurePublishUnavailable), errors.Is(err, talk.ErrTalkActivationUnavailable):
		return adapters.ErrUnavailable
	default:
		return adapters.ErrUnavailable
	}
}

var _ runtime.Adapter = (*Adapter)(nil)
