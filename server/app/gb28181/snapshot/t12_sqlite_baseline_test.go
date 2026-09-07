package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newSnapshotSQLiteBaselineDB(t *testing.T) (*gorm.DB, *gbmodels.GbDevice, *gbmodels.GbChannel) {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "snapshot.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.NoError(t, sqlitebootstrap.Migrate(context.Background(), db))

	device := &gbmodels.GbDevice{DeviceID: "34020000001320007890", Name: "T12 snapshot device"}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		DeviceID: device.DeviceID, ChannelID: "34020000001310007890", Name: "T12 snapshot channel",
		Status: gbmodels.ChannelStatusOnline,
	}
	require.NoError(t, db.Create(channel).Error)
	return db, device, channel
}

func TestT12SnapshotSQLiteBaselineCapturesAndPersistsChannel(t *testing.T) {
	db, device, channel := newSnapshotSQLiteBaselineDB(t)
	tmpDir := t.TempDir()
	client := &fakeZLMClient{returnMe: []byte{0xff, 0xd8, 0xff, 't', '1', '2'}}
	service := New(Config{
		UploadRoot:  tmpDir,
		URLPrefix:   "/uploads",
		DelayBefore: time.Millisecond,
		ZLMTimeout:  1,
		ZLMExpire:   1,
		GetClient: func(string) (ZLMClient, error) {
			return client, nil
		},
		BuildStreamURL: func(context.Context, string, string, string) (string, error) {
			return "rtsp://fixture/t12-stream", nil
		},
		Repo: NewGormRepo(db),
	})

	err := service.doCapture("node-1", "t12-stream", device.DeviceID, channel.ChannelID, "token")
	require.NoError(t, err)

	var stored gbmodels.GbChannel
	require.NoError(t, db.Where("id = ?", channel.ID).First(&stored).Error)
	require.Contains(t, stored.SnapshotURL, "/uploads/gb-channel-snapshot/")
	require.NotNil(t, stored.SnapshotAt)
	path := filepath.Join(tmpDir, "gb-channel-snapshot", stored.SnapshotAt.Format("2006-01"), device.DeviceID+"_"+channel.ChannelID+".jpg")
	bytes, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte{0xff, 0xd8, 0xff, 't', '1', '2'}, bytes)
}

func TestT12SnapshotSQLiteBaselineRejectsEmptyCaptureAndCanceledRepoWrite(t *testing.T) {
	db, device, channel := newSnapshotSQLiteBaselineDB(t)
	client := &fakeZLMClient{}
	service := New(Config{
		UploadRoot:  t.TempDir(),
		DelayBefore: time.Millisecond,
		ZLMTimeout:  1,
		GetClient: func(string) (ZLMClient, error) {
			return client, nil
		},
		BuildStreamURL: func(context.Context, string, string, string) (string, error) {
			return "rtsp://fixture/t12-empty", nil
		},
		Repo: NewGormRepo(db),
	})
	require.ErrorContains(t, service.doCapture("", "t12-empty", device.DeviceID, channel.ChannelID, ""), "空图片")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	err := NewGormRepo(db).UpdateSnapshot(canceled, device.DeviceID, channel.ChannelID, "/uploads/canceled.jpg", time.Now())
	require.ErrorIs(t, err, context.Canceled)
}
