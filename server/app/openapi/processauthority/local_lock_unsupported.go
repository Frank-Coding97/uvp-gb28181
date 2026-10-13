//go:build !linux && !darwin && !windows

package processauthority

import "os"

func lockLocalFile(*os.File) error               { return ErrLocalAuthorityUnavailable }
func secureLocalFile(*os.File, bool) bool        { return false }
func syncLocalDirectory(*os.File) error          { return ErrLocalAuthorityUnavailable }
func localFileIdentity(*os.File) (string, error) { return "", ErrLocalAuthorityUnavailable }
