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
	crashHelperEnv = "UVP_T18_CRASH_CREATE_ADMIN_HELPER"
	crashDBEnv     = "UVP_T18_CRASH_DB"
	crashReadyEnv  = "UVP_T18_CRASH_READY"
	crashReadyText = "user-row-written-before-commit"
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

	readyPath := filepath.Join(t.TempDir(), "create-admin.ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransactionHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		crashHelperEnv+"=1",
		crashDBEnv+"="+path,
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

	require.NoError(t, waitForCrashReady(readyPath, 10*time.Second))
	require.NoError(t, cmd.Process.Kill())
	select {
	case err := <-waitResult:
		waited = true
		require.Error(t, err, "the helper must be terminated before its transaction commits")
	case <-time.After(5 * time.Second):
		t.Fatal("terminated helper did not exit")
	}

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

func TestStandaloneCreateAdminProcessKillRollsBackUncommittedTransactionHelper(t *testing.T) {
	if os.Getenv(crashHelperEnv) != "1" {
		return
	}
	dbPath := os.Getenv(crashDBEnv)
	readyPath := os.Getenv(crashReadyEnv)
	if dbPath == "" || readyPath == "" {
		t.Fatal("crash helper requires test database and ready paths")
	}
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
	if err := db.Callback().Create().After("gorm:create").Register("t18_crash_after_user_create", func(tx *gorm.DB) {
		if tx.Statement.Table != "sys_users" || paused {
			return
		}
		paused = true
		if err := os.WriteFile(readyPath, []byte(crashReadyText), 0o600); err != nil {
			panic(err)
		}
		select {}
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := NewStore(db).CreateAdmin(ctx, "crash-admin", "Admin!Passw0rd#2026"); err == nil {
		t.Fatal("CreateAdmin returned even though its transaction callback was paused")
	}
}

func waitForCrashReady(path string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			if string(data) == crashReadyText {
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
