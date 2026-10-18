package upgrade

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/glebarez/go-sqlite"
	migrationsfs "uvplatform.com/uvp-gb28181/resource/database/gb28181"
)

// AvailableMigrationFiles exposes the embedded dialect-specific migration set
// for the read-only upgrade preview. It never executes SQL.
func AvailableMigrationFiles(dialect string) ([]string, error) {
	dialect = strings.ToLower(strings.TrimSpace(dialect))
	if dialect == "postgres" {
		dialect = "postgresql"
	}
	entries, err := migrationsfs.FS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, "-down.sql") {
			continue
		}
		matches := (dialect == "postgresql" && strings.HasSuffix(name, "-postgresql.sql")) ||
			(dialect == "sqlserver" && strings.HasSuffix(name, "-sqlserver.sql")) ||
			(dialect == "sqlite" && strings.HasSuffix(name, "-sqlite.sql")) ||
			(dialect == "mysql" && !strings.Contains(name, "-postgresql") && !strings.Contains(name, "-sqlserver") && !strings.Contains(name, "-sqlite"))
		if matches {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// MigrationPlan is a read-only description. The upgrade executor never turns
// these fields into SQL; the application migration package remains the only
// owner of migration execution.
type MigrationPlan struct {
	Dialect     string   `json:"dialect"`
	Applied     []string `json:"applied"`
	Pending     []string `json:"pending"`
	Unknown     []string `json:"unknown"`
	Destructive []string `json:"destructive"`
	CanApply    bool     `json:"can_apply"`
	BlockReason string   `json:"block_reason,omitempty"`
}

func BuildMigrationPlan(dialect string, applied, available, destructive []string) MigrationPlan {
	plan := MigrationPlan{Dialect: strings.TrimSpace(dialect), Applied: append([]string(nil), applied...)}
	seenApplied := make(map[string]struct{}, len(applied))
	for _, name := range applied {
		seenApplied[name] = struct{}{}
	}
	seenAvailable := make(map[string]struct{}, len(available))
	for _, name := range available {
		seenAvailable[name] = struct{}{}
		if _, ok := seenApplied[name]; !ok {
			plan.Pending = append(plan.Pending, name)
		}
	}
	for _, name := range applied {
		if _, ok := seenAvailable[name]; !ok {
			plan.Unknown = append(plan.Unknown, name)
		}
	}
	plan.Destructive = append([]string(nil), destructive...)
	plan.CanApply = plan.Dialect != "" && len(plan.Unknown) == 0 && len(plan.Destructive) == 0
	if !plan.CanApply {
		switch {
		case plan.Dialect == "":
			plan.BlockReason = "database dialect is unknown"
		case len(plan.Unknown) > 0:
			plan.BlockReason = "database contains unknown migration records"
		default:
			plan.BlockReason = "target contains destructive migrations"
		}
	}
	return plan
}

// SQLiteBackup creates a consistent snapshot while all writers are stopped.
// It deliberately uses SQLite's backup operation instead of copying the main
// file, which would omit committed WAL pages. Both paths are administrator
// controlled and must be absolute; the destination must not already exist.
func SQLiteBackup(ctx context.Context, source, destination string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(destination) {
		return errors.New("sqlite backup paths must be absolute")
	}
	if source == destination {
		return errors.New("sqlite backup destination must differ from source")
	}
	if info, err := os.Stat(source); err != nil {
		return fmt.Errorf("stat sqlite database: %w", err)
	} else if !info.Mode().IsRegular() {
		return errors.New("sqlite source is not a regular file")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("sqlite backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", source+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	// VACUUM INTO accepts a bound string in SQLite and safely copies WAL data.
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create sqlite consistent backup: %w", err)
	}
	if err := checkSQLiteIntegrity(ctx, destination); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("check sqlite backup integrity: %w", err)
	}
	return nil
}

// SQLiteRestore replaces a stopped installation's database with a verified
// SQLite backup. It never copies a live WAL file: a fresh temporary database
// is produced and checked before the main file is atomically replaced.
func SQLiteRestore(ctx context.Context, source, destination string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !filepath.IsAbs(source) || !filepath.IsAbs(destination) {
		return errors.New("sqlite restore paths must be absolute")
	}
	if filepath.Clean(source) == filepath.Clean(destination) {
		return errors.New("sqlite restore source must differ from destination")
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("stat sqlite restore source: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return errors.New("sqlite restore source is not a regular file")
	}
	parent := filepath.Dir(destination)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("stat sqlite restore directory: %w", err)
	}
	if !parentInfo.IsDir() {
		return errors.New("sqlite restore destination parent is not a directory")
	}
	destinationInfo, err := os.Lstat(destination)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat sqlite restore destination: %w", err)
	}
	if err == nil && !destinationInfo.Mode().IsRegular() {
		return errors.New("sqlite restore destination is not a regular file")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		info, err := os.Lstat(destination + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat sqlite restore sidecar %q: %w", suffix, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("sqlite restore sidecar %q is not a regular file", suffix)
		}
	}

	staging, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+".restore-")
	if err != nil {
		return fmt.Errorf("create sqlite restore staging: %w", err)
	}
	defer os.RemoveAll(staging)
	staged := filepath.Join(staging, "snapshot.db")
	if err := SQLiteBackup(ctx, source, staged); err != nil {
		return fmt.Errorf("prepare sqlite restore snapshot: %w", err)
	}
	mode := sourceInfo.Mode().Perm()
	if destinationInfo != nil {
		mode = destinationInfo.Mode().Perm()
	}
	if mode == 0 {
		mode = 0600
	}
	if err := os.Chmod(staged, mode); err != nil {
		return fmt.Errorf("protect sqlite restore snapshot: %w", err)
	}
	if err := replaceFileAtomically(staged, destination, mode); err != nil {
		return fmt.Errorf("install sqlite restore snapshot: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(destination + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale sqlite sidecar %q: %w", suffix, err)
		}
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync sqlite restore directory: %w", err)
	}
	if err := checkSQLiteIntegrity(ctx, destination); err != nil {
		return fmt.Errorf("check restored sqlite integrity: %w", err)
	}
	return nil
}

func checkSQLiteIntegrity(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("integrity check failed: %s", result)
	}
	return nil
}
