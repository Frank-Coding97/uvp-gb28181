package processauthority

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestProcessAuthorityRegistrationHelper(t *testing.T) {
	dir := os.Getenv("UVP_AUTHORITY_REGISTER_CHILD")
	if dir == "" {
		return
	}
	lock, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	defer lock.Close()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "ledger.sqlite")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	base, err := db.DB()
	require.NoError(t, err)
	defer base.Close()
	authority, err := Register(context.Background(), db, lock)
	require.NoError(t, err)
	duplicate, err := Register(context.Background(), db, lock)
	require.ErrorIs(t, err, ErrProcessAuthorityUnavailable)
	require.Nil(t, duplicate)
	if old := os.Getenv("UVP_AUTHORITY_REGISTER_OLD"); old != "" {
		require.NotEqual(t, old, authority.GenerationID())
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return authority.RequireRetiredTx(tx, old) }))
	}
	require.NoError(t, db.Transaction(authority.CheckTx))
	fmt.Fprintln(os.Stdout, authority.GenerationID())
	_, _ = io.Copy(io.Discard, os.Stdin)
	authority.Seal()
	require.ErrorIs(t, db.Transaction(authority.CheckTx), ErrProcessAuthorityUnavailable)
}

func TestProcessAuthorityRegistersOnlyAfterActualOwnerProcessExit(t *testing.T) {
	dir := privateStateDir(t)
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "ledger.sqlite")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	base, err := db.DB()
	require.NoError(t, err)
	defer base.Close()
	require.NoError(t, db.AutoMigrate(&models.ProcessGeneration{}, &models.ProcessAuthority{}))
	start := func(old string) (*exec.Cmd, io.WriteCloser, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessAuthorityRegistrationHelper$")
		cmd.Env = append(os.Environ(), "UVP_AUTHORITY_REGISTER_CHILD="+dir, "UVP_AUTHORITY_REGISTER_OLD="+old)
		input, err := cmd.StdinPipe()
		require.NoError(t, err)
		output, err := cmd.StdoutPipe()
		require.NoError(t, err)
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
		id, err := bufio.NewReader(output).ReadString('\n')
		require.NoError(t, err)
		id = strings.TrimSpace(id)
		require.True(t, validProcessIdentity(id), "%s", id)
		return cmd, input, id
	}
	first, _, old := start("")
	contender, err := AcquireLocalLock(dir)
	require.ErrorIs(t, err, ErrLocalAuthorityBusy)
	require.Nil(t, contender)
	require.NoError(t, first.Process.Kill())
	require.Error(t, first.Wait())
	second, input, current := start(old)
	require.NotEqual(t, old, current)
	require.NoError(t, input.Close())
	require.NoError(t, second.Wait())
	var generations []models.ProcessGeneration
	require.NoError(t, db.Find(&generations).Error)
	require.Len(t, generations, 2)
	var authority models.ProcessAuthority
	require.NoError(t, db.Take(&authority).Error)
	require.Equal(t, current, authority.CurrentGenerationID)
	require.Equal(t, generations[0].DomainID, generations[1].DomainID)
}
