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
