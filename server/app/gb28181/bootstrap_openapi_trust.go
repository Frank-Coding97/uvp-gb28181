package gb28181

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	openapiconfig "uvplatform.cn/uvp-gb28181/app/openapi/config"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
)

var startupOpenAPITrust struct {
	once     sync.Once
	bindings *openapimedia.FileNodeControlBindings
	err      error
}

// LoadStartupOpenAPIControlBindingsOnce must be primed by main before GB Start.
// SIP assembly also calls it for standalone roots. Success, absence and failure
// are all process-lifetime results: neither Stop nor Reload can replace trust.
func LoadStartupOpenAPIControlBindingsOnce() (*openapimedia.FileNodeControlBindings, error) {
	startupOpenAPITrust.once.Do(func() {
		startupOpenAPITrust.err = openapimedia.ErrRevocationNotConfigured
		if app.ConfigYml == nil {
			return
		}
		path := app.ConfigYml.GetString("openapi.revocation_bindings_file")
		if path == "" {
			return
		}
		startupOpenAPITrust.bindings, startupOpenAPITrust.err = openapimedia.LoadNodeControlBindings(path)
	})
	return startupOpenAPITrust.bindings, startupOpenAPITrust.err
}

func configurePlaybackRTPCleanup(inviter *uac.UAC, db *gorm.DB, intents *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier) error {
	bindings, err := LoadStartupOpenAPIControlBindingsOnce()
	if err != nil {
		app.ZapLog.Warn("持久RTP清理缺少启动信任，保留SIP恢复与pending状态", zap.String("event", "gb28181.lifecycle.rtp_cleanup_trust_missing"), zap.Error(err))
		return nil
	}
	if zlmRegistry == nil {
		app.ZapLog.Warn("持久RTP清理缺少节点注册表，保留SIP恢复与pending状态", zap.String("event", "gb28181.lifecycle.rtp_cleanup_registry_missing"))
		return nil
	}
	factory := openapimedia.NewTrustedRevocationFactory(zlmRegistry, bindings, openapiconfig.NewNodeRuntimeStore(db, time.Now))
	return inviter.ConfigurePlaybackRTPCleanup(context.Background(), intents, barrier, playback.NewZLMRTPCleanupResolver(zlmRegistry, factory))
}
