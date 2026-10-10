package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
)

func TestDialectOf(t *testing.T) {
	require.Equal(t, DialectMySQL, DialectOf(&mysql.Dialector{Config: &mysql.Config{}}))
	require.Equal(t, DialectPostgres, DialectOf(&postgres.Dialector{Config: &postgres.Config{}}))
	require.Equal(t, DialectSQLServer, DialectOf(&sqlserver.Dialector{Config: &sqlserver.Config{}}))
}

func TestDialectOfUnknown(t *testing.T) {
	require.Equal(t, DialectUnknown, DialectOf(nil))
}
