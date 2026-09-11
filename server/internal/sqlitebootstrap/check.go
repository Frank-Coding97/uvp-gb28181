package sqlitebootstrap

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// CheckCurrentSchema verifies the immutable SQLite baseline and every
// migration compiled into this release. It only performs reads on one pinned
// database connection; it never initializes or migrates the database.
func CheckCurrentSchema(ctx context.Context, db *gorm.DB) error {
	if db == nil || db.Config == nil || db.Dialector == nil {
		return errors.New("SQLite schema check requires a database")
	}
	if db.Dialector.Name() != "sqlite" {
		return errors.New("SQLite schema check requires sqlite")
	}
	if ctx == nil {
		return errors.New("SQLite schema check requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMigrationSpecs(compiledMigrations); err != nil {
		return err
	}

	raw, err := db.DB()
	if err != nil {
		return fmt.Errorf("open SQLite schema check database: %w", err)
	}
	conn, err := raw.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin SQLite schema check connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	markers, err := readMigrationMarkers(ctx, conn)
	if err != nil {
		return err
	}
	if err := validateMarkerState(markers, compiledMigrations); err != nil {
		return err
	}
	if err := validateAppliedOrder(markers, compiledMigrations); err != nil {
		return err
	}
	for _, spec := range compiledMigrations {
		if _, applied := markers[spec.Version]; !applied {
			return fmt.Errorf("SQLite migration %q is not applied", spec.Version)
		}
	}
	if err := checkForeignKeys(ctx, conn); err != nil {
		return fmt.Errorf("SQLite schema foreign keys: %w", err)
	}
	if err := checkSQLiteIntegrity(ctx, conn); err != nil {
		return fmt.Errorf("SQLite schema integrity: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
