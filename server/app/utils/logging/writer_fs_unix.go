//go:build linux || darwin

package logging

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

type logDirectory struct {
	file       *os.File
	beforeSync func() error
}

func openLogDirectory(path string) (*logDirectory, error) {
	if e := os.MkdirAll(path, 0700); e != nil {
		return nil, e
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, e
	}
	fd, e := unix.Open(resolved, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	return &logDirectory{file: os.NewFile(uintptr(fd), resolved)}, nil
}
func (d *logDirectory) fd() int { return int(d.file.Fd()) }
func leaf(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}
func safeFileStat(s *unix.Stat_t) bool {
	return s.Mode&unix.S_IFMT == unix.S_IFREG && s.Uid == uint32(os.Geteuid()) && s.Nlink == 1 && s.Mode&0022 == 0
}
func (d *logDirectory) stat(name string) (unix.Stat_t, error) {
	var s unix.Stat_t
	if !leaf(name) {
		return s, errUnsafeLogFile
	}
	e := unix.Fstatat(d.fd(), name, &s, unix.AT_SYMLINK_NOFOLLOW)
	if e != nil {
		return s, e
	}
	if !safeFileStat(&s) {
		return s, errUnsafeLogFile
	}
	return s, nil
}
func (d *logDirectory) open(name string, flags int) (*os.File, error) {
	if !leaf(name) {
		return nil, errUnsafeLogFile
	}
	fd, e := unix.Openat(d.fd(), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	var stat unix.Stat_t
	if e = unix.Fstat(fd, &stat); e != nil || !safeFileStat(&stat) {
		_ = unix.Close(fd)
		return nil, errors.Join(errUnsafeLogFile, e)
	}
	return os.NewFile(uintptr(fd), name), nil
}
func (d *logDirectory) matches(name string, f *os.File) error {
	current, e := d.stat(name)
	if e != nil {
		return e
	}
	var opened unix.Stat_t
	if e = unix.Fstat(int(f.Fd()), &opened); e != nil {
		return e
	}
	if current.Dev != opened.Dev || current.Ino != opened.Ino {
		return errUnsafeLogFile
	}
	return nil
}
func (d *logDirectory) rename(from, to string, expected *os.File) error {
	if !leaf(to) {
		return errUnsafeLogFile
	}
	if e := d.matches(from, expected); e != nil {
		return e
	}
	if e := renameLogNoReplace(d.fd(), from, to); e != nil {
		return e
	}
	if e := d.matches(to, expected); e != nil {
		return e
	}
	return d.sync()
}

type fileIdentity struct{ dev, ino uint64 }

func identity(s unix.Stat_t) fileIdentity { return fileIdentity{uint64(s.Dev), uint64(s.Ino)} }
func openedIdentity(f *os.File) (fileIdentity, error) {
	var s unix.Stat_t
	e := unix.Fstat(int(f.Fd()), &s)
	return identity(s), e
}
func (d *logDirectory) remove(name string, expected fileIdentity) error {
	f, e := d.open(name, unix.O_RDONLY)
	if e != nil {
		return e
	}
	defer f.Close()
	actual, e := openedIdentity(f)
	if e != nil {
		return e
	}
	if actual != expected {
		return errUnsafeLogFile
	}
	if e = d.matches(name, f); e != nil {
		return e
	}
	if e = unix.Unlinkat(d.fd(), name, 0); e != nil {
		return e
	}
	return d.sync()
}
func (d *logDirectory) entries() ([]os.DirEntry, error) {
	fd, e := unix.Openat(d.fd(), ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), ".")
	defer f.Close()
	return f.ReadDir(-1)
}
func lockLogFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }

const logRead = unix.O_RDONLY
const logAppend = unix.O_WRONLY | unix.O_APPEND
const logCreate = unix.O_CREAT
const logExclusive = unix.O_EXCL

func (d *logDirectory) sync() error {
	if d.beforeSync != nil {
		if e := d.beforeSync(); e != nil {
			return e
		}
	}
	return d.file.Sync()
}
