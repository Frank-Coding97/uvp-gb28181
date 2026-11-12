package security

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityPermanentPolicyMigrationsPreserveBanHistory(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	pairs := []struct {
		name string
		up   string
		down string
	}{
		{name: "mysql", up: "2026-09-05-security-permanent-auto-ban.sql", down: "2026-09-05-security-permanent-auto-ban-down.sql"},
		{name: "postgresql", up: "2026-09-05-security-permanent-auto-ban-postgresql.sql", down: "2026-09-05-security-permanent-auto-ban-postgresql-down.sql"},
		{name: "sqlserver", up: "2026-09-05-security-permanent-auto-ban-sqlserver.sql", down: "2026-09-05-security-permanent-auto-ban-sqlserver-down.sql"},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			upBytes, err := os.ReadFile(filepath.Join(root, pair.up))
			require.NoError(t, err, pair.up)
			downBytes, err := os.ReadFile(filepath.Join(root, pair.down))
			require.NoError(t, err, pair.down)

			up := strings.ToLower(string(upBytes))
			down := strings.ToLower(string(downBytes))
			for _, text := range []string{up, down} {
				require.Contains(t, text, "gb_sip_security_policy")
				require.NotContains(t, text, "gb_sip_security_ban")
			}
			require.Contains(t, up, "ban_ttl_steps")
			require.Contains(t, up, "ban_score")
			require.Contains(t, up, ":0")
			require.Contains(t, down, "ban_ttl_steps")
			require.Contains(t, down, "100:60;200:600;500:3600")
		})
	}
}

func TestSecurityFreshPolicySeedsArePermanentAcrossThreeDialects(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	files := []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"}
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(data))
		require.Contains(t, text, "gb_sip_security_policy", name)
		require.Contains(t, text, "100:0", name)
		require.NotContains(t, text, "100:60;200:600;500:3600", name)
	}
}

func TestSecurityConvertedFreshPolicySeedsAreIdempotent(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	cases := []struct {
		name  string
		guard string
	}{
		{name: "postgresql_converted.sql", guard: "on conflict (scope_key) do nothing"},
		{name: "sqlserver_converted.sql", guard: "if not exists (select 1 from gb_sip_security_policy where scope_key = n'global')"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(root, tc.name))
		require.NoError(t, err, tc.name)
		text := strings.ToLower(string(data))
		require.Equal(t, 1, strings.Count(text, "insert into gb_sip_security_policy"), tc.name)
		require.Contains(t, text, tc.guard, tc.name)
	}
}

func TestSecurityFreshSchemasCreateEverySecurityTableBeforePolicySeed(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	normalize := func(s string) string {
		return strings.NewReplacer("`", "", "\"", "", "[", "", "]", "").Replace(strings.ToLower(s))
	}
	reference, err := os.ReadFile(filepath.Join(root, "uvp-gb28181.sql"))
	require.NoError(t, err)
	for _, name := range []string{"postgresql_converted.sql", "sqlserver_converted.sql"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		text := normalize(string(data))
		seed := strings.Index(text, "insert into gb_sip_security_policy")
		for _, suffix := range []string{"event", "ban", "policy", "audit", "access_rule"} {
			table := "gb_sip_security_" + suffix
			ddl := regexp.MustCompile(`(?s)create table(?: if not exists)? ` + table + `\s*\((.*?)\n\)`)
			expected := ddl.FindStringSubmatch(normalize(string(reference)))
			actual := ddl.FindStringSubmatch(text)
			require.Len(t, expected, 2, table)
			require.Len(t, actual, 2, name+":"+table)
			fields := regexp.MustCompile(`(?m)^\s*(\w+)\s+(?:bigint|bigserial|varchar|nvarchar|datetime2?|timestamp|int)\b`)
			columnNames := func(s string) []string {
				var out []string
				for _, m := range fields.FindAllStringSubmatch(s, -1) {
					out = append(out, m[1])
				}
				return out
			}
			require.ElementsMatch(t, columnNames(expected[1]), columnNames(actual[1]), name+":"+table)
			require.Less(t, ddl.FindStringIndex(text)[0], seed, name+":"+table)
		}
	}
}
