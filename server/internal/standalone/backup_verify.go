package standalone

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerifyBackup is read-only: it checks a completed snapshot, not permission to
// restore it. Recovery must separately revoke historical credentials and gate
// external business traffic before using any of these files.
func VerifyBackup(ctx context.Context, root string) (BackupManifest, error) {
	var manifest BackupManifest
	if err := ctx.Err(); err != nil {
		return manifest, err
	}
	root, err := cleanAbsolute(root)
	if err != nil {
		return manifest, err
	}
	if err := protectConfigDir(root, false); err != nil {
		return manifest, err
	}
	readMetadata := func(name string) ([]byte, error) {
		path := filepath.Join(root, name)
		info, err := ensureReleaseChild(root, path, false)
		if err != nil {
			return nil, err
		}
		if info.Size() > 8<<20 {
			return nil, errors.New("backup metadata exceeds limit")
		}
		return readSecureConfigFile(path)
	}
	markerRaw, err := readMetadata("complete.json")
	if err != nil {
		return manifest, err
	}
	var marker backupCompleteMarker
	if err := decodeBackupObject(markerRaw, &marker, "manifest_path", "manifest_sha256"); err != nil {
		return manifest, err
	}
	if marker.ManifestPath != backupManifestFile {
		return manifest, errors.New("invalid backup completion marker")
	}
	raw, err := readMetadata(backupManifestFile)
	if err != nil {
		return manifest, err
	}
	sum := sha256.Sum256(raw)
	if marker.ManifestSHA256 != hex.EncodeToString(sum[:]) {
		return manifest, errors.New("backup manifest checksum mismatch")
	}
	fields, err := decodeStrictJSONObject(raw)
	if err != nil {
		return manifest, err
	}
	var formatVersion int
	if formatRaw, ok := fields["format_version"]; !ok {
		return manifest, errors.New("backup format version is missing")
	} else if err := decodeReleaseJSON(formatRaw, &formatVersion); err != nil {
		return manifest, err
	}
	baseKeys := []string{"format_version", "version", "source_commit", "created_at", "recordings_dir", "files", "sqlite_source_files", "redis_manifests"}
	manifestKeys := baseKeys
	if formatVersion == 2 {
		manifestKeys = append(append([]string{}, baseKeys...), "kind", "operation_id", "run_marker_sha256", "source_current_sha256")
	} else if formatVersion != 1 {
		return manifest, errors.New("unsupported backup format version")
	}
	if err := decodeBackupObject(raw, &manifest, manifestKeys...); err != nil {
		return manifest, err
	}
	if manifest.FormatVersion != formatVersion || !validReleaseVersion(manifest.Version) || manifest.CreatedAt.IsZero() || strings.TrimSpace(manifest.SourceCommit) == "" {
		return manifest, errors.New("invalid backup metadata")
	}
	if manifest.FormatVersion == 2 {
		if manifest.Kind != "unclean_snapshot" || !validMaintenancePermitHex(manifest.OperationID) ||
			!validMaintenancePermitHex(manifest.RunMarkerSHA256) || !validMaintenancePermitHex(manifest.SourceCurrentSHA256) {
			return manifest, errors.New("invalid unclean snapshot metadata")
		}
	}
	if _, err := cleanAbsolute(manifest.RecordingsDir); err != nil {
		return manifest, errors.New("invalid backup recordings reference")
	}
	sqliteFiles := make(map[string]bool)
	for _, path := range manifest.SQLiteFiles {
		if sqliteFiles[path] {
			return manifest, errors.New("duplicate SQLite source reference")
		}
		switch path {
		case "data/uvp.db", "data/uvp.db-wal", "data/uvp.db-shm", "data/uvp.db-journal":
		default:
			return manifest, errors.New("invalid SQLite source reference")
		}
		sqliteFiles[path] = true
	}
	if !sqliteFiles["data/uvp.db"] {
		return manifest, errors.New("SQLite source reference missing")
	}
	var rawFiles []json.RawMessage
	if err := json.Unmarshal(fields["files"], &rawFiles); err != nil {
		return manifest, err
	}
	expected := make(map[string]BackupFile)
	releaseReference := "install/releases/" + manifest.Version + "/manifest.json"
	for i, file := range manifest.Files {
		var checked BackupFile
		if err := decodeBackupObject(rawFiles[i], &checked, "path", "size", "sha256"); err != nil {
			return manifest, err
		}
		path, err := validateReleaseManifestPath(file.Path)
		if err != nil {
			return manifest, errors.New("invalid backup file path")
		}
		if !strings.HasPrefix(path, "config/") && !strings.HasPrefix(path, "data/") && path != "install/current.json" && path != releaseReference {
			return manifest, errors.New("unexpected backup file scope")
		}
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || len(digest) != sha256.Size || file.Size < 0 {
			return manifest, errors.New("invalid backup file metadata")
		}
		key := strings.ToLower(path)
		if _, exists := expected[key]; exists {
			return manifest, errors.New("duplicate backup file path")
		}
		expected[key] = file
	}
	for _, required := range []string{"config/config.yml", "config/redis.conf", "config/zlm.ini", "data/uvp.db", "install/current.json", releaseReference} {
		if _, exists := expected[strings.ToLower(required)]; !exists {
			return manifest, errors.New("backup is missing required data")
		}
	}
	seen := make(map[string]bool)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := ensureReleaseChild(root, path, entry.IsDir()); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "complete.json" || rel == backupManifestFile {
			return nil
		}
		key := strings.ToLower(rel)
		file, exists := expected[key]
		if !exists || seen[key] || file.Path != rel {
			return errors.New("backup contains unlisted or aliased file")
		}
		seen[key] = true
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() != file.Size {
			return errors.New("backup file size mismatch")
		}
		actual, err := releaseFileSHA256(path)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actual, file.SHA256) {
			return errors.New("backup file checksum mismatch")
		}
		return nil
	})
	if err != nil {
		return manifest, err
	}
	if len(seen) != len(expected) {
		return manifest, errors.New("backup file missing")
	}
	current, err := os.ReadFile(filepath.Join(root, "install", "current.json"))
	if err != nil {
		return manifest, err
	}
	version, err := parseReleaseCurrent(current)
	if err != nil || version != manifest.Version {
		return manifest, errors.New("backup version reference mismatch")
	}
	if manifest.FormatVersion == 2 {
		currentSum := sha256.Sum256(current)
		if manifest.SourceCurrentSHA256 != hex.EncodeToString(currentSum[:]) {
			return manifest, errors.New("unclean snapshot current pointer mismatch")
		}
	}
	reference, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(releaseReference)))
	if err != nil {
		return manifest, err
	}
	release, err := decodeReleaseManifest(reference)
	if err != nil {
		return manifest, err
	}
	// Historical producer compatibility is checked by the upgrade plan, not by
	// requiring an older program to understand the verifier's current schema.
	if release.FormatVersion != releaseManifestFormatVersion || release.Version != manifest.Version || release.SchemaMin < 1 || release.SchemaMax < release.SchemaMin || len(release.Files) == 0 {
		return manifest, errors.New("invalid historical release reference")
	}
	if release.SourceCommit != manifest.SourceCommit {
		return manifest, errors.New("backup source revision mismatch")
	}
	redisManifests, err := backupCheckRedis(root)
	if err != nil {
		return manifest, err
	}
	if strings.Join(redisManifests, "\n") != strings.Join(manifest.RedisManifests, "\n") {
		return manifest, errors.New("backup Redis manifest references mismatch")
	}
	return manifest, nil
}

func decodeBackupObject(raw []byte, destination any, keys ...string) error {
	fields, err := decodeStrictJSONObject(raw)
	if err != nil {
		return err
	}
	if len(fields) != len(keys) {
		return errors.New("incomplete backup object")
	}
	if err := requireExactJSONKeys(fields, keys...); err != nil {
		return fmt.Errorf("backup object fields: %w", err)
	}
	return decodeReleaseJSON(raw, destination)
}
