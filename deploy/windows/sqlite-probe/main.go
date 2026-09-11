// sqlite-probe exercises the pure-Go SQLite runtime used by the Windows
// standalone package. It is intentionally independent from the server module.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	minimumSQLiteVersion = "3.51.3"
	busyTimeoutMS        = 250
	writeWorkers         = 2
	rowsPerWorker        = 32
	lockContextTimeout   = 80 * time.Millisecond
	lockWaitLimit        = 2 * time.Second
)

type caseResult struct {
	Passed     bool           `json:"passed"`
	DurationMS int64          `json:"duration_ms"`
	Details    map[string]any `json:"details,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type probeReport struct {
	Schema             int                   `json:"schema"`
	Probe              string                `json:"probe"`
	GOOS               string                `json:"goos"`
	GOARCH             string                `json:"goarch"`
	PureGo             bool                  `json:"pure_go"`
	DatabasePath       string                `json:"database_path"`
	SQLiteVersion      string                `json:"sqlite_version"`
	SQLiteSourceID     string                `json:"sqlite_source_id"`
	JournalMode        string                `json:"journal_mode"`
	Synchronous        int                   `json:"synchronous"`
	ForeignKeys        int                   `json:"foreign_keys"`
	BusyTimeoutMS      int                   `json:"busy_timeout_ms"`
	MaxOpenConnections int                   `json:"max_open_connections"`
	MaxIdleConnections int                   `json:"max_idle_connections"`
	OpenConnections    int                   `json:"open_connections"`
	IdleConnections    int                   `json:"idle_connections"`
	Cases              map[string]caseResult `json:"cases"`
	Passed             bool                  `json:"passed"`
	ExitCode           int                   `json:"exit_code"`
	Error              string                `json:"error,omitempty"`
}

func main() {
	pathFlag := flag.String("path", "", "optional SQLite file path; the default uses a temporary Chinese/space path")
	flag.Parse()

	var (
		report *probeReport
		err    error
	)
	if *pathFlag == "" {
		report, err = runProbe()
	} else {
		report, err = runProbeAt(*pathFlag)
	}
	if err != nil {
		if report == nil {
			report = newReport("")
		}
		report.Passed = false
		report.ExitCode = 1
		report.Error = err.Error()
	}

	if report.Passed {
		report.ExitCode = 0
	} else {
		report.ExitCode = 1
	}
	encoded, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "sqlite probe report: %v\n", marshalErr)
		os.Exit(1)
	}
	fmt.Println(string(encoded))
	if report.ExitCode != 0 {
		os.Exit(report.ExitCode)
	}
}

func runProbe() (*probeReport, error) {
	dir, err := os.MkdirTemp("", "uvp sqlite probe 中文 ")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)

	return runProbeAt(filepath.Join(dir, "probe data.db"))
}

func runProbeAt(path string) (*probeReport, error) {
	report := newReport(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return report, fmt.Errorf("probe database path already exists: %w", err)
	}
	if err != nil {
		return report, fmt.Errorf("create probe database: %w", err)
	}
	if err := file.Close(); err != nil {
		return report, err
	}
	db, err := openSQLite(path)
	if err != nil {
		return report, err
	}
	defer db.Close()

	if err := ping(db); err != nil {
		return report, fmt.Errorf("ping SQLite: %w", err)
	}
	if err := fillRuntimeDetails(report, db); err != nil {
		return report, err
	}

	report.Cases["runtime_configuration"] = executeCase(func() (map[string]any, error) {
		return validateRuntime(report)
	})
	report.Cases["crud_transactions"] = executeCase(func() (map[string]any, error) {
		return runCRUDTransactions(db)
	})

	if err := db.Close(); err != nil {
		return report, fmt.Errorf("close SQLite before reopen: %w", err)
	}
	db, err = openSQLite(path)
	if err != nil {
		return report, fmt.Errorf("reopen SQLite: %w", err)
	}
	defer db.Close()
	if err := ping(db); err != nil {
		return report, fmt.Errorf("ping reopened SQLite: %w", err)
	}
	report.Cases["reopen_integrity"] = executeCase(func() (map[string]any, error) {
		return runReopenIntegrity(db)
	})
	report.Cases["concurrent_writes"] = executeCase(func() (map[string]any, error) {
		return runConcurrentWrites(db)
	})
	report.Cases["bounded_context_wait"] = executeCase(func() (map[string]any, error) {
		return runBoundedContextWait(path, db)
	})

	report.Passed = true
	for name, result := range report.Cases {
		if !result.Passed {
			report.Passed = false
			if report.Error == "" {
				report.Error = fmt.Sprintf("case %s failed", name)
			}
		}
	}
	report.ExitCode = 0
	if !report.Passed {
		report.ExitCode = 1
	}
	return report, nil
}

func newReport(path string) *probeReport {
	return &probeReport{
		Schema:             1,
		Probe:              "windows-sqlite-p0",
		GOOS:               runtime.GOOS,
		GOARCH:             runtime.GOARCH,
		PureGo:             true,
		DatabasePath:       path,
		MaxOpenConnections: 1,
		MaxIdleConnections: 1,
		Cases:              make(map[string]caseResult),
		ExitCode:           1,
	}
}

func openSQLite(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("SQLite path is empty")
	}
	dsn := path + "?_busy_timeout=" + strconv.Itoa(busyTimeoutMS) +
		"&_foreign_keys=on&_journal_mode=wal&_synchronous=full"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func ping(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

func fillRuntimeDetails(report *probeReport, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version(), sqlite_source_id()").Scan(&report.SQLiteVersion, &report.SQLiteSourceID); err != nil {
		return fmt.Errorf("read SQLite runtime version: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&report.JournalMode); err != nil {
		return fmt.Errorf("read journal_mode: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&report.Synchronous); err != nil {
		return fmt.Errorf("read synchronous: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&report.ForeignKeys); err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&report.BusyTimeoutMS); err != nil {
		return fmt.Errorf("read busy_timeout: %w", err)
	}
	stats := db.Stats()
	report.OpenConnections = stats.OpenConnections
	report.IdleConnections = stats.Idle
	return nil
}

func validateRuntime(report *probeReport) (map[string]any, error) {
	details := map[string]any{
		"minimum_sqlite_version": minimumSQLiteVersion,
		"single_connection":      report.MaxOpenConnections == 1 && report.MaxIdleConnections == 1,
		"sqlite_version_ok":      versionAtLeast(report.SQLiteVersion, minimumSQLiteVersion),
		"journal_mode":           report.JournalMode,
		"synchronous":            report.Synchronous,
		"foreign_keys":           report.ForeignKeys,
		"busy_timeout_ms":        report.BusyTimeoutMS,
	}
	if !versionAtLeast(report.SQLiteVersion, minimumSQLiteVersion) {
		return details, fmt.Errorf("SQLite runtime %q is below repair baseline %s", report.SQLiteVersion, minimumSQLiteVersion)
	}
	if report.JournalMode != "wal" {
		return details, fmt.Errorf("journal_mode=%q, want wal", report.JournalMode)
	}
	if report.Synchronous != 2 {
		return details, fmt.Errorf("synchronous=%d, want FULL (2)", report.Synchronous)
	}
	if report.ForeignKeys != 1 {
		return details, fmt.Errorf("foreign_keys=%d, want ON (1)", report.ForeignKeys)
	}
	if report.BusyTimeoutMS != busyTimeoutMS {
		return details, fmt.Errorf("busy_timeout=%d, want %d", report.BusyTimeoutMS, busyTimeoutMS)
	}
	if report.MaxOpenConnections != 1 || report.MaxIdleConnections != 1 {
		return details, fmt.Errorf("pool max connections are open=%d idle=%d, want 1/1", report.MaxOpenConnections, report.MaxIdleConnections)
	}
	return details, nil
}

func executeCase(fn func() (map[string]any, error)) caseResult {
	started := time.Now()
	details, err := fn()
	result := caseResult{
		Passed:     err == nil,
		DurationMS: time.Since(started).Milliseconds(),
		Details:    details,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func runCRUDTransactions(db *sql.DB) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, statement := range []string{
		`CREATE TABLE parents (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE)`,
		`CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents(id), name TEXT NOT NULL UNIQUE)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, fmt.Errorf("create schema: %w", err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin committed transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO parents(id, name) VALUES (?, ?)`, 1, "中文父节点"); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert parent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO children(id, parent_id, name) VALUES (?, ?, ?)`, 1, 1, "child-1"); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert child: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit baseline: %w", err)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rollback transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO parents(id, name) VALUES (?, ?)`, 9, "rollback-parent"); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert rollback row: %w", err)
	}
	if err := tx.Rollback(); err != nil {
		return nil, fmt.Errorf("rollback row: %w", err)
	}

	_, foreignErr := db.ExecContext(ctx, `INSERT INTO children(id, parent_id, name) VALUES (?, ?, ?)`, 2, 404, "invalid-parent")
	if foreignErr == nil {
		return nil, errors.New("foreign key violation was accepted")
	}
	_, uniqueErr := db.ExecContext(ctx, `INSERT INTO parents(id, name) VALUES (?, ?)`, 2, "中文父节点")
	if uniqueErr == nil {
		return nil, errors.New("unique violation was accepted")
	}

	var rollbackCount, parentCount, childCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM parents WHERE id = 9`).Scan(&rollbackCount); err != nil {
		return nil, fmt.Errorf("check rollback row: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM parents`).Scan(&parentCount); err != nil {
		return nil, fmt.Errorf("count parents: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM children`).Scan(&childCount); err != nil {
		return nil, fmt.Errorf("count children: %w", err)
	}
	if rollbackCount != 0 || parentCount != 1 || childCount != 1 {
		return nil, fmt.Errorf("unexpected committed state: rollback=%d parents=%d children=%d", rollbackCount, parentCount, childCount)
	}
	return map[string]any{
		"committed_parent_count": parentCount,
		"committed_child_count":  childCount,
		"rollback_row_count":     rollbackCount,
		"foreign_key_rejected":   true,
		"unique_rejected":        true,
	}, nil
}

func runReopenIntegrity(db *sql.DB) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var parentName string
	if err := db.QueryRowContext(ctx, `SELECT name FROM parents WHERE id = 1`).Scan(&parentName); err != nil {
		return nil, fmt.Errorf("read committed row after reopen: %w", err)
	}
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return nil, fmt.Errorf("integrity_check: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("integrity_check=%q, want ok", integrity)
	}
	return map[string]any{
		"reopened_parent": parentName,
		"integrity_check": integrity,
	}, nil
}

func runConcurrentWrites(db *sql.DB) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE TABLE concurrent_items (id INTEGER PRIMARY KEY, worker INTEGER NOT NULL, value TEXT NOT NULL)`); err != nil {
		return nil, fmt.Errorf("create concurrent table: %w", err)
	}

	start := make(chan struct{})
	errorsCh := make(chan error, writeWorkers)
	var wg sync.WaitGroup
	for worker := 0; worker < writeWorkers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := insertWorkerBatch(ctx, db, worker); err != nil {
				errorsCh <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		return nil, fmt.Errorf("concurrent transaction: %w", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM concurrent_items`).Scan(&count); err != nil {
		return nil, fmt.Errorf("count concurrent rows: %w", err)
	}
	want := writeWorkers * rowsPerWorker
	if count != want {
		return nil, fmt.Errorf("concurrent row count=%d, want %d", count, want)
	}
	return map[string]any{
		"workers":         writeWorkers,
		"rows_per_worker": rowsPerWorker,
		"rows":            count,
		"transactional":   true,
	}, nil
}

func insertWorkerBatch(ctx context.Context, db *sql.DB, worker int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for row := 0; row < rowsPerWorker; row++ {
		id := (worker+1)*1000 + row
		if _, err := tx.ExecContext(ctx, `INSERT INTO concurrent_items(id, worker, value) VALUES (?, ?, ?)`, id, worker, fmt.Sprintf("worker-%d-row-%d", worker, row)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func runBoundedContextWait(path string, db *sql.DB) (map[string]any, error) {
	lockDB, err := openSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("open lock connection: %w", err)
	}
	defer lockDB.Close()
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer lockCancel()
	lockTx, err := lockDB.BeginTx(lockCtx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin lock transaction: %w", err)
	}
	if _, err := lockTx.ExecContext(lockCtx, `INSERT INTO concurrent_items(id, worker, value) VALUES (?, ?, ?)`, 9000, 9, "lock-holder"); err != nil {
		lockTx.Rollback()
		return nil, fmt.Errorf("hold write lock: %w", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), lockContextTimeout)
	defer waitCancel()
	started := time.Now()
	done := make(chan error, 1)
	go func() {
		_, execErr := db.ExecContext(waitCtx, `INSERT INTO concurrent_items(id, worker, value) VALUES (?, ?, ?)`, 9001, 9, "must-timeout")
		done <- execErr
	}()

	var execErr error
	timer := time.NewTimer(lockWaitLimit)
	select {
	case execErr = <-done:
		timer.Stop()
	case <-timer.C:
		lockTx.Rollback()
		execErr = <-done
		return nil, fmt.Errorf("lock wait exceeded %s (after releasing lock: %v)", lockWaitLimit, execErr)
	}
	elapsed := time.Since(started)
	rollbackErr := lockTx.Rollback()
	if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		return nil, fmt.Errorf("release lock transaction: %w", rollbackErr)
	}
	if execErr == nil {
		return nil, errors.New("write succeeded while another transaction held the lock")
	}
	if elapsed > lockWaitLimit {
		return nil, fmt.Errorf("lock wait elapsed=%s, want <=%s", elapsed, lockWaitLimit)
	}
	if waitCtx.Err() != context.DeadlineExceeded && !errors.Is(execErr, context.DeadlineExceeded) {
		return nil, fmt.Errorf("bounded write error=%v without observable context deadline", execErr)
	}

	return map[string]any{
		"busy_timeout_ms":       busyTimeoutMS,
		"context_timeout_ms":    lockContextTimeout.Milliseconds(),
		"elapsed_ms":            elapsed.Milliseconds(),
		"context_deadline_seen": true,
		"lock_holder":           "separate test-only database handle",
		"error":                 execErr.Error(),
	}, nil
}

func versionAtLeast(version, minimum string) bool {
	got, ok := parseVersion(version)
	if !ok {
		return false
	}
	want, ok := parseVersion(minimum)
	if !ok {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return got[i] > want[i]
		}
	}
	return true
}

func parseVersion(version string) ([3]int, bool) {
	var parsed [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for i, part := range parts {
		if part == "" {
			return parsed, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsed, false
		}
		parsed[i] = value
	}
	return parsed, true
}
