package processauthority

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// 白名单清单逐条锁死：增删任何一条都会在这里变红。
// 刻意放行两类：ext4/xfs/btrfs/f2fs（"本机持久"）与 overlayfs（容器根，理由见 local_fs_linux.go）。
func TestSupportedLocalFilesystemInventory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		magic  int64
		accept bool
	}{
		{"ext4", unix.EXT4_SUPER_MAGIC, true},
		{"xfs", unix.XFS_SUPER_MAGIC, true},
		{"btrfs", unix.BTRFS_SUPER_MAGIC, true},
		{"f2fs", unix.F2FS_SUPER_MAGIC, true},
		{"overlayfs", unix.OVERLAYFS_SUPER_MAGIC, true},
		{"tmpfs", unix.TMPFS_MAGIC, false},
		{"ramfs", unix.RAMFS_MAGIC, false},
		{"nfs", unix.NFS_SUPER_MAGIC, false},
		{"proc", unix.PROC_SUPER_MAGIC, false},
		{"sysfs", unix.SYSFS_MAGIC, false},
		{"unknown", 0x1badb002, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.accept, supportedLocalFilesystem(tc.magic))
		})
	}
}

// overlayfs 的魔法值是内核 ABI，不许"顺手改"。
func TestOverlayFilesystemMagicIsPinned(t *testing.T) {
	require.Equal(t, int64(0x794c7630), int64(unix.OVERLAYFS_SUPER_MAGIC))
}

// 真机回归：只有临时目录真的落在 overlayfs 上才激活
// （开发机/普通 CI 一般不在 → skip；在容器里跑测试时会命中）。
func TestLocalAuthorityWorksOnOverlayfsWhenPresent(t *testing.T) {
	dir := privateStateDir(t)
	var fs unix.Statfs_t
	require.NoError(t, unix.Statfs(dir, &fs))
	if fs.Type != unix.OVERLAYFS_SUPER_MAGIC {
		t.Skip("临时目录不在 overlayfs 上，跳过（该用例用于回归客户容器场景）")
	}
	first, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	domain, err := first.DomainID()
	require.NoError(t, err)
	require.NoError(t, first.Close())

	// 同一个目录二次拿锁必须认回同一个 domain —— 也就是"重挂载后还能起"。
	second, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	again, err := second.DomainID()
	require.NoError(t, err)
	require.Equal(t, domain, again)
}
