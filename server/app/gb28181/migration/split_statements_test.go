package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitStatements_MultiStatementsWithComments(t *testing.T) {
	sql := `-- 头部注释

INSERT INTO t1 (a) VALUES (1);

-- 中间注释
INSERT INTO t2 (a)
SELECT b FROM t3 WHERE c = 1;
`
	stmts := splitStatements(sql)
	assert.Len(t, stmts, 2)
	assert.Contains(t, stmts[0], "INSERT INTO t1")
	assert.Contains(t, stmts[1], "INSERT INTO t2")
}

func TestSplitStatements_NoTrailingSemicolon(t *testing.T) {
	stmts := splitStatements("CREATE TABLE IF NOT EXISTS t (id INT);\nINSERT INTO t VALUES (1)")
	assert.Len(t, stmts, 2)
	assert.Contains(t, stmts[1], "INSERT INTO t VALUES (1)")
}

func TestSplitStatements_OnlyCommentsAndBlanks(t *testing.T) {
	assert.Empty(t, splitStatements("-- 只有注释\n\n  \n-- 另一条注释\n"))
}
