//go:build windows

package main

import (
	"errors"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// A Windows registration is always a standalone green installation, so the
// deployment label stays "standalone": that is what the published release
// manifests declare, and the adapter choice is made by GOOS, not by the label.
func adapterLayout(adapter upgrade.DeploymentAdapter) (upgrade.RegisteredLayout, error) {
	switch adapter := adapter.(type) {
	case *upgrade.WindowsStandaloneAdapter:
		return adapter.Layout, nil
	default:
		return upgrade.RegisteredLayout{}, errors.New("registered deployment adapter is unsupported on windows")
	}
}

func adapterDeployment(adapter upgrade.DeploymentAdapter) (string, error) {
	switch adapter.(type) {
	case *upgrade.WindowsStandaloneAdapter:
		return "standalone", nil
	default:
		return "", errors.New("registered deployment adapter is unsupported on windows")
	}
}
