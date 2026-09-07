package favorites

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newFavoritesSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "favorites.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func favoriteSQLiteContext(userID uint) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: userID}})
	return c
}

func TestChannelFavoriteServiceSQLiteBaselineNaturalKeyIsConcurrentSafe(t *testing.T) {
	db, path := newFavoritesSQLiteBaselineDB(t)
	user := basemodels.User{BaseModel: basemodels.BaseModel{ID: 702}, Username: "sqlite-favorite-user", Password: "x", Status: 1, DeptID: 1}
	require.NoError(t, db.Create(&user).Error)
	channel := gbmodels.GbChannel{DeviceID: "fav-device", ChannelID: "fav-channel", Name: "大厅", OwnerDeptID: 1}
	require.NoError(t, db.Create(&channel).Error)
	service := NewService(db)
	ctx := favoriteSQLiteContext(user.ID)
	group, err := service.Create(ctx, user.ID, CreateRequest{Name: "SQLite收藏", Channels: []ChannelInput{{DeviceCode: channel.DeviceID, ChannelCode: channel.ChannelID}}})
	require.NoError(t, err)
	require.Len(t, group.Items, 1)

	second, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	rawSecond, err := second.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawSecond.Close() })
	otherService := NewService(second)
	start := make(chan struct{})
	errors := make(chan error, 2)
	results := make(chan AppendResult, 2)
	var wg sync.WaitGroup
	for _, item := range []struct {
		service *Service
		ctx     *gin.Context
	}{
		{service: service, ctx: favoriteSQLiteContext(user.ID)},
		{service: otherService, ctx: favoriteSQLiteContext(user.ID)},
	} {
		wg.Add(1)
		go func(item struct {
			service *Service
			ctx     *gin.Context
		}) {
			defer wg.Done()
			<-start
			result, err := item.service.Append(item.ctx, user.ID, group.ID, []ChannelInput{{DeviceCode: channel.DeviceID, ChannelCode: channel.ChannelID}})
			results <- result
			errors <- err
		}(item)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	added := 0
	for result := range results {
		added += result.AddedCount
	}
	require.Zero(t, added)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannelFavoriteItem{}).Where("group_id = ? AND device_code = ? AND channel_code = ?", group.ID, channel.DeviceID, channel.ChannelID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
