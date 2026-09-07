package models

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newModelsSQLiteBaselineDB(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	return db, path
}

func TestUpsertChannelSQLiteBaselineUsesNaturalKey(t *testing.T) {
	db, path := newModelsSQLiteBaselineDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first := &GbChannel{DeviceID: "sqlite-device", ChannelID: "sqlite-ch-1", Name: "first", Status: ChannelStatusOnline}
	require.NoError(t, upsertChannel(ctx, db, first))
	firstID := first.ID
	second := &GbChannel{DeviceID: first.DeviceID, ChannelID: first.ChannelID, Name: "updated", Status: ChannelStatusOffline}
	require.NoError(t, upsertChannel(ctx, db, second))
	require.Equal(t, firstID, second.ID)

	var count int64
	require.NoError(t, db.Model(&GbChannel{}).Where("device_id = ? AND channel_id = ?", first.DeviceID, first.ChannelID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var stored GbChannel
	require.NoError(t, db.Where("id = ?", firstID).First(&stored).Error)
	require.Equal(t, "updated", stored.Name)
	require.Equal(t, ChannelStatusOffline, stored.Status)

	secondDB, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	rawSecond, err := secondDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawSecond.Close() })
	start := make(chan struct{})
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, client := range []*gorm.DB{db, secondDB} {
		wg.Add(1)
		go func(client *gorm.DB) {
			defer wg.Done()
			<-start
			errors <- upsertChannel(ctx, client, &GbChannel{DeviceID: "sqlite-device", ChannelID: "sqlite-ch-2", Name: "parallel"})
		}(client)
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.NoError(t, db.Model(&GbChannel{}).Where("device_id = ? AND channel_id = ?", "sqlite-device", "sqlite-ch-2").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
