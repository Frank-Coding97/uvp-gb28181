package talk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// sqliteTalkSessionDDL 从交付基线里抽出 gb_talk_session 的建表语句与索引语句。
//
// 按行扫描 + 括号配平，而不是正则：建表语句里有 CHECK (...) 嵌套，正则很容易截断。
func sqliteTalkSessionDDL(t *testing.T, body, table string) []string {
	t.Helper()
	lines := strings.Split(body, "\n")
	var stmts []string
	prefix := `CREATE TABLE IF NOT EXISTS "` + table + `"`
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			continue
		}
		depth := 0
		var sb strings.Builder
		for ; i < len(lines); i++ {
			cur := lines[i]
			sb.WriteString(cur)
			sb.WriteString("\n")
			depth += strings.Count(cur, "(") - strings.Count(cur, ")")
			if depth == 0 {
				break
			}
		}
		stmts = append(stmts, strings.TrimSuffix(strings.TrimSpace(sb.String()), ";"))
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "CREATE ") && strings.Contains(trimmed, ` ON "`+table+`"`) {
			stmts = append(stmts, strings.TrimSuffix(trimmed, ";"))
		}
	}
	return stmts
}

// TestTalkSessionBaselineCoversModelColumns 钉住「交付基线能承载 talk model 的全部列」。
//
// 为什么必须单独有这一条：talk 包里其它用例都走 `AutoMigrate(&models.GbTalkSession{})`
// 自建表 —— 包括那个名字看起来像对账的 `TestTalkModelIndexesMatchMigration`。
// 自己拿 model 建表、再拿 model 断言，永远不可能发现「交付基线少了列」。
// 真实后果（2026-10-25 实测）：`openapi_client_id` / `openapi_idempotency_key` /
// `openapi_request_hash` 三列在 model 里存在、在四份交付脚本里全缺，而生产代码
// **没有 AutoMigrate** ⇒ 全新部署第一次走 OpenAPI 建对讲会话就报
// `no such column: openapi_client_id`，且只有装完才暴露。
//
// ⛔ 以后 model 加列，本用例会立刻红 —— 那是提醒你去改 schema.ir.json 并重跑
// generate_sql.py / sqlitebaseline/generate.py，不是提醒你放宽断言。
func TestTalkSessionBaselineCoversModelColumns(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../../resource/database/sqlitebaseline", "baseline.sql"))
	require.NoError(t, err)
	stmts := sqliteTalkSessionDDL(t, string(body), "gb_talk_session")
	require.NotEmpty(t, stmts, "交付基线里找不到 gb_talk_session 的建表语句")

	db, err := gorm.Open(sqlite.Open("file:talk_baseline_contract?mode=memory&cache=shared"),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	for _, stmt := range stmts {
		require.NoErrorf(t, db.Exec(stmt).Error, "执行基线语句失败：%.120s", stmt)
	}

	parsed, err := schema.Parse(&models.GbTalkSession{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	var missing []string
	for _, field := range parsed.Fields {
		if field.DBName == "" {
			continue
		}
		if !db.Migrator().HasColumn("gb_talk_session", field.DBName) {
			missing = append(missing, field.DBName)
		}
	}
	require.Emptyf(t, missing,
		"交付基线缺少 model 字段对应的列 %v —— 全新部署写对讲会话会直接报 no such column", missing)

	require.True(t, db.Migrator().HasIndex("gb_talk_session", "idx_talk_session_openapi_idempotency"),
		"OpenAPI 幂等查询依赖 (openapi_client_id, openapi_idempotency_key) 复合索引")

	// 光有列名不够：真写一条带 OpenAPI 身份的会话，走一遍 Create + 幂等回查。
	repo := NewGormRepo(db)
	ctx := context.Background()
	key := "baseline-idempotency-key"
	session := newReservedSession("baseline-openapi", 11, time.Now().UTC().Add(time.Minute))
	session.OpenAPIClientID = 42
	session.OpenAPIIdempotencyKey = &key
	session.OpenAPIRequestHash = strings.Repeat("a", 64)
	require.NoError(t, repo.Create(ctx, session, "baseline-publish-token"))

	got, err := repo.FindByOpenAPIIdempotency(ctx, 42, key)
	require.NoError(t, err)
	require.NotNil(t, got, "写入后必须能按 (clientID, key) 查回，否则幂等链路在新装环境是断的")
	require.Equal(t, session.SessionID, got.SessionID)
}
