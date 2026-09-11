package standalone

import "golang.org/x/sys/unix"

func backupPublish(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
