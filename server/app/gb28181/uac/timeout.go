package uac

import (
	"context"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
)

func withSIPCommandTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, gbconfig.SIPCommandTimeout())
}
