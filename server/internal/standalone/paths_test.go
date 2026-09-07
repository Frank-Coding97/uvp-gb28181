package standalone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePathsUsesExplicitRootsAndAllowsExternalRecordings(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "UVP 中文 包")
	recordingsDir := filepath.Join(t.TempDir(), "录像 盘")
	for _, dir := range []string{
		installDir,
		filepath.Join(installDir, "config 中文"),
		filepath.Join(installDir, "resource assets"),
		filepath.Join(installDir, "web dist"),
		filepath.Join(installDir, "data 数据"),
		recordingsDir,
	} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}

	paths, err := ResolvePaths(PathOptions{
		InstallDir:    installDir,
		ConfigDir:     filepath.Join(installDir, "config 中文"),
		ResourceDir:   filepath.Join(installDir, "resource assets"),
		WebDir:        filepath.Join(installDir, "web dist"),
		DataDir:       filepath.Join(installDir, "data 数据"),
		RecordingsDir: recordingsDir,
	})

	require.NoError(t, err)
	require.True(t, paths.Explicit)
	require.Equal(t, installDir, paths.InstallDir)
	require.Equal(t, filepath.Join(installDir, "config 中文", "config.yml"), paths.ConfigFile)
	require.Equal(t, filepath.Join(installDir, "data 数据", "uvp.db"), paths.DatabasePath)
	require.Equal(t, filepath.Join(installDir, "data 数据", "uploads"), paths.UploadDir)
	require.Equal(t, filepath.Join(installDir, "logs"), paths.LogsDir)
	require.Equal(t, filepath.Join(installDir, "logs", "scheduler"), paths.SchedulerLogDir)
	require.Equal(t, recordingsDir, paths.RecordingsDir)
}

func TestResolvePathsRejectsRelativeUNCAndEscapedRoots(t *testing.T) {
	installDir := t.TempDir()
	valid := PathOptions{
		InstallDir:    installDir,
		ConfigDir:     filepath.Join(installDir, "config"),
		ResourceDir:   filepath.Join(installDir, "resource"),
		WebDir:        filepath.Join(installDir, "web"),
		DataDir:       filepath.Join(installDir, "data"),
		RecordingsDir: filepath.Join(installDir, "recordings"),
	}

	tests := []struct {
		name string
		edit func(*PathOptions)
		want error
	}{
		{name: "relative", edit: func(options *PathOptions) { options.DataDir = "data" }, want: ErrPathNotAbsolute},
		{name: "drive-relative rooted", edit: func(options *PathOptions) { options.DataDir = `\data` }, want: ErrPathNotAbsolute},
		{name: "unc", edit: func(options *PathOptions) { options.DataDir = `\\server\share\data` }, want: ErrUNCPath},
		{name: "escaped config", edit: func(options *PathOptions) { options.ConfigDir = filepath.Join(installDir, "..", "outside") }, want: ErrPathOutsideInstall},
		{name: "escaped web", edit: func(options *PathOptions) { options.WebDir = filepath.Join(installDir, "..", "outside") }, want: ErrPathOutsideInstall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := valid
			tt.edit(&options)
			_, err := ResolvePaths(options)
			require.Error(t, err)
			require.True(t, errors.Is(err, tt.want), "error = %v", err)
		})
	}
}

func TestResolveStartupPathsRequiresExplicitCompleteSetAndNeverFallsBackToCWD(t *testing.T) {
	installDir := t.TempDir()
	args := []string{
		"-uvp-install-dir", installDir,
		"-uvp-config-dir", filepath.Join(installDir, "config"),
	}

	paths, err := ResolveStartupPaths(args, func(string) string { return "" })

	require.Error(t, err)
	require.ErrorIs(t, err, ErrExplicitPathRequired)
	require.Empty(t, paths.InstallDir)
}

func TestResolveStartupPathsUsesArgumentsOverEnvironment(t *testing.T) {
	installDir := t.TempDir()
	args := []string{
		"-uvp-install-dir", installDir,
		"-uvp-config-dir", filepath.Join(installDir, "config"),
		"-uvp-resource-dir", filepath.Join(installDir, "resource"),
		"-uvp-web-dir", filepath.Join(installDir, "web"),
		"-uvp-data-dir", filepath.Join(installDir, "data"),
		"-uvp-recordings-dir", filepath.Join(installDir, "recordings-from-args"),
	}
	env := map[string]string{
		EnvRecordingsDir: filepath.Join(installDir, "recordings-from-env"),
	}

	paths, err := ResolveStartupPaths(args, func(key string) string { return env[key] })

	require.NoError(t, err)
	require.Equal(t, filepath.Join(installDir, "recordings-from-args"), paths.RecordingsDir)
}

func TestResolveStartupPathsWithoutInputsKeepsLegacyMode(t *testing.T) {
	paths, err := ResolveStartupPaths(nil, func(string) string { return "" })

	require.NoError(t, err)
	require.False(t, paths.Explicit)
	require.Empty(t, paths.InstallDir)
	require.Empty(t, paths.DatabasePath)
}

func TestPathsValidateRejectsReadOnlyDataDirectory(t *testing.T) {
	installDir := t.TempDir()
	options := testPathOptions(installDir)
	for _, dir := range []string{options.ConfigDir, options.ResourceDir, options.WebDir, options.DataDir, options.RecordingsDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	require.NoError(t, os.Chmod(options.DataDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(options.DataDir, 0o755) })

	paths, err := ResolvePaths(options)
	require.NoError(t, err)
	require.ErrorIs(t, paths.Validate(), ErrPathNotWritable)
}

func TestPathsValidateRejectsDirectorySymlinkEscape(t *testing.T) {
	installDir := t.TempDir()
	options := testPathOptions(installDir)
	outside := filepath.Join(t.TempDir(), "outside-config")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	for _, dir := range []string{options.ResourceDir, options.WebDir, options.DataDir, options.RecordingsDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	if err := os.Symlink(outside, options.ConfigDir); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	paths, err := ResolvePaths(options)
	require.NoError(t, err)
	require.ErrorIs(t, paths.Validate(), ErrPathOutsideInstall)
}

func TestPathsValidateRejectsDatabaseSymlink(t *testing.T) {
	installDir := t.TempDir()
	options := testPathOptions(installDir)
	for _, dir := range []string{options.ConfigDir, options.ResourceDir, options.WebDir, options.DataDir, options.RecordingsDir} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	outside := filepath.Join(t.TempDir(), "outside.db")
	require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
	if err := os.Symlink(outside, filepath.Join(options.DataDir, "uvp.db")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	paths, err := ResolvePaths(options)
	require.NoError(t, err)
	require.ErrorIs(t, paths.Validate(), ErrPathSymlink)
}

func testPathOptions(installDir string) PathOptions {
	return PathOptions{
		InstallDir:    installDir,
		ConfigDir:     filepath.Join(installDir, "config"),
		ResourceDir:   filepath.Join(installDir, "resource"),
		WebDir:        filepath.Join(installDir, "web"),
		DataDir:       filepath.Join(installDir, "data"),
		RecordingsDir: filepath.Join(installDir, "recordings"),
	}
}
