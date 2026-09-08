package standalone

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const backupManifestFile = "manifest.json"

type BackupFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type BackupManifest struct {
	FormatVersion  int          `json:"format_version"`
	Version        string       `json:"version"`
	SourceCommit   string       `json:"source_commit"`
	CreatedAt      time.Time    `json:"created_at"`
	RecordingsDir  string       `json:"recordings_dir"`
	Files          []BackupFile `json:"files"`
	SQLiteFiles    []string     `json:"sqlite_source_files"`
	RedisManifests []string     `json:"redis_manifests"`
}

type backupCompleteMarker struct {
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

// BackupStopped holds both installation locks and the source SQLite handles
// through publication. It never checkpoints or otherwise writes the source DB.
func BackupStopped(ctx context.Context, paths Paths, destination string) (BackupManifest, error) {
	var result BackupManifest
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := paths.Validate(); err != nil {
		return result, err
	}
	destination, err := backupDestination(paths, destination)
	if err != nil {
		return result, err
	}
	lock, err := AcquireInstanceLock(paths.InstallDir)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	err = withConfigLock(paths.InstallDir, func() error {
		if _, err := os.Lstat(filepath.Join(paths.DataDir, runMarkerName)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("backup requires a clean stopped instance without a run marker")
		}
		release, err := LoadRelease(paths.InstallDir)
		if err != nil {
			return err
		}
		if err := backupComponentsStopped(release); err != nil {
			return err
		}
		if _, err := LoadConfig(paths); err != nil {
			return err
		}
		held := make(map[string]*os.File)
		defer func() {
			for _, f := range held {
				f.Close()
			}
		}()
		result = BackupManifest{FormatVersion: 1, Version: release.Version, SourceCommit: release.SourceCommit, CreatedAt: time.Now().UTC(), RecordingsDir: filepath.Clean(paths.RecordingsDir)}
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			path := paths.DatabasePath + suffix
			f, err := backupOpenSource(path, true)
			if suffix != "" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("backup cannot exclusively read SQLite file %s: %w", filepath.Base(path), err)
			}
			held[filepath.Clean(path)] = f
			result.SQLiteFiles = append(result.SQLiteFiles, "data/"+filepath.Base(path))
		}
		staging, err := os.MkdirTemp(filepath.Dir(destination), ".uvp-backup-")
		if err != nil {
			return err
		}
		published := false
		defer func() {
			if !published {
				os.RemoveAll(staging)
			}
		}()
		if err := protectConfigDir(staging, true); err != nil {
			return err
		}
		for _, root := range []struct{ source, target string }{{paths.ConfigDir, "config"}, {paths.DataDir, "data"}} {
			if err := backupCopyTree(ctx, root.source, filepath.Join(staging, root.target), held); err != nil {
				return err
			}
		}
		for _, path := range []string{filepath.Join(paths.InstallDir, "current.json"), filepath.Join(release.ReleaseDir, "manifest.json")} {
			rel, err := filepath.Rel(paths.InstallDir, path)
			if err != nil {
				return err
			}
			if err := backupCopyFile(ctx, path, filepath.Join(staging, "install", rel), nil); err != nil {
				return err
			}
		}
		result.RedisManifests, err = backupCheckRedis(staging)
		if err != nil {
			return err
		}
		if err := backupCheckSQLite(ctx, filepath.Join(staging, "data", "uvp.db")); err != nil {
			return err
		}
		result.Files, err = backupInventory(ctx, staging)
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err := writeSecureConfigFile(filepath.Join(staging, backupManifestFile), raw, false, nil); err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		marker, err := json.Marshal(backupCompleteMarker{ManifestPath: backupManifestFile, ManifestSHA256: hex.EncodeToString(sum[:])})
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Recheck immediately before rename; the platform operation also refuses
		// an existing target so a competing creator cannot be overwritten.
		if _, err := backupDestination(paths, destination); err != nil {
			return err
		}
		if err := backupPublish(staging, destination); err != nil {
			return err
		}
		published = true
		if err := ctx.Err(); err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(destination, "complete.json"), marker, false, nil)
	})
	if err != nil {
		return BackupManifest{}, err
	}
	return result, nil
}

func backupDestination(paths Paths, destination string) (string, error) {
	clean, err := cleanAbsolute(destination)
	if err != nil {
		return "", fmt.Errorf("backup destination: %w", err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("backup destination parent: %w", err)
	}
	clean = filepath.Join(parent, filepath.Base(clean))
	for _, source := range []string{paths.InstallDir, paths.DataDir, paths.ConfigDir, paths.RecordingsDir} {
		source, err = filepath.EvalSymlinks(source)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(source, clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return "", errors.New("backup destination must be outside installation, data and recordings")
		}
	}
	if _, err := ensureReleaseDirectory(filepath.Dir(clean)); err != nil {
		return "", fmt.Errorf("backup destination parent: %w", err)
	}
	if _, err := os.Lstat(clean); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("backup destination must not exist")
	}
	return clean, nil
}

func backupCopyTree(ctx context.Context, source, target string, held map[string]*os.File) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel != "." && !strings.ContainsRune(rel, filepath.Separator) {
			switch strings.ToLower(rel) {
			case "run", "locks", "tmp", "temp", "logs", ".uvp-running.json", ".uvp-instance.lock", ".uvp-config.lock":
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if _, err := ensureReleaseChild(source, path, entry.IsDir()); err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, rel), 0700)
		}
		return backupCopyFile(ctx, path, filepath.Join(target, rel), held[filepath.Clean(path)])
	})
}

func backupCopyFile(ctx context.Context, source, target string, held *os.File) error {
	input := held
	if input == nil {
		var err error
		input, err = backupOpenSource(source, false)
		if err != nil {
			return err
		}
		defer input.Close()
	}
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup source must be a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(output, hash), backupContextReader{ctx: ctx, reader: input})
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != info.Size() {
		return errors.New("backup source size changed during copy")
	}
	actual, err := releaseFileSHA256(target)
	if err != nil {
		return err
	}
	if actual != hex.EncodeToString(hash.Sum(nil)) {
		return errors.New("backup copy checksum mismatch")
	}
	return nil
}

type backupContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r backupContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func backupCheckSQLite(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	var integrity string
	err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity)
	closeErr := db.Close()
	if err != nil {
		return fmt.Errorf("backup SQLite integrity: %w", err)
	}
	if integrity != "ok" {
		return errors.New("backup SQLite integrity check failed")
	}
	return closeErr
}

func backupInventory(ctx context.Context, root string) ([]BackupFile, error) {
	var result []BackupFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		sum, err := releaseFileSHA256(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result = append(result, BackupFile{Path: filepath.ToSlash(rel), Size: info.Size(), SHA256: sum})
		return nil
	})
	return result, err
}

func backupCheckRedis(root string) ([]string, error) {
	var manifests []string
	redis := filepath.Join(root, "data", "redis")
	err := filepath.WalkDir(redis, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() != "manifest" && !strings.HasSuffix(entry.Name(), ".manifest") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 4<<20 {
			return errors.New("AOF manifest exceeds limit")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		members := make(map[string]bool)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) != 6 || parts[0] != "file" || parts[2] != "seq" || parts[4] != "type" {
				return errors.New("invalid AOF manifest entry")
			}
			name := parts[1]
			if name == "." || name == ".." || strings.ContainsAny(name, "/\\:") || members[name] {
				return errors.New("invalid AOF manifest member")
			}
			if _, err := strconv.ParseUint(parts[3], 10, 64); err != nil {
				return errors.New("invalid AOF manifest sequence")
			}
			if parts[5] != "b" && parts[5] != "i" && parts[5] != "h" {
				return errors.New("invalid AOF manifest type")
			}
			member, err := os.Stat(filepath.Join(filepath.Dir(path), name))
			if err != nil || !member.Mode().IsRegular() {
				return errors.New("AOF manifest member missing")
			}
			members[name] = true
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("AOF manifest: %w", err)
		}
		if len(members) == 0 {
			return errors.New("AOF manifest has no members")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		manifests = append(manifests, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(manifests) == 0 {
		return nil, errors.New("Redis AOF manifest missing")
	}
	return manifests, nil
}
