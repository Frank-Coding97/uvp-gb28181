package models

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestChannelFavoriteModelsConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbChannelFavoriteGroup{}, &GbChannelFavoriteItem{}))
	group := GbChannelFavoriteGroup{OwnerUserID: 7, Name: "重点通道"}
	require.NoError(t, db.Create(&group).Error)
	require.Error(t, db.Create(&GbChannelFavoriteGroup{OwnerUserID: 7, Name: "重点通道"}).Error)
	require.NoError(t, db.Create(&GbChannelFavoriteGroup{OwnerUserID: 8, Name: "重点通道"}).Error)
	item := GbChannelFavoriteItem{GroupID: group.ID, DeviceCode: "D1", ChannelCode: "C1"}
	require.NoError(t, db.Create(&item).Error)
	require.Error(t, db.Create(&GbChannelFavoriteItem{GroupID: group.ID, DeviceCode: "D1", ChannelCode: "C1"}).Error)
	for _, index := range []string{"uk_gb_channel_favorite_group_owner_name", "idx_gb_channel_favorite_group_owner", "uk_gb_channel_favorite_item_code", "idx_gb_channel_favorite_item_group"} {
		if index == "idx_gb_channel_favorite_group_owner" {
			require.True(t, db.Migrator().HasIndex(&GbChannelFavoriteGroup{}, index))
		} else if index == "uk_gb_channel_favorite_group_owner_name" {
			require.True(t, db.Migrator().HasIndex(&GbChannelFavoriteGroup{}, index))
		} else {
			require.True(t, db.Migrator().HasIndex(&GbChannelFavoriteItem{}, index))
		}
	}
}
