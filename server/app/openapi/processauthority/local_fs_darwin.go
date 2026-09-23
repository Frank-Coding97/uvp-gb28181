package processauthority

import (
	"encoding/binary"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func persistentLocalFilesystem(file *os.File) bool {
	var fs unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &fs) != nil || fs.Flags&unix.MNT_LOCAL == 0 {
		return false
	}
	name := unix.ByteSliceToString(fs.Fstypename[:])
	return name == "apfs" || name == "hfs"
}

func secureLocalACL(file *os.File) bool {
	// Darwin ACLs can grant write access without changing POSIX mode bits.
	// Read ATTR_CMN_EXTENDED_SECURITY from the held descriptor and accept only
	// an absent/empty ACL, rather than interpreting Darwin ACE precedence.
	attrs := struct {
		Count, Reserved                       uint16
		Common, Volume, Directory, File, Fork uint32
	}{Count: 5, Common: unix.ATTR_CMN_EXTENDED_SECURITY}
	var data [4096]byte
	_, _, errno := syscall.Syscall6(unix.SYS_FGETATTRLIST, file.Fd(), uintptr(unsafe.Pointer(&attrs)), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, 0)
	runtime.KeepAlive(file)
	if errno != 0 {
		return false
	}
	return darwinACLAbsent(data[:])
}

func darwinACLAbsent(data []byte) bool {
	// attrBuf length, followed by attrreference (signed relative offset,
	// length), followed by kauth_filesec: magic, two GUIDs, ACL count/flags.
	if len(data) < 12 {
		return false
	}
	order := binary.NativeEndian
	total := int64(order.Uint32(data[:4]))
	start := int64(4) + int64(int32(order.Uint32(data[4:8])))
	length := int64(order.Uint32(data[8:12]))
	if total < 12 || total > int64(len(data)) || start < 12 || start+length > total {
		return false
	}
	if length == 0 {
		return true
	}
	if length != 44 || order.Uint32(data[start:start+4]) != 0x012cc16d {
		return false
	}
	count := order.Uint32(data[start+36 : start+40])
	return count == 0 || count == ^uint32(0)
}
