package favorites

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func favoriteFixture(t *testing.T) (*gorm.DB, *gin.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannelFavoriteGroup{}, &gbmodels.GbChannelFavoriteItem{}, &gbmodels.GbChannel{}, &gbmodels.GbDevice{}, &gbmodels.GbDeviceGrant{}, &basemodels.User{}, &basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}))
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 10}, Name: "部门"}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 7}, Username: "u", DeptID: 10}).Error)
	require.NoError(t, db.Create(&basemodels.User{BaseModel: basemodels.BaseModel{ID: 8}, Username: "v", DeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "D1", ChannelID: "C1", Name: "东门", OwnerDeptID: 10, Status: 1}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "D1", ChannelID: "C2", Name: "西门", OwnerDeptID: 10, Status: 0}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/favorites", nil)
	c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	return db, c
}

func TestChannelFavoriteServiceLifecycle(t *testing.T) {
	db, c := favoriteFixture(t)
	svc := NewService(db)
	group, err := svc.Create(c, 7, CreateRequest{Name: "重点", Channels: []ChannelInput{{DeviceCode: "D1", ChannelCode: "C1"}}})
	require.NoError(t, err)
	require.Equal(t, "重点", group.Name)
	result, err := svc.Append(c, 7, group.ID, []ChannelInput{{DeviceCode: "D1", ChannelCode: "C1"}, {DeviceCode: "D1", ChannelCode: "C2"}})
	require.NoError(t, err)
	require.Equal(t, AppendResult{RequestedCount: 2, AddedCount: 1, SkippedCount: 1}, result)
	groups, err := svc.List(c, 7)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Items, 2)
	require.Equal(t, 0, groups[0].UnavailableCount)
	_, err = svc.List(c, 8)
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannelFavoriteGroup{}).Where("owner_user_id = ?", 8).Count(&count).Error)
	require.Zero(t, count)
}

func TestChannelFavoriteServiceCreateIsAtomic(t *testing.T) {
	db, c := favoriteFixture(t)
	_, err := NewService(db).Create(c, 7, CreateRequest{Name: "失败组", Channels: []ChannelInput{{DeviceCode: "D1", ChannelCode: "C1"}, {DeviceCode: "NO", ChannelCode: "C9"}}})
	var domain *DomainError
	require.ErrorAs(t, err, &domain)
	require.Equal(t, ErrChannelNotVisible, domain.Code)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbChannelFavoriteGroup{}).Count(&count).Error)
	require.Zero(t, count)
}
