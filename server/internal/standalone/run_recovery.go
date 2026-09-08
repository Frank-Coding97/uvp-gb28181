package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

var ErrUncleanRecoveryRequired = errors.New("standalone: previous run did not stop cleanly; local recovery is required")

type RunMarkerObservation struct {
	PreviousUnclean bool
	SHA256          string
	Nonce           string
	StartedAt       string
}

// InspectRunMarker never replaces or clears evidence from an earlier lifetime.
// The caller must own InstanceLock before inspecting startup admission.
func InspectRunMarker(paths Paths) (observation RunMarkerObservation, failure error) {
	if !paths.Explicit || !isWithin(paths.InstallDir, paths.DataDir) {
		return observation, ErrPathOutsideInstall
	}
	if err := requireMaintenanceInstanceLock(paths.InstallDir); err != nil {
		return observation, err
	}
	failure = withConfigLock(paths.InstallDir, func() error {
		path := filepath.Join(paths.DataDir, runMarkerName)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errors.Join(ErrUncleanRecoveryRequired, err)
		}
		if info.Size() > 4096 {
			return errors.Join(ErrUncleanRecoveryRequired, errRunMarkerInvalid)
		}
		raw, err := readSecureConfigFile(path)
		if err != nil {
			return errors.Join(ErrUncleanRecoveryRequired, err)
		}
		payload, err := decodeRunMarker(raw)
		if err != nil {
			return errors.Join(ErrUncleanRecoveryRequired, err)
		}
		digest := sha256.Sum256(raw)
		observation = RunMarkerObservation{PreviousUnclean: true, SHA256: hex.EncodeToString(digest[:]), Nonce: payload.Nonce, StartedAt: payload.StartedAt}
		return ErrUncleanRecoveryRequired
	})
	return observation, failure
}
