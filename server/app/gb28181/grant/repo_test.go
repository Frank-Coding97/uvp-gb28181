package grant

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newGrantTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceGrant{}, &gbmodels.GbDevice{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "d1", OwnerDeptID: 1}).Error)
	return db
}

// T1 RED-1: 创建成功
func TestRepoCreate_Inserts(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	created, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{
		DeviceID: 1, TargetType: "dept", TargetID: 2, CreatedBy: 1,
	})
	require.NoError(t, err)
	assert.True(t, created)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// T1 RED-2: 同键重复创建 → 跳过,不产生新行
func TestRepoCreate_DuplicateSkips(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	first, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 1, TargetType: "dept", TargetID: 2, CreatedBy: 1})
	require.NoError(t, err)
	require.True(t, first)

	second, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 1, TargetType: "dept", TargetID: 2, CreatedBy: 1})
	require.NoError(t, err)
	assert.False(t, second)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// T1 RED-3: 软删后同键再创建 → 恢复
func TestRepoCreate_RestoresSoftDeleted(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	created, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 1, TargetType: "dept", TargetID: 2, CreatedBy: 1})
	require.NoError(t, err)
	require.True(t, created)

	var first gbmodels.GbDeviceGrant
	require.NoError(t, db.First(&first).Error)
	require.NoError(t, repo.Remove(context.Background(), 1, first.ID))

	restored, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 1, TargetType: "dept", TargetID: 2, CreatedBy: 7})
	require.NoError(t, err)
	assert.True(t, restored)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceGrant{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)

	var current gbmodels.GbDeviceGrant
	require.NoError(t, db.First(&current).Error)
	assert.EqualValues(t, 7, current.CreatedBy, "恢复后应刷新操作人")
}

// T1 RED-4: 软删后 ListByDevice 不可见
func TestRepoListByDevice_ExcludesSoftDeleted(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	created, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 1, TargetType: "dept", TargetID: 2})
	require.NoError(t, err)
	require.True(t, created)

	list, err := repo.ListByDevice(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, repo.Remove(context.Background(), 1, list[0].ID))

	list, err = repo.ListByDevice(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, list)
}

// T1 RED-5: 批量创建 added/skipped 正确
func TestRepoCreateBatch_Counts(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	added, skipped, err := repo.CreateBatch(context.Background(), 1, []GrantTarget{
		{Type: "dept", ID: 2},
		{Type: "dept", ID: 3},
		{Type: "dept", ID: 2}, // 重复
	})
	require.NoError(t, err)
	assert.Equal(t, 2, added)
	assert.Equal(t, 1, skipped)
}

// T1 RED-6: 设备不存在 → ErrDeviceNotFound
func TestRepoCreate_DeviceMissing(t *testing.T) {
	db := newGrantTestDB(t)
	repo := NewRepo(db)

	_, err := repo.Create(context.Background(), &gbmodels.GbDeviceGrant{DeviceID: 999, TargetType: "dept", TargetID: 2})
	assert.True(t, errors.Is(err, ErrDeviceNotFound))
}
