//go:build windows

package launcher

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// Opt-in: stops only the explicitly supplied disposable t18-setup fixture.
func TestWindowsBackupRunningInstallation(t *testing.T) {
	root := os.Getenv("UVP_BACKUP_TEST_ROOT")
	if root == "" {
		t.Skip("requires isolated backup fixture")
	}
	destination, recordings := os.Getenv("UVP_BACKUP_TEST_OUTPUT"), os.Getenv("UVP_BACKUP_TEST_RECORDINGS")
	if destination == "" || recordings == "" {
		t.Fatal("requires new output and actual recordings directory")
	}
	release, err := standalone.LoadRelease(root)
	if err != nil || release.Version != "t18-setup" {
		t.Fatal("requires verified isolated t18-setup release")
	}
	before := backupTestHashes(t, recordings)
	if len(before) == 0 {
		t.Fatal("requires existing recordings")
	}
	urls := make(chan string, 1)
	run := t18StartWithRecordings(t, root, recordings, urls)
	defer func() { run.cancel(); t18WaitFinished(t, run, 90*time.Second) }()
	if _, ok := t18WaitBrowserEntry(run, urls, 90*time.Second); !ok {
		t.Fatal("launcher did not start")
	}
	t19WaitBusinessReady(t, run)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	manifest, err := Backup(ctx, root, recordings, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !t18WaitFinished(t, run, 30*time.Second) {
		t.Fatal("backup returned before owner exited")
	}
	if _, err := os.Stat(filepath.Join(root, "data", ".uvp-running.json")); !os.IsNotExist(err) {
		t.Fatal("run marker remains")
	}
	if manifest.Version != release.Version || !strings.EqualFold(filepath.Clean(manifest.RecordingsDir), filepath.Clean(recordings)) {
		t.Fatal("backup source metadata mismatch")
	}
	if len(manifest.Files) < 8 {
		t.Fatal("backup unexpectedly incomplete")
	}
	for _, file := range manifest.Files {
		path := filepath.FromSlash(file.Path)
		if strings.HasSuffix(strings.ToLower(path), ".mp4") || strings.Contains(path, ".uvp-running") {
			t.Fatal("backup included video or runtime marker")
		}
		f, err := os.Open(filepath.Join(destination, path))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		n, copyErr := io.Copy(h, f)
		f.Close()
		if copyErr != nil || n != file.Size || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			t.Fatal("backup manifest hash mismatch")
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(destination, "data", "uvp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"sys_users", "gb_recording_file", "gb_recording_plan"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count == 0 {
			t.Fatalf("business table %s unavailable: %v", table, err)
		}
	}
	rows, err := db.Query("SELECT file_path, file_size FROM gb_recording_file WHERE file_size > 0")
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for rows.Next() {
		var path string
		var size int64
		if err := rows.Scan(&path, &size); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		path = filepath.Clean(filepath.FromSlash(path))
		rel, err := filepath.Rel(recordings, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Size() == size && before[rel] != "" {
			matched = true
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil || !matched {
		t.Fatal("backup recording index does not resolve to original external recording")
	}
	if !reflect.DeepEqual(before, backupTestHashes(t, recordings)) {
		t.Fatal("original recordings changed during backup")
	}
	t.Logf("running installation stopped; %d backup files verified; business records and %d original recordings retained", len(manifest.Files), len(before))
}

func backupTestHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
