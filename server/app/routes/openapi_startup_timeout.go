package routes

import (
	"time"

	"uvplatform.com/uvp-gb28181/app/global/app"
)

const (
	openAPIStartupTimeoutConfigKey = "openapi.startup_timeout_seconds"
	openAPIStartupTimeoutDefault   = 60 * time.Second
	openAPIStartupTimeoutMinimum   = 15 * time.Second
	openAPIStartupTimeoutMaximum   = 5 * time.Minute
)

func openAPIStartupTimeout(config app.YmlConfigInterf) time.Duration {
	if config == nil {
		return openAPIStartupTimeoutDefault
	}
	seconds := config.GetInt(openAPIStartupTimeoutConfigKey)
	timeout := time.Duration(seconds) * time.Second
	if timeout < openAPIStartupTimeoutMinimum || timeout > openAPIStartupTimeoutMaximum {
		return openAPIStartupTimeoutDefault
	}
	return timeout
}
