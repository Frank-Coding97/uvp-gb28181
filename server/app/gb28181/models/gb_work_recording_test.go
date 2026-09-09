package models

import (
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"testing"
)

func TestWorkRecordingSchemaPersistenceAndUniqueness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.db")
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		require.NoError(t, err)
		return db
	}
	db := open()
	for i := 0; i < 2; i++ {
		require.NoError(t, db.AutoMigrate(&GbWorkRecording{}, &GbRecorderClaim{}, &GbWorkRecordingFile{}))
	}
	job := GbWorkRecording{ID: "job-1", ChannelID: 1, CreatedBy: 2, RequestID: "request-1", State: "unknown", Version: 1, DeviceID: "00000000000000000001", FormJSON: `{"projectName":"测试项目"}`}
	require.NoError(t, db.Create(&job).Error)
	duplicate := job
	duplicate.ID = "job-2"
	require.Error(t, db.Create(&duplicate).Error)
	claim := GbRecorderClaim{ResourceKey: "channel-1", OwnerKind: "work_job", OwnerID: job.ID, State: "starting", Version: 1}
	require.NoError(t, db.Create(&claim).Error)
	claim.OwnerID = "other"
	require.Error(t, db.Create(&claim).Error)
	link := GbWorkRecordingFile{FileID: 1, WorkRecordingID: job.ID}
	require.NoError(t, db.Create(&link).Error)
	link.WorkRecordingID = "job-2"
	require.Error(t, db.Create(&link).Error)
	raw, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db = open()
	raw, err = db.DB()
	require.NoError(t, err)
	defer raw.Close()
	var restored GbWorkRecording
	require.NoError(t, db.First(&restored, "id = ?", job.ID).Error)
	require.Equal(t, job.DeviceID, restored.DeviceID)
	require.Equal(t, job.FormJSON, restored.FormJSON)
	require.Equal(t, "unknown", restored.State)
}
