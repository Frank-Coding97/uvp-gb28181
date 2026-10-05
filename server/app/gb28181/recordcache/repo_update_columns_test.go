package recordcache

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// recordCacheMutableColumns 是**运行期会被缓存服务改写**的列。
//
// ⛔⛔ 这份清单必须与 `GormRepo.Update` 的 Select / Updates 同步，少一列就是
// 「不报错的错法」：GORM 的列级更新只写白名单里的列，漏掉的列既不报错也不落库。
// 最惨的一次是 `segments`（分片产出清单）—— 它写不进去时：
//  1. CachedMediaDuration() 读回来恒为 0；
//  2. 游标退化成"按请求区间推进"，把设备只推了 84 秒当成拉满 20 分钟；
//  3. 任务宣布 **succeeded**，可用户一个文件都下不到（ZLM 上文件明明录着）。
//
// 所以它不靠"记得加"，靠这条测试钉住。
var recordCacheMutableColumns = []string{
	"session_id", "node_id", "vhost", "app", "stream",
	"file_id", "file_name", "file_path", "file_size",
	"cached_bytes", "estimated_bytes", "cursor_at", "segments",
	"state", "last_error", "started_at", "finished_at", "expires_at",
}

var quotedLiteral = regexp.MustCompile(`"([a-z0-9_]+)"`)

func TestRecordCacheUpdatePersistsEveryMutableColumn(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	require.NoError(t, err)

	body := sliceUntil(textAfter(string(source), "func (r *GormRepo) Update("), "\nfunc ")
	require.NotEmpty(t, body, "没找到 GormRepo.Update 的方法体")

	selected := literalSet(sliceUntil(textAfter(body, "Select("), ")"))
	require.NotEmpty(t, selected, "没找到 Update 里的 Select(...)")
	assigned := literalSet(sliceUntil(textAfter(body, "Updates(map[string]any{"), "})"))
	require.NotEmpty(t, assigned, "没找到 Update 里的 Updates(map[string]any{...})")

	for _, column := range recordCacheMutableColumns {
		require.Contains(t, selected, column,
			"GormRepo.Update 的 Select 列清单少了 %s：该列永远落不了库，且不会有任何报错", column)
		require.Contains(t, assigned, column,
			"GormRepo.Update 的 Updates map 少了 %s：Select 里列了也不会被写入", column)
	}
}

// 反向：Select 里的每一项都必须是模型真实存在的列 —— 拼错一个字母会让
// GORM 静默忽略（或直接报 unknown column），两种都不是我们想要的失败方式。
func TestRecordCacheUpdateSelectOnlyUsesModelColumns(t *testing.T) {
	source, err := os.ReadFile("repo.go")
	require.NoError(t, err)
	body := sliceUntil(textAfter(string(source), "func (r *GormRepo) Update("), "\nfunc ")
	selected := literalSet(sliceUntil(textAfter(body, "Select("), ")"))

	known := modelColumnSet(t)
	for column := range selected {
		require.Contains(t, known, column, "Select 里的 %s 不是 GbRecordCacheTask 的列（拼错了？）", column)
	}
}

func modelColumnSet(t *testing.T) map[string]struct{} {
	t.Helper()
	columns := map[string]struct{}{}
	typ := reflect.TypeOf(gbmodels.GbRecordCacheTask{})
	for index := 0; index < typ.NumField(); index++ {
		for _, part := range strings.Split(typ.Field(index).Tag.Get("gorm"), ";") {
			if value, ok := strings.CutPrefix(part, "column:"); ok {
				columns[value] = struct{}{}
			}
		}
	}
	require.NotEmpty(t, columns, "没能从模型上读出任何列名")
	return columns
}

func textAfter(source, marker string) string {
	index := strings.Index(source, marker)
	if index < 0 {
		return ""
	}
	return source[index:]
}

func sliceUntil(source, marker string) string {
	index := strings.Index(source, marker)
	if index < 0 {
		return ""
	}
	return source[:index]
}

func literalSet(block string) map[string]struct{} {
	values := map[string]struct{}{}
	for _, matched := range quotedLiteral.FindAllStringSubmatch(block, -1) {
		values[matched[1]] = struct{}{}
	}
	return values
}
