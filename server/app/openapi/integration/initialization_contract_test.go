package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readInitializationContractSQL(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	return string(body)
}

func TestMySQLInitializationPTZTableUsesSingleStatementTerminator(t *testing.T) {
	body := readInitializationContractSQL(t, "uvp-gb28181.sql")
	const marker = "COMMENT='GB28181 latest PTZ state'"

	var matchingLines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, marker) {
			matchingLines = append(matchingLines, strings.TrimSpace(line))
		}
	}
	require.Len(t, matchingLines, 1)
	require.Equal(t, 1, strings.Count(matchingLines[0], ";"), matchingLines[0])
	require.True(t, strings.HasSuffix(matchingLines[0], ";"), matchingLines[0])
}

func TestPostgreSQLInitializationQuotesSysJobsGroupIdentifier(t *testing.T) {
	body := readInitializationContractSQL(t, "postgresql_converted.sql")

	require.Contains(t, body, "    \"group\" VARCHAR(100) NOT NULL,")
	require.Contains(t, body, `COMMENT ON COLUMN sys_jobs."group" IS '任务分组名称';`)
	require.NotContains(t, body, "\n    group VARCHAR(100) NOT NULL,")
	require.NotContains(t, body, "COMMENT ON COLUMN sys_jobs.group IS")
}
