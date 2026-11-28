package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// VersionInfo 版本信息结构
type VersionInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	BuildID string `json:"build_id"`
}

// AppVersion 全局版本信息
var AppVersion VersionInfo

// BuildID is injected for release binaries with:
// -X uvplatform.com/uvp-gb28181/app/global/app.BuildID=<release-build-id>
// The package version file must carry the same value before a candidate can
// identify itself as an upgrade target.
var BuildID string

// RuntimeBuildID returns the identity embedded in the executing binary. A
// clean VCS revision is useful for local development, but a dirty worktree is
// deliberately not accepted as a releasable runtime identity.
func RuntimeBuildID() string {
	if buildID := strings.TrimSpace(BuildID); buildID != "" {
		return buildID
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	var revision string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			if setting.Value == "true" {
				return ""
			}
		}
	}
	return revision
}

// RuntimeVersionIdentity only returns an identity when the package metadata
// and executing binary agree. Callers use an empty result as a fail-closed
// candidate readiness result.
func RuntimeVersionIdentity() (version, buildID string) {
	version = strings.TrimSpace(AppVersion.Version)
	buildID = RuntimeBuildID()
	if version == "" || buildID == "" || strings.TrimSpace(AppVersion.BuildID) != buildID {
		return "", ""
	}
	return version, buildID
}

// LoadVersionInfo 从 version.json 加载版本信息
func LoadVersionInfo() error {
	versionPath := strings.TrimSpace(os.Getenv("UVP_VERSION_PATH"))
	if versionPath == "" {
		versionPath = filepath.Join(BasePath, "version.json")
	}
	data, err := os.ReadFile(versionPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &AppVersion)
}
