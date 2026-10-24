package upgrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type AutoUpgradePolicy struct {
	Enabled            bool   `json:"enabled"`
	PolicyVersion      uint64 `json:"policy_version"`
	Timezone           string `json:"timezone"`
	WindowStart        string `json:"window_start"`
	WindowEnd          string `json:"window_end"`
	Channel            string `json:"channel"`
	Deployment         string `json:"deployment,omitempty"`
	OS                 string `json:"os,omitempty"`
	Arch               string `json:"arch,omitempty"`
	PatchOnly          bool   `json:"patch_only"`
	AllowRestart       bool   `json:"allow_restart"`
	AutoRollback       bool   `json:"auto_rollback"`
	NotificationTarget string `json:"notification_target,omitempty"`
	RetainVersions     int    `json:"retain_versions"`
	CreatedBy          string `json:"created_by,omitempty"`
}

var hhmmPattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

func (p AutoUpgradePolicy) Validate() error {
	if p.PolicyVersion == 0 {
		return errors.New("policy_version must be positive")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("invalid IANA timezone: %w", err)
	}
	if !hhmmPattern.MatchString(p.WindowStart) || !hhmmPattern.MatchString(p.WindowEnd) {
		return errors.New("maintenance window must use HH:MM")
	}
	if p.Channel != "stable" && p.Channel != "beta" {
		return errors.New("unsupported upgrade channel")
	}
	if p.Deployment != "" && p.Deployment != "standalone" && p.Deployment != "systemd" && p.Deployment != "compose" {
		return errors.New("unsupported deployment in upgrade policy")
	}
	if p.OS != "" && (len(p.OS) > 32 || !safePolicyToken(p.OS)) {
		return errors.New("invalid operating system in upgrade policy")
	}
	if p.Arch != "" && (len(p.Arch) > 32 || !safePolicyToken(p.Arch)) {
		return errors.New("invalid architecture in upgrade policy")
	}
	if p.NotificationTarget != "" && len(p.NotificationTarget) > 512 {
		return errors.New("notification target is too long")
	}
	if p.RetainVersions < 0 || p.RetainVersions > 50 {
		return errors.New("retain_versions must be between 0 and 50")
	}
	if p.CreatedBy != "" && len(p.CreatedBy) > 128 {
		return errors.New("created_by is too long")
	}
	return nil
}

func safePolicyToken(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' && r != '_' {
			return false
		}
	}
	return value != ""
}

func (p AutoUpgradePolicy) InWindow(now time.Time) bool {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return false
	}
	local := now.In(loc)
	start, _ := time.ParseInLocation("15:04", p.WindowStart, loc)
	end, _ := time.ParseInLocation("15:04", p.WindowEnd, loc)
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()
	nowMinutes := local.Hour()*60 + local.Minute()
	if startMinutes == endMinutes {
		return false
	}
	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

func (p AutoUpgradePolicy) Allows(manifest Manifest, current string) bool {
	return p.AllowsAt(manifest, current, time.Now())
}

func (p AutoUpgradePolicy) AllowsAt(manifest Manifest, current string, now time.Time) bool {
	return p.InWindow(now) && p.AllowsRelease(manifest, current)
}

// AllowsRelease is independent from the window so verified artifacts can be
// downloaded in advance while activation remains confined to maintenance.
func (p AutoUpgradePolicy) AllowsRelease(manifest Manifest, current string) bool {
	if !p.Enabled || manifest.Channel != p.Channel || !isSupportedVersion(current) || !isSupportedVersion(manifest.Version) {
		return false
	}
	if manifest.Version == current || !IsVersionAtLeast(manifest.Version, current) {
		return false
	}
	if manifest.MinVersion != "" && !IsVersionAtLeast(current, manifest.MinVersion) {
		return false
	}
	if p.Channel == "stable" && strings.Contains(manifest.Version, "-") {
		return false
	}
	return !p.PatchOnly || IsPatchOrMinorUpgrade(current, manifest.Version)
}

func (p AutoUpgradePolicy) AllowsTarget(target Target) bool {
	if p.Deployment != "" && p.Deployment != target.Deployment {
		return false
	}
	if p.OS != "" && p.OS != target.OS {
		return false
	}
	if p.Arch != "" && p.Arch != target.Arch {
		return false
	}
	// Every supported deployment has a service/container interruption during
	// activation. Never let an enabled policy silently authorize that when the
	// administrator did not explicitly allow automatic restart.
	return p.AllowRestart
}

func IsPatchOrMinorUpgrade(current, target string) bool {
	currentParts := parseSemver(current)
	targetParts := parseSemver(target)
	if currentParts[0] != targetParts[0] {
		return false
	}
	return targetParts[1] >= currentParts[1]
}

func parseSemver(value string) [3]int {
	var out [3]int
	var major, minor, patch int
	if _, err := fmt.Sscanf(value, "%d.%d.%d", &major, &minor, &patch); err == nil {
		out = [3]int{major, minor, patch}
	}
	return out
}

type PolicyStore struct{ path string }

func OpenPolicyStore(path string) (*PolicyStore, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("policy path must be absolute")
	}
	return &PolicyStore{path: path}, nil
}

func (s *PolicyStore) Load() (AutoUpgradePolicy, error) {
	if s == nil || s.path == "" {
		return AutoUpgradePolicy{}, errors.New("policy store is not initialized")
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return AutoUpgradePolicy{PolicyVersion: 1, Timezone: "Asia/Shanghai", WindowStart: "02:00", WindowEnd: "04:00", Channel: "stable", PatchOnly: true, RetainVersions: 2}, nil
	}
	if err != nil {
		return AutoUpgradePolicy{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var policy AutoUpgradePolicy
	if err := decoder.Decode(&policy); err != nil {
		return AutoUpgradePolicy{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return AutoUpgradePolicy{}, errors.New("policy contains multiple JSON values")
		}
		return AutoUpgradePolicy{}, err
	}
	if err := policy.Validate(); err != nil {
		return AutoUpgradePolicy{}, err
	}
	return policy, nil
}

func (s *PolicyStore) Save(policy AutoUpgradePolicy) error {
	if s == nil || s.path == "" {
		return errors.New("policy store is not initialized")
	}
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	return s.save(policy)
}

// SaveNext allocates the revision under the same interprocess lock as the
// durable write. Client supplied revisions never grant newer authorization.
func (s *PolicyStore) SaveNext(policy AutoUpgradePolicy) (AutoUpgradePolicy, error) {
	lock, err := s.lock()
	if err != nil {
		return AutoUpgradePolicy{}, err
	}
	defer lock.Close()
	current, err := s.Load()
	if err != nil {
		return AutoUpgradePolicy{}, err
	}
	if current.PolicyVersion == ^uint64(0) {
		return AutoUpgradePolicy{}, errors.New("policy revision exhausted")
	}
	policy.PolicyVersion = current.PolicyVersion + 1
	if err := s.save(policy); err != nil {
		return AutoUpgradePolicy{}, err
	}
	return policy, nil
}

func (s *PolicyStore) lock() (*upgradeLock, error) {
	if s == nil || s.path == "" {
		return nil, errors.New("policy store is not initialized")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return nil, err
	}
	return acquireUpgradeLockWait(s.path + ".lock")
}

func (s *PolicyStore) save(policy AutoUpgradePolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return nil
}
