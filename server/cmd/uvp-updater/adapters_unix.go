//go:build !windows

package main

import (
	"errors"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

func adapterLayout(adapter upgrade.DeploymentAdapter) (upgrade.RegisteredLayout, error) {
	switch adapter := adapter.(type) {
	case *upgrade.StandaloneAdapter:
		return adapter.Layout, nil
	case *upgrade.SystemdAdapter:
		return adapter.Layout, nil
	case *upgrade.ComposeAdapter:
		return adapter.Layout, nil
	default:
		return upgrade.RegisteredLayout{}, errors.New("registered deployment adapter is unsupported")
	}
}

func adapterDeployment(adapter upgrade.DeploymentAdapter) (string, error) {
	switch adapter.(type) {
	case *upgrade.StandaloneAdapter:
		return "standalone", nil
	case *upgrade.SystemdAdapter:
		return "systemd", nil
	case *upgrade.ComposeAdapter:
		return "compose", nil
	default:
		return "", errors.New("registered deployment adapter is unsupported")
	}
}
