package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/securestore"
)

const upstreamPasswordPurpose = "upstream-password"

var (
	ErrInvalidPlatformConfig     = errors.New("invalid cascade platform config")
	ErrCredentialUnavailable     = errors.New("cascade credential sealer unavailable")
	ErrRuntimeUnavailable        = errors.New("cascade runtime unavailable")
	// ErrRuntimeSyncFailed 配置已持久化但运行时同步失败:调用方应返回已提交
	// 资源而非整体失败,客户端重试会产生唯一键冲突
	ErrRuntimeSyncFailed = errors.New("cascade config persisted but runtime sync failed")
	ErrPlatformDisabled          = errors.New("cascade platform disabled")
	ErrPlatformHasActiveSessions = errors.New("cascade platform has active media sessions")
)

type ManagementStore interface {
	CreatePlatform(context.Context, *model.GbCascadePlatform) error
	FindPlatform(context.Context, uint64) (*model.GbCascadePlatform, error)
	ListPlatforms(context.Context) ([]model.GbCascadePlatform, error)
	UpdatePlatformConfig(context.Context, *model.GbCascadePlatform, uint64) (*model.GbCascadePlatform, error)
	SoftDeletePlatform(context.Context, uint64) error
	ReplaceProjection(context.Context, uint64, uint64, []repository.DeviceProjectionInput, []repository.ChannelProjectionInput) error
	ProjectionSnapshot(context.Context, uint64) (*repository.ProjectionSnapshot, error)
	ListNonterminalMediaSessions(context.Context, uint64) ([]model.GbCascadeMediaSession, error)
}

type CredentialSealer interface {
	Encrypt(string, []byte) (securestore.Envelope, error)
}

type ManagementRuntime interface {
	Reload(context.Context) error
	Reconnect(uint64)
	PlatformIDs() []uint64
}

type PlatformConfigInput struct {
	Name              string
	UpstreamServerID  string
	UpstreamDomain    string
	Host              string
	Port              int
	LocalDeviceID     string
	LocalDomain       string
	LocalSIPIP        string
	LocalSIPPort      int
	MediaAdvertiseIP  string
	AuthUsername      string
	Password          *string
	ProfileOverride   model.CascadeProfileOverride
	CharsetOverride   string
	RegisterExpires   int
	KeepaliveInterval int
	RetryPolicy       string
	Transport         string
	CatalogBatchSize  int
	PublishPlatform   bool
	PublishCivil      bool
	PublishGroup      bool
	MaxStreams        int
	PTZEnabled        bool
	Enabled           bool
}

type PlatformView struct {
	ID                   uint64                       `json:"id"`
	Name                 string                       `json:"name"`
	UpstreamServerID     string                       `json:"upstreamServerId"`
	UpstreamDomain       string                       `json:"upstreamDomain"`
	Host                 string                       `json:"host"`
	Port                 int                          `json:"port"`
	LocalDeviceID        string                       `json:"localDeviceId"`
	LocalDomain          string                       `json:"localDomain"`
	LocalSIPIP           string                       `json:"localSipIp"`
	LocalSIPPort         int                          `json:"localSipPort"`
	MediaAdvertiseIP     string                       `json:"mediaAdvertiseIp,omitempty"`
	AuthUsername         string                       `json:"authUsername,omitempty"`
	HasPassword          bool                         `json:"hasPassword"`
	ProfileOverride      model.CascadeProfileOverride `json:"profileOverride"`
	EffectiveVersion     string                       `json:"effectiveVersion"`
	EffectiveVersionFrom string                       `json:"effectiveVersionFrom"`
	CharsetOverride      string                       `json:"charsetOverride,omitempty"`
	RegisterExpires      int                          `json:"registerExpires"`
	KeepaliveInterval    int                          `json:"keepaliveInterval"`
	Transport            string                       `json:"transport"`
	CatalogBatchSize     int                          `json:"catalogBatchSize"`
	PublishPlatform      bool                         `json:"publishPlatform"`
	PublishCivil         bool                         `json:"publishCivil"`
	PublishGroup         bool                         `json:"publishGroup"`
	MaxStreams           int                          `json:"maxStreams"`
	PTZEnabled           bool                         `json:"ptzEnabled"`
	Enabled              bool                         `json:"enabled"`
	ConfigRevision       uint64                       `json:"configRevision"`
	ProjectionRevision   uint64                       `json:"projectionRevision"`
	Registration         RegistrationState            `json:"registration"`
	Heartbeat            HeartbeatState               `json:"heartbeat"`
	Overall              PlatformState                `json:"overall"`
	RegisterAt           *time.Time                   `json:"registerAt,omitempty"`
	RegisterExpiresAt    *time.Time                   `json:"registerExpiresAt,omitempty"`
	HeartbeatAt          *time.Time                   `json:"heartbeatAt,omitempty"`
	LastErrorCode        string                       `json:"lastErrorCode,omitempty"`
	LastErrorMessage     string                       `json:"lastErrorMessage,omitempty"`
	LastErrorAt          *time.Time                   `json:"lastErrorAt,omitempty"`
}

type ManagementService struct {
	store   ManagementStore
	sealer  CredentialSealer
	runtime ManagementRuntime
	clock   Clock
}

func NewManagementService(store ManagementStore, sealer CredentialSealer, runtime ManagementRuntime, clock Clock) *ManagementService {
	if clock == nil {
		clock = managementWallClock{}
	}
	return &ManagementService{store: store, sealer: sealer, runtime: runtime, clock: clock}
}

func (s *ManagementService) Create(ctx context.Context, input PlatformConfigInput) (*PlatformView, error) {
	if s == nil || s.store == nil {
		return nil, repository.ErrPlatformNotFound
	}
	platform, err := platformFromInput(input)
	if err != nil {
		return nil, err
	}
	if platform.Enabled && s.runtime == nil {
		return nil, ErrRuntimeUnavailable
	}
	if err := s.applyCredential(platform, input.Password); err != nil {
		return nil, err
	}
	if err := s.store.CreatePlatform(ctx, platform); err != nil {
		return nil, err
	}
	if platform.Enabled {
		if err := s.runtime.Reload(ctx); err != nil {
			// DB 已提交:返回已保存资源 + 降级错误,不谎报整体失败
			view := s.view(*platform)
			return &view, fmt.Errorf("%w: %v", ErrRuntimeSyncFailed, err)
		}
	}
	view := s.view(*platform)
	return &view, nil
}

func (s *ManagementService) Get(ctx context.Context, id uint64) (*PlatformView, error) {
	platform, err := s.store.FindPlatform(ctx, id)
	if err != nil {
		return nil, err
	}
	view := s.view(*platform)
	return &view, nil
}

func (s *ManagementService) List(ctx context.Context) ([]PlatformView, error) {
	platforms, err := s.store.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].ID < platforms[j].ID })
	views := make([]PlatformView, 0, len(platforms))
	for _, platform := range platforms {
		views = append(views, s.view(platform))
	}
	return views, nil
}

func (s *ManagementService) Update(ctx context.Context, id, expectedRevision uint64, input PlatformConfigInput) (*PlatformView, error) {
	stored, err := s.store.FindPlatform(ctx, id)
	if err != nil {
		return nil, err
	}
	updated, err := platformFromInput(input)
	if err != nil {
		return nil, err
	}
	updated.ID = stored.ID
	updated.SecretNonce = append([]byte(nil), stored.SecretNonce...)
	updated.SecretCiphertext = append([]byte(nil), stored.SecretCiphertext...)
	updated.SecretAlg = stored.SecretAlg
	updated.SecretKeyVersion = stored.SecretKeyVersion
	updated.RegisterAt = stored.RegisterAt
	updated.RegisterExpiresAt = stored.RegisterExpiresAt
	updated.HeartbeatAt = stored.HeartbeatAt
	updated.LastErrorCode = stored.LastErrorCode
	updated.LastErrorMessage = stored.LastErrorMessage
	updated.LastErrorAt = stored.LastErrorAt
	updated.ConfigRevision = stored.ConfigRevision
	updated.ProjectionRevision = stored.ProjectionRevision
	if err := s.applyCredential(updated, input.Password); err != nil {
		return nil, err
	}
	if (stored.Enabled || updated.Enabled) && s.runtime == nil {
		return nil, ErrRuntimeUnavailable
	}
	result, err := s.store.UpdatePlatformConfig(ctx, updated, expectedRevision)
	if err != nil {
		return nil, err
	}
	if stored.Enabled || result.Enabled {
		if err := s.runtime.Reload(ctx); err != nil {
			view := s.view(*result)
			return &view, fmt.Errorf("%w: %v", ErrRuntimeSyncFailed, err)
		}
	}
	view := s.view(*result)
	return &view, nil
}

func (s *ManagementService) SetEnabled(ctx context.Context, id, expectedRevision uint64, enabled bool) (*PlatformView, error) {
	if s.runtime == nil {
		return nil, ErrRuntimeUnavailable
	}
	platform, err := s.store.FindPlatform(ctx, id)
	if err != nil {
		return nil, err
	}
	platform.Enabled = enabled
	updated, err := s.store.UpdatePlatformConfig(ctx, platform, expectedRevision)
	if err != nil {
		return nil, err
	}
	if err := s.runtime.Reload(ctx); err != nil {
		view := s.view(*updated)
		return &view, fmt.Errorf("%w: %v", ErrRuntimeSyncFailed, err)
	}
	view := s.view(*updated)
	return &view, nil
}

func (s *ManagementService) Reconnect(ctx context.Context, id uint64) error {
	platform, err := s.store.FindPlatform(ctx, id)
	if err != nil {
		return err
	}
	if !platform.Enabled {
		return ErrPlatformDisabled
	}
	if s.runtime == nil || !containsPlatformID(s.runtime.PlatformIDs(), id) {
		return ErrRuntimeUnavailable
	}
	s.runtime.Reconnect(id)
	return nil
}

func (s *ManagementService) Delete(ctx context.Context, id uint64) error {
	platform, err := s.store.FindPlatform(ctx, id)
	if err != nil {
		return err
	}
	sessions, err := s.store.ListNonterminalMediaSessions(ctx, id)
	if err != nil {
		return err
	}
	if len(sessions) > 0 {
		return ErrPlatformHasActiveSessions
	}
	if platform.Enabled {
		if s.runtime == nil {
			return ErrRuntimeUnavailable
		}
		platform.Enabled = false
		platform, err = s.store.UpdatePlatformConfig(ctx, platform, platform.ConfigRevision)
		if err != nil {
			return err
		}
		if err := s.runtime.Reload(ctx); err != nil {
			return err
		}
	}
	if err := s.store.SoftDeletePlatform(ctx, id); err != nil {
		return err
	}
	if s.runtime != nil {
		return s.runtime.Reload(ctx)
	}
	return nil
}

func (s *ManagementService) ReplaceProjection(ctx context.Context, platformID uint64, expectedProjectionRevision uint64, devices []repository.DeviceProjectionInput, channels []repository.ChannelProjectionInput) error {
	if _, err := s.store.FindPlatform(ctx, platformID); err != nil {
		return err
	}
	return s.store.ReplaceProjection(ctx, platformID, expectedProjectionRevision, devices, channels)
}

func (s *ManagementService) Projection(ctx context.Context, platformID uint64) (*repository.ProjectionSnapshot, error) {
	return s.store.ProjectionSnapshot(ctx, platformID)
}

func (s *ManagementService) applyCredential(platform *model.GbCascadePlatform, password *string) error {
	if password == nil || *password == "" {
		return nil
	}
	if s.sealer == nil {
		return ErrCredentialUnavailable
	}
	envelope, err := s.sealer.Encrypt(upstreamPasswordPurpose, []byte(*password))
	if err != nil {
		return fmt.Errorf("seal cascade credential: %w", err)
	}
	platform.SecretNonce = append([]byte(nil), envelope.Nonce...)
	platform.SecretCiphertext = append([]byte(nil), envelope.Ciphertext...)
	platform.SecretAlg = envelope.Algorithm
	platform.SecretKeyVersion = envelope.KeyVersion
	return nil
}

func (s *ManagementService) view(platform model.GbCascadePlatform) PlatformView {
	state := DerivePlatformState(platform, s.clock, 0)
	return PlatformView{
		ID: platform.ID, Name: platform.Name, UpstreamServerID: platform.UpstreamServerID, UpstreamDomain: platform.UpstreamDomain,
		Host: platform.Host, Port: platform.Port, LocalDeviceID: platform.LocalDeviceID, LocalDomain: platform.LocalDomain,
		LocalSIPIP: platform.LocalSIPIP, LocalSIPPort: platform.LocalSIPPort, MediaAdvertiseIP: platform.MediaAdvertiseIP,
		AuthUsername: platform.AuthUsername, HasPassword: len(platform.SecretCiphertext) > 0,
		ProfileOverride: platform.ProfileOverride, EffectiveVersion: platform.EffectiveVersion, EffectiveVersionFrom: platform.EffectiveVersionFrom,
		CharsetOverride: platform.CharsetOverride, RegisterExpires: platform.RegisterExpires, KeepaliveInterval: platform.KeepaliveInterval,
		Transport: platform.Transport, CatalogBatchSize: platform.CatalogBatchSize, PublishPlatform: platform.PublishPlatform,
		PublishCivil: platform.PublishCivil, PublishGroup: platform.PublishGroup, MaxStreams: platform.MaxStreams, PTZEnabled: platform.PTZEnabled,
		Enabled: platform.Enabled, ConfigRevision: platform.ConfigRevision, ProjectionRevision: platform.ProjectionRevision,
		Registration: state.Registration, Heartbeat: state.Heartbeat, Overall: state.Overall,
		RegisterAt: platform.RegisterAt, RegisterExpiresAt: platform.RegisterExpiresAt, HeartbeatAt: platform.HeartbeatAt,
		LastErrorCode: platform.LastErrorCode, LastErrorMessage: platform.LastErrorMessage, LastErrorAt: platform.LastErrorAt,
	}
}

func platformFromInput(input PlatformConfigInput) (*model.GbCascadePlatform, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.UpstreamServerID = strings.TrimSpace(input.UpstreamServerID)
	input.UpstreamDomain = strings.TrimSpace(input.UpstreamDomain)
	input.Host = strings.TrimSpace(input.Host)
	input.LocalDeviceID = strings.TrimSpace(input.LocalDeviceID)
	input.LocalDomain = strings.TrimSpace(input.LocalDomain)
	input.LocalSIPIP = strings.TrimSpace(input.LocalSIPIP)
	input.MediaAdvertiseIP = strings.TrimSpace(input.MediaAdvertiseIP)
	input.AuthUsername = strings.TrimSpace(input.AuthUsername)
	input.Transport = strings.ToUpper(strings.TrimSpace(input.Transport))
	if input.Name == "" || !validGBID(input.UpstreamServerID) || input.UpstreamDomain == "" || !validHost(input.Host) || !validPort(input.Port) ||
		!validGBID(input.LocalDeviceID) || input.LocalDomain == "" || net.ParseIP(input.LocalSIPIP) == nil || !validPort(input.LocalSIPPort) ||
		(input.MediaAdvertiseIP != "" && net.ParseIP(input.MediaAdvertiseIP) == nil) || !validProfileOverride(input.ProfileOverride) ||
		(input.Transport != "UDP" && input.Transport != "TCP") || input.RegisterExpires <= 0 || input.KeepaliveInterval <= 0 ||
		input.CatalogBatchSize <= 0 || input.MaxStreams <= 0 {
		return nil, ErrInvalidPlatformConfig
	}
	return &model.GbCascadePlatform{
		Name: input.Name, UpstreamServerID: input.UpstreamServerID, UpstreamDomain: input.UpstreamDomain, Host: input.Host, Port: input.Port,
		LocalDeviceID: input.LocalDeviceID, LocalDomain: input.LocalDomain, LocalSIPIP: input.LocalSIPIP, LocalSIPPort: input.LocalSIPPort,
		MediaAdvertiseIP: input.MediaAdvertiseIP, AuthUsername: input.AuthUsername, ProfileOverride: input.ProfileOverride,
		CharsetOverride: strings.TrimSpace(input.CharsetOverride), RegisterExpires: input.RegisterExpires, KeepaliveInterval: input.KeepaliveInterval,
		RetryPolicy: input.RetryPolicy, Transport: input.Transport, CatalogBatchSize: input.CatalogBatchSize,
		PublishPlatform: input.PublishPlatform, PublishCivil: input.PublishCivil, PublishGroup: input.PublishGroup,
		MaxStreams: input.MaxStreams, PTZEnabled: input.PTZEnabled, Enabled: input.Enabled,
	}, nil
}

func validGBID(value string) bool {
	if len(value) != 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validHost(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func validPort(value int) bool { return value > 0 && value <= 65535 }

func validProfileOverride(value model.CascadeProfileOverride) bool {
	switch value {
	case model.CascadeProfileOverrideAuto, model.CascadeProfileOverride2016, model.CascadeProfileOverride2022:
		return true
	default:
		return false
	}
}

func containsPlatformID(ids []uint64, target uint64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

type managementWallClock struct{}

func (managementWallClock) Now() time.Time { return time.Now() }
