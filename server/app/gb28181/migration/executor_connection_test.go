package migration

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMigrationExecutorKeepsSessionBetweenStatements(t *testing.T) {
	for _, prepare := range []bool{false, true} {
		name := "plain"
		if prepare {
			name = "prepared"
		}
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "session.db")), &gorm.Config{PrepareStmt: prepare, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, raw.Close()) })
			// Closing every returned connection deterministically exposes statements
			// accidentally executed via the pool instead of one database session.
			raw.SetMaxIdleConns(0)
			err = (&dbExecutor{db: db}).ExecSQL("CREATE TEMP TABLE migration_session_probe (id INTEGER);\nINSERT INTO migration_session_probe VALUES (1);\nDROP TABLE migration_session_probe;")
			require.NoError(t, err)
		})
	}
}
