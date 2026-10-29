package upgrade

import (
	"errors"
	"os"
	"time"
)

var (
	ErrTaskBusy            = errors.New("another upgrade task is active")
	ErrIdempotencyConflict = errors.New("idempotency key belongs to a different request")
	errUpgradeLockBusy     = errors.New("upgrade state lock is busy")
)

type upgradeLock struct{ file *os.File }

func acquireUpgradeLock(path string) (*upgradeLock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockUpgradeFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &upgradeLock{file: file}, nil
}

func acquireUpgradeLockWait(path string) (*upgradeLock, error) {
	var last error
	for attempt := 0; attempt < 200; attempt++ {
		lock, err := acquireUpgradeLock(path)
		if err == nil {
			return lock, nil
		}
		last = err
		if !errors.Is(err, errUpgradeLockBusy) {
			return nil, err
		}
		time.Sleep(time.Millisecond)
	}
	return nil, last
}

func (l *upgradeLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockUpgradeFile(l.file)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
