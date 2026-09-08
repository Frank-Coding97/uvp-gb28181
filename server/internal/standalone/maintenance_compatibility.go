package standalone

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Set only by the controlled launcher build, after the exact backend builds
// have passed maintenance/generation acceptance. No installation file or
// manifest can grant itself compatibility. An ordinary build trusts nothing.
var maintenanceBackendSHA256Allowlist string

// loadMaintenanceReleases must run before creating the maintenance journal or
// changing config, data or current.json. The caller keeps the instance lock
// through the later operation and revalidates releases before execution.
func loadMaintenanceReleases(installDir, candidateVersion string) (Release, Release, error) {
	return loadMaintenanceReleasesWithTrust(installDir, candidateVersion, maintenanceBackendSHA256Allowlist)
}

func loadMaintenanceReleasesWithTrust(installDir, candidateVersion, allowlist string) (Release, Release, error) {
	denied := errors.New("standalone: release is not qualified for automatic upgrade and recovery")
	trusted := map[string]bool{}
	for _, value := range strings.Split(allowlist, ",") {
		value = strings.TrimSpace(value)
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			return Release{}, Release{}, denied
		}
		trusted[strings.ToLower(value)] = true
	}
	current, err := LoadRelease(installDir)
	if err != nil {
		return Release{}, Release{}, err
	}
	candidate, err := LoadReleaseVersion(installDir, candidateVersion)
	if err != nil {
		return Release{}, Release{}, err
	}
	releases, err := loadInstalledMaintenanceReleases(installDir)
	if err != nil {
		return Release{}, Release{}, err
	}
	for _, release := range releases {
		digest, err := releaseFileSHA256(release.BackendExe)
		if err != nil || !trusted[strings.ToLower(digest)] {
			return Release{}, Release{}, denied
		}
	}
	return current, candidate, nil
}

func loadInstalledMaintenanceReleases(installDir string) ([]Release, error) {
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(root, "releases")
	if _, err := ensureReleaseDirectory(directory); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var releases []Release
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, errors.New("unrecognized object in installed releases")
		}
		release, err := LoadReleaseVersion(root, entry.Name())
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	if len(releases) == 0 {
		return nil, errors.New("no installed releases")
	}
	return releases, nil
}
