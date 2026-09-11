package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyBackupAcceptsCompleteSnapshotWithoutChangingFiles(t *testing.T) {
	root, want := newVerifyBackup(t)
	before, err := backupInventory(context.Background(), root)
	require.NoError(t, err)
	got, err := VerifyBackup(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, want, got)
	after, err := backupInventory(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestVerifyBackupRejectsChangedMissingAndUnlistedData(t *testing.T) {
	for _, kind := range []string{"changed", "missing", "unlisted", "marker"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := newVerifyBackup(t)
			switch kind {
			case "changed":
				require.NoError(t, os.WriteFile(filepath.Join(root, "data", "uvp.db"), []byte("broken"), 0600))
			case "missing":
				require.NoError(t, os.Remove(filepath.Join(root, "config", "redis.conf")))
			case "unlisted":
				require.NoError(t, os.WriteFile(filepath.Join(root, "data", "unknown"), []byte("extra"), 0600))
			case "marker":
				require.NoError(t, os.Remove(filepath.Join(root, "complete.json")))
			}
			_, err := VerifyBackup(context.Background(), root)
			require.Error(t, err)
		})
	}
}

func TestVerifyBackupRejectsUnsafeManifestEvenWithMatchingMarkerHash(t *testing.T) {
	for _, kind := range []string{"traversal", "duplicate", "missing-database", "wrong-version", "sqlite-reference"} {
		t.Run(kind, func(t *testing.T) {
			root, manifest := newVerifyBackup(t)
			switch kind {
			case "traversal":
				manifest.Files[0].Path = "../outside"
			case "duplicate":
				manifest.Files = append(manifest.Files, manifest.Files[0])
			case "missing-database":
				for i, file := range manifest.Files {
					if file.Path == "data/uvp.db" {
						manifest.Files = append(manifest.Files[:i], manifest.Files[i+1:]...)
						break
					}
				}
			case "wrong-version":
				manifest.Version = "wrong-version"
			case "sqlite-reference":
				manifest.SQLiteFiles = append(manifest.SQLiteFiles, "../outside")
			}
			raw, err := json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, writeSecureConfigFile(filepath.Join(root, backupManifestFile), raw, true, nil))
			sum := sha256.Sum256(raw)
			marker, err := json.Marshal(backupCompleteMarker{ManifestPath: backupManifestFile, ManifestSHA256: hex.EncodeToString(sum[:])})
			require.NoError(t, err)
			require.NoError(t, writeSecureConfigFile(filepath.Join(root, "complete.json"), marker, true, nil))
			_, err = VerifyBackup(context.Background(), root)
			require.Error(t, err)
		})
	}
}

func TestVerifyBackupExistingNativeSnapshot(t *testing.T) {
	root := os.Getenv("UVP_BACKUP_VERIFY_ROOT")
	if root == "" {
		t.Skip("requires explicitly supplied native backup")
	}
	before, err := backupInventory(context.Background(), root)
	require.NoError(t, err)
	manifest, err := VerifyBackup(context.Background(), root)
	require.NoError(t, err)
	after, err := backupInventory(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, before, after)
	t.Logf("verified %d files from %s without modifying snapshot", len(manifest.Files), manifest.Version)
}

func newVerifyBackup(t *testing.T) (string, BackupManifest) {
	t.Helper()
	paths := newBackupTestPaths(t)
	root := filepath.Join(filepath.Dir(paths.InstallDir), "verified-backup")
	manifest, err := BackupStopped(context.Background(), paths, root)
	require.NoError(t, err)
	return root, manifest
}

func TestVerifyBackupDoesNotRequireHistoricalReleaseToSupportCurrentSchema(t *testing.T) {
	root, manifest := newVerifyBackup(t)
	refPath := "install/releases/" + manifest.Version + "/manifest.json"
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(refPath)))
	require.NoError(t, err)
	ref, err := decodeReleaseManifest(raw)
	require.NoError(t, err)
	ref.SchemaMax = 2
	raw, err = json.Marshal(ref)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(refPath)), raw, 0600))
	sum := sha256.Sum256(raw)
	for i := range manifest.Files {
		if manifest.Files[i].Path == refPath {
			manifest.Files[i].Size = int64(len(raw))
			manifest.Files[i].SHA256 = hex.EncodeToString(sum[:])
		}
	}
	raw, err = json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(root, backupManifestFile), raw, true, nil))
	sum = sha256.Sum256(raw)
	marker, err := json.Marshal(backupCompleteMarker{ManifestPath: backupManifestFile, ManifestSHA256: hex.EncodeToString(sum[:])})
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(root, "complete.json"), marker, true, nil))
	_, err = VerifyBackup(context.Background(), root)
	require.NoError(t, err, "snapshot integrity is separate from upgrade schema compatibility")
}
