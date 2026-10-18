//go:build !linux && !darwin

package upgrade

import (
	"errors"
	"os"
)

func openTrustedRegistration(string) (*os.File, error) {
	return nil, errors.New("trusted updater registration is unsupported on this platform")
}
