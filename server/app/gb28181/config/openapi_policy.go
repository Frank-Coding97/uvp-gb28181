package config

import (
	"errors"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

var ErrPlayAuthRequired = errors.New("play authorization is required by the persistent OpenAPI security commitment")
var mustAuthRequired atomic.Bool

// RequirePlayAuth installs a requirement loaded from the durable security
// singleton before GB/HTTP startup. There is deliberately no inverse function.
// This memory latch is only the effective-policy projection, not persistence.
func RequirePlayAuth() {
	fixedAddressPlaybackMu.Lock()
	defer fixedAddressPlaybackMu.Unlock()
	mustAuthRequired.Store(true)
}

// PlayAuthConfigConflict lets the configuration watcher report a fixed,
// secret-free warning without rewriting the operator's configuration file.
func PlayAuthConfigConflict() bool {
	fixedAddressPlaybackMu.RLock()
	defer fixedAddressPlaybackMu.RUnlock()
	return mustAuthRequired.Load() && !PlayAuthSettingsFrom(app.ConfigYml).Enabled
}
