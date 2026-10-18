package controllers

import (
	"context"
	"errors"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// serviceUpgradeRPC is the only stateful upgrade boundary used by the
// business process in production. Keeping the call behind a tiny interface
// lets controller tests inject a fake without creating helper-owned state in
// the application process.
type serviceUpgradeRPC interface {
	Call(context.Context, string, upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error)
}

type serviceUpgradeRPCCall func(context.Context, string, upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error)

func (f serviceUpgradeRPCCall) Call(ctx context.Context, socket string, request upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error) {
	return f(ctx, socket, request)
}

func defaultServiceUpgradeRPC(ctx context.Context, socket string, request upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error) {
	return upgrade.CallUnix(ctx, socket, request)
}

func (c *ServiceUpgradeController) callUpgradeRPC(ctx context.Context, request upgrade.ProtocolRequest) (upgrade.ProtocolResponse, error) {
	if c == nil || c.rpc == nil {
		return upgrade.ProtocolResponse{}, errors.New("upgrade helper RPC is not configured")
	}
	return c.rpc.Call(ctx, c.updaterSocket(), request)
}

func upgradeRPCResponseError(response upgrade.ProtocolResponse) error {
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return errors.New("upgrade helper rejected request")
}
