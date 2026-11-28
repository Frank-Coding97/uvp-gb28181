//go:build !windows

package upgrade

import (
	"archive/tar"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const controlName = "uvp-gb28181-ctl.sh"

func zlmControlScript(persistent bool) string {
	path := "$root/current/vendor/zlm/config.ini"
	if persistent {
		path = "$root/config/zlm.ini"
	}
	return "#!/bin/sh\nroot=$(CDPATH= cd -- \"$(dirname \"$0\")\" && pwd)\n" +
		"if [ \"$1\" = start ]; then cat \"" + path + "\" > \"$root/observed.ini\"; fi\n"
}

func writeControlUpgrade(t *testing.T, path, control string, mode int64) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	tw := tar.NewWriter(f)
	entries := []struct {
		name, body string
		mode       int64
	}{
		{"release/bin/uvp-server", "new", 0700},
		{"release/version.json", `{"version":"1.2.3","build_id":"candidate"}`, 0600},
		{"release/vendor/zlm/config.ini", shippedINI, 0600},
	}
	if control != "" {
		entries = append(entries, struct {
			name, body string
			mode       int64
		}{"release/" + controlName, control, mode})
	}
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body))}))
		_, err = tw.Write([]byte(e.body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, f.Close())
}

func standaloneControlFixture(t *testing.T, flat bool) (*StandaloneAdapter, TaskRecord) {
	t.Helper()
	root := t.TempDir()
	old := filepath.Join(root, "releases", "old")
	if flat {
		old = root
	}
	require.NoError(t, os.MkdirAll(filepath.Join(old, "vendor", "zlm"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(old, legacyZLMConfigPath), []byte(legacyINI), 0644))
	if !flat {
		require.NoError(t, os.Symlink(old, filepath.Join(root, "current")))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, controlName), []byte(zlmControlScript(false)), 0755))
	artifact := filepath.Join(root, "upgrade.tar")
	writeControlUpgrade(t, artifact, zlmControlScript(true), 0700)
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, controlName)}}
	record := TaskRecord{TaskID: strings.Repeat("d", 32), StagingPath: artifact, PreviousTargetCaptured: true}
	if !flat {
		record.PreviousTarget = old
	}
	return a, record
}

func TestStandaloneUpgradeStartsWithPersistentZLMConfig(t *testing.T) {
	a, record := standaloneControlFixture(t, false)
	require.NoError(t, os.MkdirAll(filepath.Join(a.Layout.Root, "config"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(a.Layout.Root, persistentZLMConfigPath), []byte(legacyINI), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(record.PreviousTarget, legacyZLMConfigPath), []byte(shippedINI), 0644))
	require.NoError(t, a.Activate(context.Background(), record))
	require.NoError(t, a.Validate(context.Background(), TaskRecord{}))
	got, err := os.ReadFile(filepath.Join(a.Layout.Root, "observed.ini"))
	require.NoError(t, err)
	require.Equal(t, legacyINI, string(got), "实际启动入口仍在读取出厂模板")
	// 页面保存新值后，连续升级与再次启动都必须读到它。
	edited := strings.ReplaceAll(legacyINI, "62.234.152.39", "198.51.100.9")
	require.NoError(t, os.WriteFile(filepath.Join(a.Layout.Root, persistentZLMConfigPath), []byte(edited), 0644))
	for _, id := range []string{strings.Repeat("e", 32), strings.Repeat("f", 32)} {
		record.TaskID = id
		require.NoError(t, a.Activate(context.Background(), record))
		require.NoError(t, a.Validate(context.Background(), TaskRecord{}))
		got, err = os.ReadFile(filepath.Join(a.Layout.Root, "observed.ini"))
		require.NoError(t, err)
		require.Equal(t, edited, string(got))
	}
}

func TestStandaloneFirstFlatUpgradePreservesZLMConfig(t *testing.T) {
	a, record := standaloneControlFixture(t, true)
	require.NoError(t, a.Activate(context.Background(), record))
	got, err := os.ReadFile(filepath.Join(a.Layout.Root, persistentZLMConfigPath))
	require.NoError(t, err, "平铺安装首次升级未迁移实际配置")
	require.Equal(t, legacyINI, string(got))
	require.NoError(t, a.Validate(context.Background(), TaskRecord{}))
	got, err = os.ReadFile(filepath.Join(a.Layout.Root, "observed.ini"))
	require.NoError(t, err)
	require.Equal(t, legacyINI, string(got))
}

func TestStandaloneControlRollbackRestoresOriginalOnRetry(t *testing.T) {
	for _, flat := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "flat"}[flat], func(t *testing.T) {
			a, record := standaloneControlFixture(t, flat)
			original := strings.ReplaceAll(zlmControlScript(false), "$root/current/vendor", "$root/vendor")
			require.NoError(t, os.WriteFile(a.Layout.Control, []byte(original), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(a.Layout.Root, "vendor", "zlm"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(a.Layout.Root, legacyZLMConfigPath), []byte(legacyINI), 0644))
			for i := 0; i < 2; i++ {
				require.NoError(t, a.Activate(context.Background(), record))
			}
			installed, err := os.ReadFile(a.Layout.Control)
			require.NoError(t, err)
			require.Equal(t, zlmControlScript(true), string(installed))
			record.Phase = PhaseActivating
			digest := strings.Repeat("a", 64)
			record.Preflight = &PreflightSnapshot{CurrentVersion: "1.0.0", CurrentBuildID: "old", DatabaseDialect: "mysql", MigrationDigest: digest}
			record.Compatibility = &CompatibilityDeclaration{AutomaticAllowed: true, Database: DatabaseCompatibility{Dialect: "mysql", TargetMigrationDigest: digest}, Rollback: &RollbackCompatibility{AutomaticAllowed: true, DatabaseCompatible: true, DependenciesCompatible: true}}
			identity, err := expectedRollbackIdentity(record)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(identity) }))
			defer server.Close()
			a.Layout.ReadinessURL = server.URL + RuntimeIdentityPath
			a.Probe = RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second}
			require.NoError(t, a.Rollback(context.Background(), record))
			installed, err = os.ReadFile(a.Layout.Control)
			require.NoError(t, err)
			require.Equal(t, original, string(installed), "重试覆盖了原入口备份")
			observed, err := os.ReadFile(filepath.Join(a.Layout.Root, "observed.ini"))
			require.NoError(t, err)
			require.Equal(t, legacyINI, string(observed))
			if flat {
				_, err = os.Lstat(a.Layout.Current)
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

func TestStandaloneUpgradeRejectsNonExecutableControl(t *testing.T) {
	a, record := standaloneControlFixture(t, false)
	writeControlUpgrade(t, record.StagingPath, zlmControlScript(true), 0600)
	require.Error(t, a.Activate(context.Background(), record))
	current, err := os.Readlink(a.Layout.Current)
	require.NoError(t, err)
	require.Equal(t, record.PreviousTarget, current)
	original, err := os.ReadFile(a.Layout.Control)
	require.NoError(t, err)
	require.Equal(t, zlmControlScript(false), string(original))
}

func TestStandaloneUpgradeKeepsLegacyPackageAndCustomControl(t *testing.T) {
	t.Run("no packaged control", func(t *testing.T) {
		a, record := standaloneControlFixture(t, false)
		writeControlUpgrade(t, record.StagingPath, "", 0)
		require.NoError(t, a.Activate(context.Background(), record))
		got, err := os.ReadFile(a.Layout.Control)
		require.NoError(t, err)
		require.Equal(t, zlmControlScript(false), string(got))
	})
	t.Run("custom control", func(t *testing.T) {
		a, record := standaloneControlFixture(t, false)
		custom := filepath.Join(a.Layout.Root, "custom.sh")
		require.NoError(t, os.Rename(a.Layout.Control, custom))
		a.Layout.Control = custom
		require.NoError(t, a.Activate(context.Background(), record))
		got, err := os.ReadFile(custom)
		require.NoError(t, err)
		require.Equal(t, zlmControlScript(false), string(got))
	})
}
