package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// upgradeNotice is the lightweight payload the sidebar badge polls. It carries
// no task or event data on purpose: the service upgrade page keeps using
// /status, while the layout only needs "is a newer release offered?".
type upgradeNotice struct {
	CurrentVersion string `json:"currentVersion,omitempty"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	LatestBuildID  string `json:"latestBuildId,omitempty"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	ReleaseNotes   string `json:"releaseNotes,omitempty"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	HasUpdate      bool   `json:"hasUpdate"`
	// Ignored means an administrator hid exactly this release. A newer release
	// resets it implicitly because the version no longer matches.
	Ignored bool `json:"ignored"`
	// CheckEnabled is false on offline installations, which have no release
	// source. The badge must stay hidden there instead of showing an error.
	CheckEnabled bool `json:"checkEnabled"`
	// AutoCheck is the administrator's schedule switch: false means the helper
	// keeps its last result but stops probing the release source on a timer.
	// Manual checks from this page are unaffected.
	AutoCheck bool `json:"autoCheck"`
}

const ignoredUpdateFilename = "ignored-update.json"

type ignoredUpdateFile struct {
	Version string `json:"version"`
}

// Notice reports the background check result without contacting the release
// source itself. Probing happens in the helper every few hours.
func (c *ServiceUpgradeController) Notice(ctx *gin.Context) {
	state, checkEnabled := c.noticeState(ctx.Request.Context())
	notice := upgradeNotice{CheckEnabled: checkEnabled, AutoCheck: c.readAutoCheck()}
	if state != nil {
		notice.CurrentVersion = state.CurrentVersion
		notice.LatestVersion = state.LatestVersion
		notice.LatestBuildID = state.LatestBuildID
		notice.PublishedAt = state.PublishedAt
		notice.ReleaseNotes = state.ReleaseNotes
		notice.HasUpdate = state.HasUpdate
		if !state.CheckedAt.IsZero() {
			notice.CheckedAt = state.CheckedAt.UTC().Format(time.RFC3339)
		}
	}
	ignored := c.readIgnoredUpdate()
	notice.Ignored = ignored != "" && ignored == notice.LatestVersion
	c.Success(ctx, notice)
}

// IgnoreNotice hides the badge for the release currently offered. It refuses
// to store a version the helper is not offering, so a stale browser cannot
// suppress a release that appears later.
func (c *ServiceUpgradeController) IgnoreNotice(ctx *gin.Context) {
	var request struct {
		Version string `json:"version"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil {
		c.FailAndAbort(ctx, "参数解析失败", err, http.StatusBadRequest)
		return
	}
	version := strings.TrimSpace(request.Version)
	if !validReleaseVersion(version) {
		c.FailAndAbort(ctx, "版本号不合法", errors.New("version is required"), http.StatusBadRequest)
		return
	}
	state, _ := c.noticeState(ctx.Request.Context())
	if state == nil || !state.HasUpdate || state.LatestVersion != version {
		c.FailAndAbort(ctx, "该版本已不再提供", errors.New("no matching update is currently offered"), http.StatusConflict)
		return
	}
	if err := c.writeIgnoredUpdate(version); err != nil {
		c.FailAndAbort(ctx, "保存忽略设置失败", err, http.StatusInternalServerError)
		return
	}
	c.Success(ctx, gin.H{"version": version})
}

// ClearIgnoredNotice restores the badge for a previously ignored release.
func (c *ServiceUpgradeController) ClearIgnoredNotice(ctx *gin.Context) {
	if err := c.writeIgnoredUpdate(""); err != nil {
		c.FailAndAbort(ctx, "清除忽略设置失败", err, http.StatusInternalServerError)
		return
	}
	c.Success(ctx, gin.H{})
}

// SetAutoCheck flips the scheduled version check. The helper reads this file
// before every probe, so the change takes effect without restarting it; the
// last discovered result is kept either way.
func (c *ServiceUpgradeController) SetAutoCheck(ctx *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := ctx.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		c.FailAndAbort(ctx, "参数解析失败", errors.New("enabled is required"), http.StatusBadRequest)
		return
	}
	if err := upgrade.WriteAutoCheckPreference(c.autoCheckPath(), *request.Enabled); err != nil {
		c.FailAndAbort(ctx, "保存定时检查设置失败", err, http.StatusInternalServerError)
		return
	}
	c.Success(ctx, gin.H{"enabled": *request.Enabled})
}

// noticeState reads the helper cache. A missing or failed helper is not an
// error for the badge: the layout simply shows nothing.
func (c *ServiceUpgradeController) noticeState(ctx context.Context) (*upgrade.UpdateWatchState, bool) {
	if c == nil || c.local {
		return nil, false
	}
	response, err := c.callUpgradeRPC(ctx, upgrade.ProtocolRequest{Protocol: upgrade.UpdaterProtocolVersion, Action: upgrade.ActionStatus})
	if err != nil || !response.OK || response.Status == nil {
		return nil, false
	}
	return response.Status.UpdateCheck, response.Status.OnlineCheck
}

func (c *ServiceUpgradeController) ignoredUpdatePath() string {
	root := filepath.Dir(c.sequencePath)
	if strings.TrimSpace(root) == "" || root == "." {
		return filepath.Join(upgradeStateRoot(c.versionPath), ignoredUpdateFilename)
	}
	return filepath.Join(root, ignoredUpdateFilename)
}

// autoCheckPath lives next to ignored-update.json: both are business-owned
// preferences that the helper only reads.
func (c *ServiceUpgradeController) autoCheckPath() string {
	root := filepath.Dir(c.sequencePath)
	if strings.TrimSpace(root) == "" || root == "." {
		return filepath.Join(upgradeStateRoot(c.versionPath), upgrade.AutoCheckPreferenceFilename)
	}
	return filepath.Join(root, upgrade.AutoCheckPreferenceFilename)
}

// readAutoCheck mirrors the helper's default: only an explicit "disabled"
// turns scheduled checks off.
func (c *ServiceUpgradeController) readAutoCheck() bool {
	return upgrade.ReadAutoCheckPreference(c.autoCheckPath())
}

func (c *ServiceUpgradeController) readIgnoredUpdate() string {
	data, err := os.ReadFile(c.ignoredUpdatePath())
	if err != nil {
		return ""
	}
	var file ignoredUpdateFile
	if err := json.Unmarshal(data, &file); err != nil {
		// A corrupt preference file only means "nothing is ignored".
		return ""
	}
	return strings.TrimSpace(file.Version)
}

func (c *ServiceUpgradeController) writeIgnoredUpdate(version string) error {
	path := c.ignoredUpdatePath()
	if version == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(ignoredUpdateFile{Version: version})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// validReleaseVersion keeps the preference file free of path separators and
// control characters, since the value ends up in persisted JSON.
func validReleaseVersion(version string) bool {
	if version == "" || len(version) > 64 {
		return false
	}
	for _, char := range version {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '.' || char == '-' || char == '+' {
			continue
		}
		return false
	}
	return true
}
