package logging

import "golang.org/x/sys/unix"

func renameLogNoReplace(fd int, from, to string) error {
	return unix.RenameatxNp(fd, from, fd, to, unix.RENAME_EXCL)
}
