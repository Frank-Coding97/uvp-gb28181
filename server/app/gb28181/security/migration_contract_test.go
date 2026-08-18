package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityMigrationsContainSameTablesAndProtectSeed(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-08-public-sip-security.sql", "2026-08-08-public-sip-security-postgresql.sql", "2026-08-08-public-sip-security-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		for _, table := range []string{"gb_sip_security_event", "gb_sip_security_ban", "gb_sip_security_policy", "gb_sip_security_audit"} {
			require.Contains(t, text, table, name)
		}
		require.Contains(t, text, "protect", name)
	}
}

func TestSecurityExistingGlobalPolicyIsUpgradedToProtectDefaults(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-09-public-sip-security-protect-default.sql", "2026-08-09-public-sip-security-protect-default-postgresql.sql", "2026-08-09-public-sip-security-protect-default-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		require.Contains(t, text, "mode", name)
		require.Contains(t, text, "protect", name)
		require.Contains(t, text, "window_seconds", name)
		require.Contains(t, text, "10", name)
	}
}

func TestSecurityFreshSchemaAndDefaultAllowlistStayInSync(t *testing.T) {
	databaseRoot := filepath.Join("..", "..", "..", "resource", "database")
	fresh, err := os.ReadFile(filepath.Join(databaseRoot, "uvp-gb28181.sql"))
	require.NoError(t, err)
	for _, table := range []string{"gb_sip_security_event", "gb_sip_security_ban", "gb_sip_security_policy", "gb_sip_security_audit"} {
		require.Contains(t, string(fresh), table)
	}
	require.Contains(t, strings.ToLower(string(fresh)), "`expires_at` datetime default null")
	require.Contains(t, strings.ToLower(string(fresh)), "'100:60;200:600;500:3600'")
	require.Contains(t, strings.ToLower(string(fresh)), "`device_id` varchar(64)")
	require.Contains(t, strings.ToLower(string(fresh)), "`risk_scope` varchar(16)")

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

func TestSecurityFalsePositiveRemediationMigrationsCoverThreeDialects(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{
		"2026-08-18-public-sip-security-false-positive-remediation.sql",
		"2026-08-18-public-sip-security-false-positive-remediation-postgresql.sql",
		"2026-08-18-public-sip-security-false-positive-remediation-sqlserver.sql",
	}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		for _, token := range []string{"device_id", "risk_scope", "100:60;200:600;500:3600", "origin", "auto", "expires_at", "60", "gb_sip_security_audit", "not exists"} {
			require.Contains(t, text, token, name)
		}
		require.NotContains(t, text, "origin = 'manual'", name)
		require.NotContains(t, text, "origin = n'manual'", name)
	}
}

func TestSecurityAccessRuleMigrationsCoverThreeDialectsAndAPIs(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-09-public-sip-security-access-rules.sql", "2026-08-09-public-sip-security-access-rules-postgresql.sql", "2026-08-09-public-sip-security-access-rules-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		require.Contains(t, text, "gb_sip_security_access_rule", name)
		require.Contains(t, text, "/api/gb28181/security/access-rules", name)
	}
}

func TestSecurityBanEvidenceMigrationsCoverThreeDialects(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-09-public-sip-security-ban-evidence.sql", "2026-08-09-public-sip-security-ban-evidence-postgresql.sql", "2026-08-09-public-sip-security-ban-evidence-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		for _, column := range []string{"trigger_method", "trigger_count", "trigger_threshold", "window_seconds", "policy_mode", "firewall_applied_at", "blocked_count_after_ban", "last_blocked_at"} {
			require.Contains(t, text, column, name)
		}
	}
}

func TestSecurityPermanentBanMigrationsCoverThreeDialects(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{"2026-08-09-public-sip-security-permanent-ban.sql", "2026-08-09-public-sip-security-permanent-ban-postgresql.sql", "2026-08-09-public-sip-security-permanent-ban-sqlserver.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		require.Contains(t, text, "expires_at", name)
		require.Contains(t, text, "origin", name)
		require.Contains(t, text, "agent_failed", name)
		require.Contains(t, text, "ban_ttl_steps", name)
		require.Contains(t, text, ":0", name)
	}
}
