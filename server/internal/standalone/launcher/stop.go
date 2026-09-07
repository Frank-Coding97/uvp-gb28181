package launcher

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v8"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
	"uvplatform.cn/uvp-gb28181/internal/standalone/control"
)

// Stop reads the existing protected instance configuration. It does not create
// configuration, acquire ownership, or discover processes by PID or image name.
func Stop(ctx context.Context, installDir, recordingsDir string) error {
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		return err
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: recordingsDir})
	if err != nil {
		return err
	}
	config, err := standalone.LoadConfig(paths)
	if err != nil {
		return err
	}
	name, key, err := control.Endpoint(paths.InstallDir, "launcher", config.JWTSecret())
	if err != nil {
		return err
	}
	reply, err := control.Call(ctx, name, key, control.Stop)
	if err != nil {
		return err
	}
	if reply != control.Finalized {
		return errors.New("instance did not confirm a complete shutdown")
	}
	return nil
}

func shutdownRedis(ctx context.Context, address, password string) error {
	client := redis.NewClient(&redis.Options{Addr: address, Password: password, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	defer client.Close()
	// go-redis normalizes the expected SHUTDOWN EOF. Runtime additionally waits
	// for the exact owned process to exit successfully before declaring completion.
	return client.Shutdown(ctx).Err()
}
