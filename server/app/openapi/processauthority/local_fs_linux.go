package processauthority

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// supportedLocalFilesystem 列出可承担"本机持久独占"职责的文件系统。
//
// ⛔ overlayfs 是**刻意放行**的（2026-10-12 客户事故后）：绿色包经常被装进容器里跑，
// 而容器的根目录就是 overlayfs —— 不放行就是"装得上、起不来"（后端只留一行
// phase=process_authority reason=state_unavailable，且**不含原因**）。
//
// 放行前实测过（220，用本包代码）：同一组 lower/upper/work 重挂后 st_dev/st_ino 不变
// （旁边另挂一个 overlay 干扰也一样）；真容器里 docker restart / stop-start 后 domain
// 一致。⇒ LocalLock 里那份 fileIdentity 仍能对上，其"把 domain 字节拷到新文件不能
// 继承授权"的语义没有被削弱，故本次**未改身份判定**。
// 容器 rm+run 重建会让状态目录随容器一起消失 ⇒ 换成新 domain；单机包的 DB 同在包里、
// 一并重置，因此不会撞上 database_domain_mismatch。
func supportedLocalFilesystem(fsType int64) bool {
	switch fsType {
	case unix.EXT4_SUPER_MAGIC,
		unix.XFS_SUPER_MAGIC,
		unix.BTRFS_SUPER_MAGIC,
		unix.F2FS_SUPER_MAGIC,
		unix.OVERLAYFS_SUPER_MAGIC:
		return true
	default:
		// Network and memory filesystems cannot establish the deployment's
		// persistent local authority domain by this check.
		return false
	}
}

func persistentLocalFilesystem(file *os.File) bool {
	var fs unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &fs) != nil {
		return false
	}
	return supportedLocalFilesystem(fs.Type)
}

func secureLocalACL(file *os.File) bool {
	for _, attr := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		_, err := unix.Fgetxattr(int(file.Fd()), attr, nil)
		if !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
			return false
		}
	}
	return true
}
