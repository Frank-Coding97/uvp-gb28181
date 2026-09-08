package standalone

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestBackupStoppedCopiesAuthoritativeSetAndPublishesMarker(t *testing.T) {
	paths := newBackupTestPaths(t)
	recordingPath := filepath.Join(paths.RecordingsDir, "source.mp4")
	recording := []byte("recording fixture remains at its original root")
	require.NoError(t, os.WriteFile(recordingPath, recording, 0o600))

	destination := filepath.Join(filepath.Dir(paths.InstallDir), "backup-output")
	manifest, err := BackupStopped(context.Background(), paths, destination)
	if err != nil {
		t.Logf("backup destination=%s", destination)
		entries, readErr := os.ReadDir(destination)
		t.Logf("destination entries=%v readErr=%v", entries, readErr)
	}
	require.NoError(t, err)
	require.Equal(t, "t24.1", manifest.Version)
	require.Equal(t, "0123456789abcdef0123456789abcdef01234567", manifest.SourceCommit)
	require.Equal(t, filepath.Clean(paths.RecordingsDir), manifest.RecordingsDir)
	require.NotZero(t, manifest.CreatedAt)

	files := make(map[string]BackupFile, len(manifest.Files))
	for _, file := range manifest.Files {
		files[file.Path] = file
		require.NotContains(t, file.Path, "recordings")
		require.NotEmpty(t, file.SHA256)
	}
	for _, want := range []string{
		"config/config.yml",
		"config/redis.conf",
		"config/zlm.ini",
		"data/uvp.db",
		"data/uploads/upload.txt",
		"data/redis/appendonlydir/manifest",
		"data/redis/appendonlydir/appendonly.aof.1.base.rdb",
		"data/redis/appendonlydir/appendonly.aof.1.incr.aof",
		"install/current.json",
		"install/releases/t24.1/manifest.json",
	} {
		require.Contains(t, files, want)
	}

	markerRaw, err := os.ReadFile(filepath.Join(destination, "complete.json"))
	require.NoError(t, err)
	var marker backupCompleteMarker
	require.NoError(t, json.Unmarshal(markerRaw, &marker))
	require.Equal(t, backupManifestFile, marker.ManifestPath)
	manifestRaw, err := os.ReadFile(filepath.Join(destination, backupManifestFile))
	require.NoError(t, err)
	sum := sha256.Sum256(manifestRaw)
	require.Equal(t, hex.EncodeToString(sum[:]), marker.ManifestSHA256)

	gotRecording, err := os.ReadFile(recordingPath)
	require.NoError(t, err)
	require.Equal(t, recording, gotRecording)
}

func TestBackupStoppedRejectsRunningInstanceBeforeCreatingOutput(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()

	destination := filepath.Join(filepath.Dir(paths.InstallDir), "backup-running")
	_, err = BackupStopped(context.Background(), paths, destination)
	require.ErrorIs(t, err, ErrInstanceRunning)
	_, statErr := os.Stat(destination)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestBackupStoppedRejectsExistingDestinationWithoutChangingIt(t *testing.T) {
	paths := newBackupTestPaths(t)
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "backup-existing")
	require.NoError(t, os.Mkdir(destination, 0o700))
	marker := filepath.Join(destination, "complete.json")
	require.NoError(t, os.WriteFile(marker, []byte("keep"), 0o600))

	_, err := BackupStopped(context.Background(), paths, destination)
	require.Error(t, err)
	require.ErrorContains(t, err, "destination")
	got, readErr := os.ReadFile(marker)
	require.NoError(t, readErr)
	require.Equal(t, []byte("keep"), got)
}

func TestBackupStoppedRejectsMissingRedisManifestMemberWithoutMarker(t *testing.T) {
	paths := newBackupTestPaths(t)
	manifestPath := filepath.Join(paths.DataDir, "redis", "appendonlydir", "manifest")
	require.NoError(t, os.WriteFile(manifestPath, []byte("file missing.aof seq 1 type i\n"), 0o600))
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "backup-bad-redis")

	_, err := BackupStopped(context.Background(), paths, destination)
	require.Error(t, err)
	require.ErrorContains(t, err, "AOF manifest")
	_, statErr := os.Stat(destination)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestBackupStoppedRejectsRunMarkerBeforeCreatingOutput(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	_, err = BeginRun(paths)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
	marker := filepath.Join(paths.DataDir, runMarkerName)
	before, err := os.ReadFile(marker)
	require.NoError(t, err)
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "marker-backup")
	_, err = BackupStopped(context.Background(), paths, destination)
	require.ErrorContains(t, err, "run marker")
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
	raw, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, before, raw)
}

func TestBackupInvalidDestinationDoesNotRequestRunningOwnerStop(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()
	for _, destination := range []string{"relative", paths.DataDir, filepath.Join(paths.RecordingsDir, "backup")} {
		_, err := BackupStopped(context.Background(), paths, destination)
		require.Error(t, err)
		require.False(t, errors.Is(err, ErrInstanceRunning), "invalid destination must fail before launcher considers Stop")
	}
}

func TestBackupCanceledContextCreatesNoOutput(t *testing.T) {
	paths := newBackupTestPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "canceled-backup")
	_, err := BackupStopped(ctx, paths, destination)
	require.ErrorIs(t, err, context.Canceled)
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestBackupPublishDoesNotReplaceCompetingDestination(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "staging"), filepath.Join(root, "destination")
	require.NoError(t, os.Mkdir(source, 0700))
	require.NoError(t, os.Mkdir(target, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "private-data"), []byte("fixture"), 0600))
	require.Error(t, backupPublish(source, target))
	entries, err := os.ReadDir(target)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(filepath.Join(source, "private-data"))
	require.NoError(t, err)
}

type backupPausedCopyContext struct {
	context.Context
	started chan struct{}
	proceed chan struct{}
	calls   int
}

func (ctx *backupPausedCopyContext) Err() error {
	ctx.calls++
	if ctx.calls == 2 {
		close(ctx.started)
		<-ctx.proceed
	}
	return ctx.Context.Err()
}

func TestBackupHoldsInstanceOwnershipDuringCopy(t *testing.T) {
	paths := newBackupTestPaths(t)
	ctx := &backupPausedCopyContext{Context: context.Background(), started: make(chan struct{}), proceed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := BackupStopped(ctx, paths, filepath.Join(filepath.Dir(paths.InstallDir), "held-copy-backup"))
		done <- err
	}()
	select {
	case <-ctx.started:
	case err := <-done:
		t.Fatalf("backup exited before copy: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("backup did not reach copy")
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if lock != nil {
		lock.Close()
	}
	close(ctx.proceed)
	require.ErrorIs(t, err, ErrInstanceRunning)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("backup did not finish")
	}
}

func TestDebugBackupSQLiteURI(t *testing.T) {
	paths := newBackupTestPaths(t)
	db, err := sql.Open("sqlite", paths.DatabasePath)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	require.NoError(t, db.Close())
	for _, dsn := range []string{
		"file:" + filepath.ToSlash(paths.DatabasePath) + "?mode=ro",
		"file:" + filepath.ToSlash(paths.DatabasePath),
		((&url.URL{Scheme: "file", Path: filepath.ToSlash(paths.DatabasePath)}).String()) + "?mode=ro",
	} {
		ro, openErr := sql.Open("sqlite", dsn)
		pingErr := error(nil)
		if openErr == nil {
			pingErr = ro.Ping()
			_ = ro.Close()
		}
		t.Logf("dsn=%q open=%v ping=%v", dsn, openErr, pingErr)
	}
}

func newBackupTestPaths(t *testing.T) Paths {
	t.Helper()
	fixture := newTestReleaseFixture(t, "t24.1")
	for _, name := range []string{"config", "resource", "web", "data", "uploads", "recordings", "logs"} {
		require.NoError(t, os.MkdirAll(filepath.Join(fixture.installDir, name), 0o755))
	}
	paths, err := ResolvePaths(PathOptions{
		InstallDir:    fixture.installDir,
		ConfigDir:     filepath.Join(fixture.installDir, "config"),
		ResourceDir:   filepath.Join(fixture.installDir, "resource"),
		WebDir:        filepath.Join(fixture.installDir, "web"),
		DataDir:       filepath.Join(fixture.installDir, "data"),
		RecordingsDir: filepath.Join(fixture.installDir, "recordings"),
	})
	require.NoError(t, err)
	_, err = InitializeConfig(paths)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(paths.UploadDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(paths.UploadDir, "upload.txt"), []byte("upload"), 0o600))

	db, err := sql.Open("sqlite", paths.DatabasePath)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE backup_fixture (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO backup_fixture(value) VALUES ('complete');")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	redisDir := filepath.Join(paths.DataDir, "redis", "appendonlydir")
	require.NoError(t, os.MkdirAll(redisDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(redisDir, "appendonly.aof.1.base.rdb"), []byte("base"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(redisDir, "appendonly.aof.1.incr.aof"), []byte("incr"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(redisDir, "manifest"), []byte(strings.Join([]string{
		"file appendonly.aof.1.base.rdb seq 1 type b",
		"file appendonly.aof.1.incr.aof seq 1 type i",
		"",
	}, "\n")), 0o600))
	return paths
}
