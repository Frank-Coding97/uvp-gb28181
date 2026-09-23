package logging

import "golang.org/x/sys/unix"

func renameLogNoReplace(fd int, from, to string) error {
	return unix.Renameat2(fd, from, fd, to, unix.RENAME_NOREPLACE)
}
