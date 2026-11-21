//go:build windows

package upgrade

// syncDirectoryPath is a no-op on Windows.
//
// FlushFileBuffers on a directory handle always fails with ERROR_ACCESS_DENIED:
// Windows has no notion of fsyncing a directory entry. The durability we get
// from the Unix call is covered here by the filesystem journal — NTFS and ReFS
// log the rename, and a crashed rename resolves to either the old or the new
// name, never to a missing one. The data itself is already flushed by the
// caller syncing the temporary file before the rename, so returning nil keeps
// the same guarantee without pretending the call succeeded.
func syncDirectoryPath(string) error {
	return nil
}
