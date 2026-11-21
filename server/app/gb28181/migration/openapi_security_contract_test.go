package migration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 2026-09-23 清理历史迁移（ecbc72de）时把 openapi-must-auth 的幂等插入一并删掉了，
// 但全量基线没有接住这行单例种子。全新装库后 sys_openapi_security_state 是空表，
// OpenAPI 初始化按 fail-closed 规则直接拒绝启动，四个方言表现一致。
//
// 这个契约守两件事：
//  1. 每个交付基线都必须携带且只携带一条 id=1 的初始行（false, NULL, 0）；
//  2. 初始化脚本永远不许改写已有状态（一次性上锁只能由应用侧 Latch 完成）。
func TestOpenAPIMustAuthSingletonSeedInReleasedBaselines(t *testing.T) {
	// PG 的布尔列字面量是 false，其余方言是 0；值本身必须与历史迁移一致。
	seedValue := regexp.MustCompile(`\(1,\s*(?:0|false),\s*null,\s*0\)`)
	for _, snapshot := range []string{
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
		"dameng_converted.sql",
	} {
		t.Run(snapshot, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", snapshot))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))

			var seeds []string
			for _, statement := range splitStatements(sql) {
				if !strings.Contains(statement, "insert into") {
					continue
				}
				if !strings.Contains(statement, "sys_openapi_security_state") {
					continue
				}
				seeds = append(seeds, statement)
			}
			require.Lenf(t, seeds, 1,
				"%s 必须且只能有一条 sys_openapi_security_state 单例种子", snapshot)

			flat := strings.Join(strings.Fields(seeds[0]), " ")
			require.Regexpf(t, seedValue, flat,
				"%s 的种子值必须保持历史迁移的 (1, false/0, NULL, 0)", snapshot)

			require.NotContains(t, sql, "update sys_openapi_security_state",
				"%s: 初始化路径永远不许改写安全状态", snapshot)
			require.NotContains(t, sql, "delete from sys_openapi_security_state",
				"%s: 初始化路径永远不许删除安全状态", snapshot)
		})
	}
}
