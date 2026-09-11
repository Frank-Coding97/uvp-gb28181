package candidatehealth

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestCandidateHealthChecksWithoutMigratingOrChangingPolicies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"config", "data", "resource", "web", "recordings"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, name), 0700))
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), ResourceDir: filepath.Join(root, "resource"), WebDir: filepath.Join(root, "web")})
	require.NoError(t, err)
	_, err = standalone.InitializeConfig(paths)
	require.NoError(t, err)
	cfg := ymlconfig.CreateYamlFactoryFromFile(paths.ConfigFile)
	db, err := gormhelper.NewSQLiteClient(paths.DatabasePath)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	defer raw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	client := healthTestRedis(t)
	defer client.Close()
	require.ErrorContains(t, Check(ctx, db, client, cfg), "schema")
	require.NoError(t, sqlitebootstrap.Migrate(ctx, db))
	var before []map[string]any
	require.NoError(t, db.Table("gb_schema_migrations").Order("version").Find(&before).Error)
	require.NoError(t, Check(ctx, db, client, cfg))
	var after []map[string]any
	require.NoError(t, db.Table("gb_schema_migrations").Order("version").Find(&after).Error)
	require.Equal(t, before, after)
	require.EqualValues(t, 0, client.DBSize(ctx).Val())
	cfg.Set("casbin.modelconfig", "invalid model")
	require.ErrorContains(t, Check(ctx, db, client, cfg), "policy")
	require.NoError(t, client.Close())
	require.ErrorContains(t, Check(ctx, db, client, cfg), "redis")
}

func healthTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary := os.Getenv("UVP_RECOVERY_REDIS_BINARY")
	if binary == "" {
		binary, _ = exec.LookPath("redis-server")
	}
	if binary == "" {
		t.Skip("requires isolated Redis test binary")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	dir := t.TempDir()
	conf := "bind 127.0.0.1\nport " + strconv.Itoa(port) + "\nprotected-mode yes\ndaemonize no\nsave \"\"\nappendonly no\ndir .\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "redis.conf"), []byte(conf), 0600))
	cmd := exec.Command(binary, "redis.conf")
	cmd.Dir = dir
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))})
	deadline := time.Now().Add(5 * time.Second)
	for client.Ping(context.Background()).Err() != nil {
		if time.Now().After(deadline) {
			t.Fatal("test Redis did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return client
}
