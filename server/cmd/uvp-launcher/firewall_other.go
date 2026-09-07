//go:build !windows

package main

import (
	"context"

	"uvplatform.cn/uvp-gb28181/internal/standalone/firewall"
)

func requireFirewallElevation() error {
	return firewall.NewError(firewall.ReasonUnsupportedPlatform)
}

func elevateFirewall(context.Context, firewall.Action, string, string) error {
	return firewall.NewError(firewall.ReasonUnsupportedPlatform)
}
