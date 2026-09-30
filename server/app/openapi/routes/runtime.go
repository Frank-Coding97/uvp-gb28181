package routes

import (
	"context"
	"encoding/base64"
	"os"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
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
// Enabled gateway startup
// cannot silently fall back after recovery/configuration/dependency failure.
func InitializeRuntime(ctx context.Context, db *gorm.DB, permissions client.ManagementPermissionAuthorizer, settings RuntimeSettings, ptz auth.PTZDispatcher, playback ...auth.PlayDispatcher) (*auth.Gateway, *controllers.ClientAdminController, error) {
	if settings == nil {
		return nil, nil, auth.ErrUnavailable
	}
	enabled := settings.GetBool("openapi.enabled")
	if !enabled {
		return nil, nil, nil
	}
	if db == nil || permissions == nil {
		return nil, nil, auth.ErrUnavailable
	}
	if err := checkRuntimeSchema(ctx, db); err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	catalogRuntime, err := InitializeCatalogRuntime(ctx, db)
	if err != nil {
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
	service, err := client.NewService(db, keys, client.WithManagementBoundary(client.NewManagementScopeBoundary(db, permissions)))
	if err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	options := make([]auth.GatewayOption, 0, 2)
	if ptz != nil {
		options = append(options, auth.WithPTZDispatcher(ptz))
	}
	if len(playback) > 0 && playback[0] != nil {
		options = append(options, auth.WithPlayDispatcher(playback[0]))
	}
	options = append(options, auth.WithCatalogRuntime(catalogRuntime))
	gate, err := auth.NewGateway(ctx, db, keys, auth.GatewayConfig{Audience: settings.GetString("openapi.audience"), Timeout: time.Duration(timeout) * time.Second, AuditReserve: time.Second, MaxInFlight: 64}, options...)
	if err != nil {
		return nil, nil, auth.ErrUnavailable
	}
	return gate, controllers.NewClientAdminController(db, service, permissions), nil
}

// Read-only, zero-row probes check actual columns, not merely table existence.
// They neither migrate schema nor load client secrets into diagnostics.
func checkRuntimeSchema(ctx context.Context, db *gorm.DB) error {
	for _, model := range []any{&models.Client{}, &models.ClientScope{}, &models.Nonce{}, &models.Audit{}} {
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
