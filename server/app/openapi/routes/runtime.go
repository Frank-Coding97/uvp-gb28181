package routes

import (
	"context"
	"encoding/base64"
	"os"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/controllers"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type RuntimeSettings interface {
	GetBool(string) bool
	GetString(string) string
	GetInt(string) int
	GetStringSlice(string) []string
}

// InitializeRuntime is startup-only, after DB migrations and Casbin initialization.
// The disabled machine gateway needs neither a database nor a master key.
// RestoreMediaSecurity still runs unconditionally at the application root.
// Enabled gateway startup
// cannot silently fall back after recovery/configuration/dependency failure.
func InitializeRuntime(ctx context.Context, db *gorm.DB, permissions client.ManagementPermissionAuthorizer, settings RuntimeSettings, media auth.MediaDispatcher) (*auth.Gateway, *controllers.ClientAdminController, error) {
	if settings == nil {
		return nil, nil, auth.ErrUnavailable
	}
	enabled := settings.GetBool("openapi.enabled")
	playEnabled := settings.GetBool("openapi.play_enabled")
	if playEnabled && (!enabled || media == nil) {
		return nil, nil, auth.ErrUnavailable
	}
	if !enabled {
		return nil, nil, nil
	}
	if db == nil || permissions == nil {
		return nil, nil, auth.ErrUnavailable
	}
	if err := checkRuntimeSchema(ctx, db); err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	timeout := settings.GetInt("openapi.request_timeout_seconds")
	if timeout == 0 {
		timeout = 5
	}
	if timeout < 2 || timeout > 60 || settings.GetInt("httpserver.write_timeout") <= timeout+1 {
		return nil, nil, auth.ErrUnavailable
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(os.Getenv("UVP_OPENAPI_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		return nil, nil, auth.ErrUnavailable
	}
	keys, err := client.NewSecretManager(key, settings.GetString("openapi.master_key_id"))
	clear(key)
	if err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	revocation := playauth.NewOpenAPIRevocationStore(db, time.Now)
	service, err := client.NewService(db, keys,
		client.WithManagementBoundary(client.NewManagementScopeBoundary(db, permissions)),
		client.WithRevocationIntentStore(revocation))
	if err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	options := make([]auth.GatewayOption, 0, 1)
	if playEnabled {
		options = append(options, auth.WithMediaDispatcher(media))
	}
	gate, err := auth.NewGateway(ctx, db, keys, auth.GatewayConfig{Audience: settings.GetString("openapi.audience"), TLSProxies: settings.GetStringSlice("openapi.tls_terminator_proxies"), Timeout: time.Duration(timeout) * time.Second, AuditReserve: time.Second, MaxInFlight: 64}, options...)
	if err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	// Recording intent immediately revokes new admission, even when media control
	// is unavailable. No worker is started here: progress is durable evidence only,
	// and empty/unobserved targets remain unknown, never inferred closed.
	progress := func(ctx context.Context, clientID int64) (controllers.RevocationView, error) {
		state, err := revocation.Progress(ctx, clientID)
		if err != nil {
			return controllers.RevocationView{}, err
		}
		return controllers.RevocationView{Status: state.Status, Pending: state.Pending, Closed: state.Closed}, nil
	}
	return gate, controllers.NewClientAdminController(db, service, permissions, progress), nil
}

// ActivateMediaSecurity commits the one-way authentication latch before any
// media root can become ready. Runtime projection changes only after the
// durable transaction succeeds.
func ActivateMediaSecurity(ctx context.Context, db *gorm.DB, requireAuth func()) error {
	if requireAuth == nil {
		return openapiconfig.ErrUnavailable
	}
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Latch(ctx)
	if err != nil || !state.MustAuthLocked {
		return openapiconfig.ErrUnavailable
	}
	requireAuth()
	return nil
}

// RestoreMediaSecurity is mandatory before GB/HTTP startup even when OpenAPI
// is disabled. Missing/unreadable persistent state stops startup; there is no
// inference from YAML, clients, grants or an empty viewer list.
func RestoreMediaSecurity(ctx context.Context, db *gorm.DB, requireAuth func()) error {
	if requireAuth == nil {
		return openapiconfig.ErrUnavailable
	}
	state, err := openapiconfig.NewMustAuthStore(db, time.Now).Load(ctx)
	if err != nil {
		return err
	}
	if state.MustAuthLocked {
		requireAuth()
	}
	return nil
}

// Read-only, zero-row probes check actual columns, not merely table existence.
// They neither migrate schema nor load client secrets into diagnostics.
func checkRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	for _, model := range []any{&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}, &models.PlayGrant{}, &models.Viewer{}} {
		if err := db.WithContext(ctx).Session(&gorm.Session{QueryFields: true}).Where("1 = 0").Find(model).Error; err != nil {
			return auth.ErrUnavailable
		}
	}
	for _, probe := range []struct{ table, columns string }{
		{"sys_department", "id,name,status,deleted_at"},
		{"gb_device", "id,device_id,owner_dept_id,deleted_at,name,alias,manufacturer,model,status"},
		{"gb_channel", "id,device_id,channel_id,owner_dept_id,deleted_at,name,alias,manufacturer,model,status,ptz_type"},
	} {
		var rows []map[string]any
		if err := db.WithContext(ctx).Table(probe.table).Select(probe.columns).Where("1 = 0").Find(&rows).Error; err != nil {
			return auth.ErrUnavailable
		}
	}
	return nil
}
