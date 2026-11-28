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

// 模拟旧助手已把 current 切到新包，却未更新安装根的启动入口。
func TestNewHelperRepairsControlAfterLegacyUpgrade(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "recovery gate"}[blocked], func(t *testing.T) {
			a, _ := standaloneControlFixture(t, false)
			require.NoError(t, a.Layout.validate())
			store, err := OpenTaskStore(a.Layout.State)
			require.NoError(t, err)
			defer store.Close()
			record, err := store.Submit(TaskRequest{IdempotencyKey: "legacy", Actor: "admin", TargetDigest: strings.Repeat("a", 64)})
			require.NoError(t, err)
			for _, phase := range []TaskPhase{PhaseVerifying, PhasePreparing, PhaseAwaitingConfirmation, PhaseStopping, PhaseBackingUp, PhaseMigrating, PhaseActivating, PhaseValidating, PhaseCommitting, PhaseSucceeded} {
				require.NoError(t, store.MarkPhase(record.TaskID, phase))
			}
			old, err := a.CurrentTarget()
			require.NoError(t, err)
			require.NoError(t, store.UpdateMetadata(record.TaskID, func(r *TaskRecord) error { r.PreviousTarget = old; return nil }))
			target := filepath.Join(a.Layout.Releases, record.TaskID)
			require.NoError(t, os.MkdirAll(filepath.Join(target, "vendor", "zlm"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(target, legacyZLMConfigPath), []byte(shippedINI), 0644))
			script := strings.ReplaceAll(zlmControlScript(true), `[ "$1" = start ]`, `[ "$1" = start ] || [ "$1" = restart-zlm ]`)
			require.NoError(t, os.WriteFile(filepath.Join(target, controlName), []byte(script), 0755))
			require.NoError(t, os.Remove(a.Layout.Current))
			require.NoError(t, os.Symlink(target, a.Layout.Current))
			gate, err := OpenGateStore(filepath.Join(a.Layout.State, "gate.json"))
			require.NoError(t, err)
			if blocked {
				require.NoError(t, gate.Save(GateState{Mode: GateRecovery, TaskID: record.TaskID, BuildID: "candidate", Reason: "operator recovery"}))
			}
			w := &WorkflowExecutor{Store: store, Adapter: a, Gate: gate}
			require.NoError(t, w.Recover(context.Background()))
			got, err := os.ReadFile(a.Layout.Control)
			require.NoError(t, err)
			if blocked {
				require.Equal(t, zlmControlScript(false), string(got))
				return
			}
			require.Equal(t, script, string(got), "新助手启动后仍未接管旧入口")
			got, err = os.ReadFile(filepath.Join(a.Layout.Root, "observed.ini"))
			require.NoError(t, err)
			require.Equal(t, legacyINI, string(got), "首次过渡必须立即恢复实际配置")
			// 后续助手重启不应重复重启 ZLM。
			require.NoError(t, os.Remove(filepath.Join(a.Layout.Root, "observed.ini")))
			require.NoError(t, w.Recover(context.Background()))
			require.NoFileExists(t, filepath.Join(a.Layout.Root, "observed.ini"))
		})
	}
}

func TestStandaloneControlSyncKeepsUpgradeProgressAction(t *testing.T) {
	a, record := standaloneControlFixture(t, false)
	script := zlmControlScript(true) + "# stop-for-upgrade\n"
	writeControlUpgrade(t, record.StagingPath, script, 0700)
	require.NoError(t, a.Activate(context.Background(), record))
	require.Equal(t, "stop-for-upgrade", standaloneUpgradeStopAction(a.Layout))
	before, err := os.Stat(a.Layout.Control)
	require.NoError(t, err)
	require.NoError(t, a.installControl(filepath.Join(a.Layout.Releases, record.TaskID), record))
	after, err := os.Stat(a.Layout.Control)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after), "相同脚本不应重复替换")
}

func TestStandaloneControlRejectsEscapingOrSymlinkedPayload(t *testing.T) {
	for _, scenario := range []string{"outside release", "symlink payload", "symlink releases", "oversized payload", "symlink entry"} {
		t.Run(scenario, func(t *testing.T) {
			a, record := standaloneControlFixture(t, false)
			require.NoError(t, a.Layout.validate())
			external := t.TempDir()
			release := filepath.Join(a.Layout.Releases, record.TaskID)
			require.NoError(t, os.MkdirAll(release, 0755))
			source := filepath.Join(release, controlName)
			switch scenario {
			case "outside release":
				release = external
				source = filepath.Join(release, controlName)
			case "symlink payload":
				target := filepath.Join(external, "control.sh")
				require.NoError(t, os.WriteFile(target, []byte(zlmControlScript(true)), 0755))
				require.NoError(t, os.Symlink(target, source))
			case "symlink releases":
				a.Layout.Releases = filepath.Join(a.Layout.Root, "linked-releases")
				require.NoError(t, os.Symlink(external, a.Layout.Releases))
				release = filepath.Join(a.Layout.Releases, record.TaskID)
				require.NoError(t, os.MkdirAll(release, 0755))
				source = filepath.Join(release, controlName)
			case "symlink entry":
				target := filepath.Join(external, "custom.sh")
				require.NoError(t, os.Rename(a.Layout.Control, target))
				require.NoError(t, os.Symlink(target, a.Layout.Control))
			}
			if scenario != "symlink payload" {
				body := zlmControlScript(true)
				if scenario == "oversized payload" {
					body = strings.Repeat("x", 1<<20+1)
				}
				require.NoError(t, os.WriteFile(source, []byte(body), 0755))
			}
			require.Error(t, a.installControl(release, record))
			got, err := os.ReadFile(a.Layout.Control)
			require.NoError(t, err)
			require.Equal(t, zlmControlScript(false), string(got))
		})
	}
}
