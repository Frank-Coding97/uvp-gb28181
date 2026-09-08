package launcher

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// Backup stops this installation when necessary, then copies only while the
// backup core owns its instance lock. A competing owner may cause a busy error.
func Backup(ctx context.Context, installDir, recordingsDir, destination string) (standalone.BackupManifest, error) {
	var manifest standalone.BackupManifest
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		return manifest, err
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: recordingsDir})
	if err != nil {
		return manifest, err
	}
	err = backupAfterStop(ctx, 100*time.Millisecond, func() error {
		var attemptErr error
		manifest, attemptErr = standalone.BackupStopped(ctx, paths, destination)
		return attemptErr
	}, func() error {
		stopCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return Stop(stopCtx, installDir, recordingsDir)
	})
	return manifest, err
}

func backupAfterStop(ctx context.Context, interval time.Duration, attempt, stop func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := attempt()
	if !errors.Is(err, standalone.ErrInstanceRunning) {
		return err
	}
	if err = stop(); err != nil {
		return fmt.Errorf("backup stop not confirmed: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		if waitCtx.Err() != nil {
			return fmt.Errorf("backup could not acquire maintenance ownership: %w", standalone.ErrInstanceRunning)
		}
		err = attempt()
		if !errors.Is(err, standalone.ErrInstanceRunning) {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return fmt.Errorf("backup could not acquire maintenance ownership: %w", standalone.ErrInstanceRunning)
		case <-timer.C:
		}
	}
}
