package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 3.5:各方言锁 SQL 特征
func TestAcquireSQLDialects(t *testing.T) {
	require.Contains(t, acquireSQL(DialectMySQL), "GET_LOCK")
	require.Contains(t, acquireSQL(DialectPostgres), "pg_advisory_lock")
	require.Contains(t, acquireSQL(DialectSQLServer), "sp_getapplock")
}

func TestReleaseSQLDialects(t *testing.T) {
	require.Contains(t, releaseSQL(DialectMySQL), "RELEASE_LOCK")
	require.Contains(t, releaseSQL(DialectPostgres), "pg_advisory_unlock")
	require.Contains(t, releaseSQL(DialectSQLServer), "sp_releaseapplock")
}
