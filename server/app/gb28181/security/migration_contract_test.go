package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityMigrationsContainSameTablesAndObserveSeed(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-08-public-sip-security.sql", "2026-08-08-public-sip-security-postgresql.sql", "2026-08-08-public-sip-security-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		for _, table := range []string{"gb_sip_security_event", "gb_sip_security_ban", "gb_sip_security_policy", "gb_sip_security_audit"} {
			require.Contains(t, text, table, name)
		}
		require.Contains(t, text, "observe", name)
	}
}

func TestSecurityFreshSchemaAndDefaultAllowlistStayInSync(t *testing.T) {
	databaseRoot := filepath.Join("..", "..", "..", "resource", "database")
	fresh, err := os.ReadFile(filepath.Join(databaseRoot, "uvp-gb28181.sql"))
	require.NoError(t, err)
	for _, table := range []string{"gb_sip_security_event", "gb_sip_security_ban", "gb_sip_security_policy", "gb_sip_security_audit"} {
		require.Contains(t, string(fresh), table)
	}

	migrations := filepath.Join(databaseRoot, "gb28181", "migrations")
	files := []string{"2026-08-09-public-sip-security-default-allowlist.sql", "2026-08-09-public-sip-security-default-allowlist-postgresql.sql", "2026-08-09-public-sip-security-default-allowlist-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(migrations, name))
		require.NoError(t, err, name)
		text := string(data)
		for _, network := range []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			require.Contains(t, text, network, name)
		}
	}
}
