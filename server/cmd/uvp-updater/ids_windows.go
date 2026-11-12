//go:build windows

package main

// Windows has no process UID/GID. The helper and the business process always
// run as the same interactive account, so a single fixed identity (0) keeps the
// registration payload well formed without inventing a fake account.
func defaultLocalUID() uint { return 0 }
func defaultLocalGID() uint { return 0 }
