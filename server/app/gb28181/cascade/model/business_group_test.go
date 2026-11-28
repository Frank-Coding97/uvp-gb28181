package model_test

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

func TestCascadeBusinessGroupModelsHavePlatformScopedUniqueKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.GbCascadeBusinessNode{}, &model.GbCascadeBusinessBinding{}))

	require.Equal(t, "gb_cascade_business_node", model.GbCascadeBusinessNode{}.TableName())
	require.Equal(t, "gb_cascade_business_binding", model.GbCascadeBusinessBinding{}.TableName())
	require.True(t, db.Migrator().HasIndex(&model.GbCascadeBusinessNode{}, "uk_cascade_business_node_code"))
	require.True(t, db.Migrator().HasIndex(&model.GbCascadeBusinessBinding{}, "uk_cascade_business_binding_channel"))

	first := model.GbCascadeBusinessNode{PlatformID: 1, Code: "34020000002150000001", Name: "业务根", Kind: "group"}
	require.NoError(t, db.Create(&first).Error)
	require.Error(t, db.Create(&model.GbCascadeBusinessNode{PlatformID: 1, Code: first.Code, Name: "重复", Kind: "group"}).Error)
	require.NoError(t, db.Create(&model.GbCascadeBusinessNode{PlatformID: 2, Code: first.Code, Name: "另一平台", Kind: "group"}).Error)

	require.NoError(t, db.Create(&model.GbCascadeBusinessBinding{PlatformID: 1, SourceChannelID: 11, NodeCode: "34020000002160000001"}).Error)
	require.Error(t, db.Create(&model.GbCascadeBusinessBinding{PlatformID: 1, SourceChannelID: 11, NodeCode: "34020000002160000002"}).Error)
	require.NoError(t, db.Create(&model.GbCascadeBusinessBinding{PlatformID: 2, SourceChannelID: 11, NodeCode: "34020000002160000001"}).Error)
}
