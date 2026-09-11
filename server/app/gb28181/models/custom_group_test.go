package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newCustomGroupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbCustomGroup{}, &gbmodels.GbCustomGroupDevice{}))
	return db
}

func TestCustomGroupModelContract(t *testing.T) {
	db := newCustomGroupTestDB(t)
	migrator := db.Migrator()
	require.True(t, migrator.HasTable(&gbmodels.GbCustomGroup{}))
	require.True(t, migrator.HasIndex(&gbmodels.GbCustomGroup{}, "uk_custom_group_sibling_name"))
	require.True(t, migrator.HasIndex(&gbmodels.GbCustomGroup{}, "idx_custom_group_dept_path"))
	require.True(t, migrator.HasIndex(&gbmodels.GbCustomGroupDevice{}, "uk_custom_group_device"))

	root := gbmodels.GbCustomGroup{OwnerDeptID: 10, ParentID: 0, Path: "/1/", Name: "东区", CreatedBy: 7}
	require.NoError(t, db.Create(&root).Error)
	require.Error(t, db.Create(&gbmodels.GbCustomGroup{OwnerDeptID: 10, ParentID: 0, Path: "/2/", Name: "东区", CreatedBy: 7}).Error)
	require.NoError(t, db.Create(&gbmodels.GbCustomGroup{OwnerDeptID: 10, ParentID: root.ID, Path: "/1/3/", Depth: 1, Name: "东区", CreatedBy: 7}).Error)
}

func TestGroupDeviceModelContract(t *testing.T) {
	db := newCustomGroupTestDB(t)
	require.NoError(t, db.Create(&gbmodels.GbCustomGroupDevice{GroupID: 1, DeviceID: 9, CreatedBy: 7}).Error)
	require.Error(t, db.Create(&gbmodels.GbCustomGroupDevice{GroupID: 1, DeviceID: 9, CreatedBy: 8}).Error)
	require.NoError(t, db.Create(&gbmodels.GbCustomGroupDevice{GroupID: 2, DeviceID: 9, CreatedBy: 7}).Error)
}
