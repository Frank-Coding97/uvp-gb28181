package sqlitebootstrap

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type transactionOptions struct {
	foreignKeysOff bool
}

// withImmediateTransaction reserves the sole writer before inspecting the
// database. The rollback state machine lives in withSQLiteTransaction so the
// baseline and incremental migrations cannot drift apart.
func withImmediateTransaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB, *sql.Conn) error) error {
	return withSQLiteTransaction(ctx, db, transactionOptions{}, fn)
}

func withForeignKeysOffImmediateTransaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB, *sql.Conn) error) error {
	return withSQLiteTransaction(ctx, db, transactionOptions{foreignKeysOff: true}, fn)
}

// withSQLiteTransaction pins one sql.Conn, optionally disables foreign-key
// enforcement before BEGIN IMMEDIATE, and restores it after commit or rollback.
// SQLite PRAGMAs are deliberately executed outside the transaction because
// foreign_keys changes inside a transaction are ignored.
func withSQLiteTransaction(ctx context.Context, db *gorm.DB, options transactionOptions, fn func(*gorm.DB, *sql.Conn) error) (err error) {
	return db.WithContext(ctx).Connection(func(tx *gorm.DB) (err error) {
		conn, ok := tx.Statement.ConnPool.(*sql.Conn)
		if !ok {
			return errors.New("SQLite initialization requires a dedicated connection")
		}

		foreignKeysOff := false
		began := false
		committed := false
		defer func() {
			if began && !committed {
				rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, rollbackErr := conn.ExecContext(rollbackCtx, "ROLLBACK")
				cancel()
				if rollbackErr != nil {
					discardSQLiteConn(conn)
					err = errors.Join(err, fmt.Errorf("SQLite rollback failed: %w", rollbackErr))
				}
			}
			if foreignKeysOff {
				if restoreErr := restoreForeignKeys(conn); restoreErr != nil {
					discardSQLiteConn(conn)
					err = errors.Join(err, restoreErr)
				}
			}
		}()

		if options.foreignKeysOff {
			if err = verifyForeignKeys(conn, ctx, 1); err != nil {
				return err
			}
			// Keep cleanup responsible for restoration even if the PRAGMA or its
			// confirmation fails after the driver has changed the connection.
			foreignKeysOff = true
			if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
				return err
			}
			if err = verifyForeignKeys(conn, ctx, 0); err != nil {
				return err
			}
		}

		if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return err
		}
		began = true
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

func verifyForeignKeys(conn *sql.Conn, ctx context.Context, expected int) error {
	var value int
	if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&value); err != nil {
		return fmt.Errorf("read SQLite foreign_keys: %w", err)
	}
	if value != expected {
		return fmt.Errorf("SQLite foreign_keys=%d, want %d", value, expected)
	}
	return nil
}

func restoreForeignKeys(conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("restore SQLite foreign_keys: %w", err)
	}
	if err := verifyForeignKeys(conn, ctx, 1); err != nil {
		return fmt.Errorf("verify restored SQLite foreign_keys: %w", err)
	}
	return nil
}

func discardSQLiteConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}
