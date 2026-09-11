package winprocess

import (
	"errors"
	"os"
)

// StartSpec describes one process started under a Job.
//
// Path and Dir must be absolute Windows paths. Args excludes Path. Env is an
// explicit environment block; a nil Env uses the current process environment.
// Missing standard streams are connected to NUL.
type StartSpec struct {
	NoConsole bool
	Path      string
	Args      []string
	Env       []string
	Dir       string
	Stdin     *os.File
	Stdout    *os.File
	Stderr    *os.File
}

var (
	// ErrUnsupported is returned by this package on non-Windows platforms.
	ErrUnsupported = errors.New("standalone process jobs are unsupported on this platform")
	// ErrJobClosed means the Job can no longer start processes.
	ErrJobClosed = errors.New("standalone process job is closed")
	// ErrProcessClosed means the Process handle was already released.
	ErrProcessClosed = errors.New("standalone process handle is closed")
)
