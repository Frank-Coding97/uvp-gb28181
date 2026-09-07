// Package sqlitebootstrap applies the standalone release baseline atomically.
package sqlitebootstrap

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// applyBaseline accepts only a release script whose digest is independently
// pinned. The seed callback is part of the same transaction as schema and marker.
func applyBaseline(ctx context.Context, db *gorm.DB, version, script, expectedChecksum string, seed func(*gorm.DB) error) (created bool, err error) {
	if db.Dialector.Name() != "sqlite" {
		return false, errors.New("SQLite baseline requires sqlite")
	}
	if version == "" {
		return false, errors.New("SQLite baseline version is empty")
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(script))) != expectedChecksum {
		return false, errors.New("SQLite baseline checksum mismatch")
	}
	err = withImmediateTransaction(ctx, db, func(tx *gorm.DB, conn *sql.Conn) error {
		var markerExists int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gb_schema_migrations'").Scan(&markerExists); err != nil {
			return err
		}
		if markerExists != 0 {
			var checksum string
			if err := conn.QueryRowContext(ctx, "SELECT checksum FROM gb_schema_migrations WHERE version=?", version).Scan(&checksum); err != nil {
				return fmt.Errorf("unknown SQLite baseline: %w", err)
			}
			if checksum != expectedChecksum {
				return errors.New("stored SQLite baseline checksum mismatch")
			}
			return nil
		}
		var tables int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT GLOB 'sqlite_*'").Scan(&tables); err != nil {
			return err
		}
		if tables != 0 {
			return errors.New("refusing to initialize non-empty SQLite database without baseline marker")
		}
		// The modernc driver executes the entire batch on this pinned connection.
		// Do not route batch SQL through GORM's prepared-statement cache.
		if _, err := conn.ExecContext(ctx, script); err != nil {
			return fmt.Errorf("SQLite baseline SQL: %w", err)
		}
		if seed != nil {
			if err := seed(tx); err != nil {
				return err
			}
		}
		rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		invalid := rows.Next()
		rowErr := rows.Err()
		closeErr := rows.Close()
		if err := errors.Join(rowErr, closeErr); err != nil {
			return err
		}
		if invalid {
			return errors.New("SQLite baseline foreign key check failed")
		}
		var integrity string
		if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite baseline integrity: %s", integrity)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE gb_schema_migrations(version TEXT PRIMARY KEY NOT NULL, applied_at DATETIME NOT NULL, checksum TEXT NOT NULL)"); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO gb_schema_migrations(version,applied_at,checksum) VALUES(?,?,?)", version, time.Now().UTC(), expectedChecksum); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return created, nil
}

// BEGIN IMMEDIATE reserves the sole writer before inspecting the marker, so a
// competing initializer cannot read an empty state and race schema creation.
func withImmediateTransaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB, *sql.Conn) error) error {
	return db.WithContext(ctx).Connection(func(tx *gorm.DB) (err error) {
		conn, ok := tx.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return errors.New("SQLite initialization requires a dedicated connection")
		}
		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK"); rollbackErr != nil {
					// Never return an uncertain open transaction to the pool.
					_ = conn.Raw(func(any) error { return driver.ErrBadConn })
					err = errors.Join(err, fmt.Errorf("SQLite rollback failed: %w", rollbackErr))
				}
			}
		}()
		tx = tx.Session(&gorm.Session{NewDB: true, SkipDefaultTransaction: true})
		tx.Config.PrepareStmt = false
		if err = fn(tx, conn); err != nil {
			return err
		}
		if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
			return err
		}
		committed = true
		return nil
	})
}
