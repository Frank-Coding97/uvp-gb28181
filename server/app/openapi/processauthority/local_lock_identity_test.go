//go:build linux || darwin

package processauthority

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLocalAuthorityIdentityIgnoresVolumeDeviceNumber locks the reboot trap:
// Darwin renumbers APFS volumes on every boot, so a record that stores st_dev
// stops matching the very same lock file and startup fails with
// phase=process_authority reason=state_unavailable.
func TestLocalAuthorityIdentityIgnoresVolumeDeviceNumber(t *testing.T) {
	dir := privateStateDir(t)
	owner, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	live, err := localFileIdentity(owner.state.file)
	require.NoError(t, err)
	require.NoError(t, owner.Close())

	path := filepath.Join(dir, lockName)
	inode := fmt.Sprintf("%x", statInode(t, path))
	require.Equal(t, "unix:"+inode, live, "a fresh record must not embed the volume device number")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var domain localDomain
	require.NoError(t, json.Unmarshal(raw, &domain))
	// Rewrite the record with a stale device number, exactly what the next
	// reboot produces, without touching the file itself.
	domain.FileIdentity = "unix:deadbeef:" + inode
	rewritten, err := json.Marshal(domain)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(rewritten, '\n'), 0600))

	reopened, err := AcquireLocalLock(dir)
	require.NoError(t, err, "a stale device number must not deny the same file")
	defer func() { _ = reopened.Close() }()
	reopenedDomain, err := reopened.DomainID()
	require.NoError(t, err)
	require.Equal(t, domain.DomainID, reopenedDomain)
}

func statInode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return uint64(stat.Ino)
}
