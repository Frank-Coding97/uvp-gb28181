//go:build windows

package installation

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

const (
	crashHelperEnv         = "UVP_T18_CRASH_CREATE_ADMIN_HELPER"
	crashModeEnv           = "UVP_T18_CRASH_MODE"
	crashModeCreateAdmin   = "create-admin"
	crashModeCompleteSIP   = "complete-sip"
	crashDBEnv             = "UVP_T18_CRASH_DB"
	crashReadyEnv          = "UVP_T18_CRASH_READY"
	crashCreateReadyText   = "user-row-written-before-commit"
	crashCompleteReadyText = "meta-node-written-before-commit"
)

// TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransaction is a
// Windows process boundary test. The child pauses from a real GORM create
// callback after the user row is written inside CreateAdmin's transaction;
// the parent then terminates only that child and reopens the same SQLite file.
// It proves crash recovery for CreateAdmin, rather than an HTTP interruption.
func TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransaction(t *testing.T) {
	if os.Getenv(crashHelperEnv) == "1" {
		t.Skip("helper process")
	}
	db, path := newInstallationDB(t)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	runCrashHelper(t, path, crashModeCreateAdmin, crashCreateReadyText)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reopened, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })
	store := NewStore(reopened)

	state, err := store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhasePendingAdmin, state.Phase)
	require.Zero(t, countTable(t, reopened, "sys_users"))
	require.Zero(t, countTable(t, reopened, "sys_user_role"))
	require.Zero(t, countTable(t, reopened, "sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'"))
	require.Zero(t, countTable(t, reopened, "meta_node"))

	state, err = store.CreateAdmin(ctx, "retry-admin", "Admin!Passw0rd#2026")
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, state.Phase)
	require.EqualValues(t, 1, countTable(t, reopened, "sys_users"))
	require.EqualValues(t, 1, countTable(t, reopened, "sys_user_role"))
	require.EqualValues(t, 1, countTable(t, reopened, "sys_casbin_rule WHERE ptype = 'g' AND v0 LIKE 'user_%'"))
	require.Zero(t, countTable(t, reopened, "meta_node"))

	state, err = store.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, state.Phase)
}

// TestStandaloneCompleteSIPWithMediaProcessKillRollsBackUncommittedTransaction
// exercises the same process boundary after the real media-node INSERT. The
// parent reopens the same file after killing its child, then retries the real
// completion transaction to prove the interrupted SIP and node writes were
// atomic with the pending_sip state transition.
func TestStandaloneCompleteSIPWithMediaProcessKillRollsBackUncommittedTransaction(t *testing.T) {
	if os.Getenv(crashHelperEnv) == "1" {
		t.Skip("helper process")
	}
	db, path := newInstallationDB(t)
	ctx := installationContext(t)
	store := NewStore(db)
	require.NoError(t, createPendingSIPAdmin(store, ctx))

	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	runCrashHelper(t, path, crashModeCompleteSIP, crashCompleteReadyText)

	reopened, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })
	reopenedStore := NewStore(reopened)

	state, err := reopenedStore.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhasePendingSIP, state.Phase)
	require.Zero(t, countTable(t, reopened, "gb_sip_config"), "interrupted completion must not enable SIP")
	require.Zero(t, countTable(t, reopened, "meta_node"), "interrupted completion must not publish a media node")

	_, err = reopenedStore.CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	require.NoError(t, err)
	state, err = reopenedStore.State(ctx)
	require.NoError(t, err)
	require.Equal(t, PhaseComplete, state.Phase)
	require.EqualValues(t, 1, countTable(t, reopened, "gb_sip_config"))
	require.EqualValues(t, 1, countTable(t, reopened, "meta_node"))
}

func runCrashHelper(t *testing.T, dbPath, mode, expectedMarker string) {
	t.Helper()
	readyPath := filepath.Join(t.TempDir(), mode+".ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransactionHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		crashHelperEnv+"=1",
		crashModeEnv+"="+mode,
		crashDBEnv+"="+dbPath,
		crashReadyEnv+"="+readyPath,
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	require.NoError(t, cmd.Start())
	waitResult := make(chan error, 1)
	go func() { waitResult <- cmd.Wait() }()
	waited := false
	t.Cleanup(func() {
		if waited {
			return
		}
		_ = cmd.Process.Kill()
		select {
		case <-waitResult:
		case <-time.After(5 * time.Second):
			t.Errorf("crash helper did not exit during cleanup")
		}
	})

	require.NoError(t, waitForCrashReady(readyPath, expectedMarker, 10*time.Second))
	require.NoError(t, cmd.Process.Kill())
	select {
	case err := <-waitResult:
		waited = true
		require.Error(t, err, "the helper must be terminated before its transaction commits")
	case <-time.After(5 * time.Second):
		t.Fatal("terminated helper did not exit")
	}
}

func TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransactionHelper(t *testing.T) {
	if os.Getenv(crashHelperEnv) != "1" {
		return
	}
	dbPath := os.Getenv(crashDBEnv)
	readyPath := os.Getenv(crashReadyEnv)
	if dbPath == "" || readyPath == "" {
		t.Fatal("crash helper requires test database and ready paths")
	}
	mode := os.Getenv(crashModeEnv)
	if mode == "" {
		mode = crashModeCreateAdmin
	}
	targetTable, readyText := crashHookTarget(mode)
	db, err := gormhelper.NewSQLiteClient(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	paused := false
	if err := db.Callback().Create().After("gorm:create").Register("t18_crash_after_key_create", func(tx *gorm.DB) {
		table := tx.Statement.Table
		if table == "" && tx.Statement.Schema != nil {
			table = tx.Statement.Schema.Table
		}
		if table != targetTable || paused {
			return
		}
		paused = true
		if err := os.WriteFile(readyPath, []byte(readyText), 0o600); err != nil {
			panic(err)
		}
		select {}
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var operationErr error
	switch mode {
	case crashModeCreateAdmin:
		_, operationErr = NewStore(db).CreateAdmin(ctx, "crash-admin", "Admin!Passw0rd#2026")
	case crashModeCompleteSIP:
		_, operationErr = NewStore(db).CompleteSIPWithMedia(ctx, validMediaSIPRequest(), localZLMConfig())
	default:
		t.Fatalf("unknown crash helper mode %q", mode)
	}
	if operationErr == nil {
		t.Fatal("operation returned even though its transaction callback was paused")
	}
}

func crashHookTarget(mode string) (string, string) {
	switch mode {
	case crashModeCreateAdmin:
		return "sys_users", crashCreateReadyText
	case crashModeCompleteSIP:
		return "meta_node", crashCompleteReadyText
	default:
		return "", ""
	}
}

func waitForCrashReady(path, expected string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			if string(data) == expected {
				return nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-deadline.C:
			return errors.New("timed out waiting for crash helper callback")
		case <-ticker.C:
		}
	}
}
