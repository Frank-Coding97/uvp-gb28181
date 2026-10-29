package recordcache

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 这几条用例走**真 GORM + sqlite**，不套 fakeRepo。
//
// ⛔ 为什么必须这样：收藏的"跳过自动清理"判据写在 `GormRepo.ListExpired` 的 **SQL** 里。
// service 层用例用的是 fakeRepo，改坏 SQL 它一点反应都没有 —— 用例会绿，线上照样删。
// 同款"SQL 层判据只能靠真库测"的坑见 ListQuery 的 favorite 过滤。

func newFavoriteRepo(t *testing.T) (*GormRepo, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbRecordCacheTask{}))
	return NewGormRepo(db), db
}

// favoriteRow 造一条最小可插入的任务行。
func favoriteRow(t *testing.T, db *gorm.DB, taskID, state string, favorite bool, expiresAt time.Time) *gbmodels.GbRecordCacheTask {
	t.Helper()
	row := &gbmodels.GbRecordCacheTask{
		TaskID: taskID, OwnerDeptID: 1, CreatedByUser: 1, CreatedByName: "admin",
		ChannelID: 31, DeviceID: "34020000001320000001", ChannelCode: "34020000001320000001",
		ChannelName: "东门", DeviceName: "一号 NVR",
		StartTime:  time.Date(2026, 8, 2, 8, 10, 0, 0, time.UTC),
		EndTime:    time.Date(2026, 8, 2, 8, 42, 0, 0, time.UTC),
		RecordType: "all", RecordKey: "rk-1", DownloadSpeed: 4,
		VHost: "__defaultVhost__", App: "rtp", Stream: "pb-1",
		State: state, Favorite: favorite, ExpiresAt: &expiresAt,
	}
	require.NoError(t, db.Create(row).Error)
	return row
}

// TestListExpiredSkipsFavoriteTasks 是这条功能的核心判据：
// 收藏过的录像**永远不会**被自动清理挑中，未收藏的照常挑中。
func TestListExpiredSkipsFavoriteTasks(t *testing.T) {
	repo, db := newFavoriteRepo(t)
	past := time.Now().Add(-48 * time.Hour)

	kept := favoriteRow(t, db, "task-favorite", gbmodels.RecordCacheStateSucceeded, true, past)
	doomed := favoriteRow(t, db, "task-normal", gbmodels.RecordCacheStateSucceeded, false, past)

	expired, err := repo.ListExpired(context.Background(), time.Now(), 20)
	require.NoError(t, err)

	ids := make([]string, 0, len(expired))
	for _, row := range expired {
		ids = append(ids, row.TaskID)
	}
	require.Contains(t, ids, doomed.TaskID, "未收藏的过期任务必须被清理挑中")
	require.NotContains(t, ids, kept.TaskID,
		"收藏的录像不许进清理集合 —— 进去了就会被 removeFiles 把文件删掉")
}

// TestListExpiredKeepsFilteringInSQL 钉住"过滤在 SQL 里而不是 Go 循环里"。
//
// ⛔ 每轮只取 limit 条：如果在循环里 `continue` 跳过收藏行，一批收藏任务会反复占满名额，
// 把真正该清的排在后面**永远清不到**。所以造足够多的收藏行把 limit 占满，
// 未收藏的那条仍然必须出现在结果里。
func TestListExpiredKeepsFilteringInSQL(t *testing.T) {
	repo, db := newFavoriteRepo(t)
	past := time.Now().Add(-48 * time.Hour)

	for i := 0; i < 5; i++ {
		favoriteRow(t, db, "fav-"+string(rune('a'+i)), gbmodels.RecordCacheStateSucceeded, true, past)
	}
	doomed := favoriteRow(t, db, "task-normal", gbmodels.RecordCacheStateSucceeded, false, past)

	// limit 只有 2：若实现是"先取 2 条再在 Go 里跳过收藏"，这里会一条都拿不到。
	expired, err := repo.ListExpired(context.Background(), time.Now(), 2)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, doomed.TaskID, expired[0].TaskID)
}

// TestUpdatePersistsFavorite 钉住 favorite 在 Update 的列清单里。
//
// ⛔ 漏了这一列的错法很隐蔽：内存里 Favorite 已经是 true，界面星标也亮，
// 但库里还是 0 —— 重启后星标消失、清理照样把文件删掉。
func TestUpdatePersistsFavorite(t *testing.T) {
	repo, db := newFavoriteRepo(t)
	future := time.Now().Add(24 * time.Hour)
	row := favoriteRow(t, db, "task-1", gbmodels.RecordCacheStateSucceeded, false, future)

	row.Favorite = true
	require.NoError(t, repo.Update(context.Background(), row))

	stored, err := repo.FindByTaskID(context.Background(), "task-1", nil)
	require.NoError(t, err)
	require.True(t, stored.Favorite, "收藏必须真的落库（Update 的列清单漏了 favorite 就落不下去）")

	// 反向也要能落：取消收藏。
	stored.Favorite = false
	require.NoError(t, repo.Update(context.Background(), stored))
	again, err := repo.FindByTaskID(context.Background(), "task-1", nil)
	require.NoError(t, err)
	require.False(t, again.Favorite)
}

// TestListFiltersByFavorite 钉住「只看收藏 / 只看未收藏 / 不筛」三种语义互不串味。
func TestListFiltersByFavorite(t *testing.T) {
	repo, db := newFavoriteRepo(t)
	future := time.Now().Add(24 * time.Hour)
	favorited := favoriteRow(t, db, "task-fav", gbmodels.RecordCacheStateSucceeded, true, future)
	plain := favoriteRow(t, db, "task-plain", gbmodels.RecordCacheStateSucceeded, false, future)

	ids := func(favorite *bool) []string {
		rows, _, err := repo.List(context.Background(), ListQuery{Page: 1, PageSize: 20, Favorite: favorite})
		require.NoError(t, err)
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.TaskID)
		}
		return out
	}

	yes := true
	no := false
	require.ElementsMatch(t, []string{favorited.TaskID}, ids(&yes), "只看收藏")
	require.ElementsMatch(t, []string{plain.TaskID}, ids(&no), "只看未收藏")
	require.ElementsMatch(t, []string{favorited.TaskID, plain.TaskID}, ids(nil),
		"不传 favorite 时不能被零值当成「只看未收藏」")
}

// TestServiceSetFavoritePersists 串一遍服务层到真库：SetFavorite 必须落库且幂等。
func TestServiceSetFavoritePersists(t *testing.T) {
	repo, db := newFavoriteRepo(t)
	future := time.Now().Add(24 * time.Hour)
	favoriteRow(t, db, "task-1", gbmodels.RecordCacheStateSucceeded, false, future)

	service := &Service{repo: repo}
	view, err := service.SetFavorite(context.Background(), "task-1", nil, true)
	require.NoError(t, err)
	require.True(t, view.Favorite)

	stored, err := repo.FindByTaskID(context.Background(), "task-1", nil)
	require.NoError(t, err)
	require.True(t, stored.Favorite)

	// 幂等：重复设置同一个值不报错，结果不变。
	again, err := service.SetFavorite(context.Background(), "task-1", nil, true)
	require.NoError(t, err)
	require.True(t, again.Favorite)

	_, err = service.SetFavorite(context.Background(), "missing", nil, true)
	require.ErrorIs(t, err, ErrTaskNotFound)
}
