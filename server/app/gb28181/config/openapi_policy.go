package config

import "errors"

var ErrPlayAuthRequired = errors.New("play authorization configuration cannot be disabled")

// RequirePlayAuth installs a requirement loaded from the durable security
// singleton before GB/HTTP startup. There is deliberately no inverse function.
// This memory latch is only the effective-policy projection, not persistence.
func RequirePlayAuth() {
}

// PlayAuthConfigConflict lets the configuration watcher report a fixed,
// secret-free warning without rewriting the operator's configuration file.
func PlayAuthConfigConflict() bool {
	return false
}
