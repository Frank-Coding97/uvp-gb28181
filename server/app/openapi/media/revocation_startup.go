package media

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/openapi/config"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var ErrRevocationNotConfigured = errors.New("openapi revocation control is not configured")

// StartConfiguredRevocation loads startup-only trust and starts compensation,
// never playback. Call after GB registry startup. The returned stop cancels and
// joins all worker activity; root must call it before GB/media teardown.
func StartConfiguredRevocation(ctx context.Context, db *gorm.DB, registry RevocationNodeRegistry, path string, report func(RevocationTickResult, error)) (func(), error) {
	if path == "" {
		return nil, ErrRevocationNotConfigured
	}
	if ctx == nil || ctx.Err() != nil || db == nil || registry == nil {
		return nil, ErrRevocationWorkerUnavailable
	}
	if concrete, ok := registry.(*node.Registry); ok && concrete == nil {
		return nil, ErrRevocationWorkerUnavailable
	}
	bindings, err := LoadNodeControlBindings(path)
	if err != nil {
		return nil, err
	}
	checkContext, cancelCheck := context.WithTimeout(ctx, 5*time.Second)
	defer cancelCheck()
	for _, model := range []any{&models.PlayGrant{}, &models.Viewer{}, &models.MediaNodeSecurity{}} {
		if err := db.WithContext(checkContext).Session(&gorm.Session{QueryFields: true}).Where("1 = 0").Find(model).Error; err != nil {
			return nil, ErrRevocationWorkerUnavailable
		}
	}
	var rows []map[string]any
	if err := db.WithContext(checkContext).Table("meta_node").Select("id,revision,media_server_uuid").Where("1 = 0").Find(&rows).Error; err != nil {
		return nil, ErrRevocationWorkerUnavailable
	}
	worker := NewRevocationWorker(db, NewTrustedRevocationFactory(registry, bindings, config.NewNodeRuntimeStore(db, time.Now)), time.Now, 0)
	runContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.RunMaintenance(runContext, report)
	}()
	return func() { cancel(); <-done }, nil
}
