package gb28181

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
)

type bootstrapWorkRecordingTestConfig struct{}

func (bootstrapWorkRecordingTestConfig) ConfigFileChangeListen(...func()) {}
func (bootstrapWorkRecordingTestConfig) Get(string) interface{}           { return nil }
func (bootstrapWorkRecordingTestConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (bootstrapWorkRecordingTestConfig) GetBool(string) bool              { return false }
func (bootstrapWorkRecordingTestConfig) GetInt(string) int                { return 0 }
func (bootstrapWorkRecordingTestConfig) GetInt32(string) int32            { return 0 }
func (bootstrapWorkRecordingTestConfig) GetInt64(string) int64            { return 0 }
func (bootstrapWorkRecordingTestConfig) GetFloat64(string) float64        { return 0 }
func (bootstrapWorkRecordingTestConfig) GetDuration(string) time.Duration { return 0 }
func (bootstrapWorkRecordingTestConfig) GetStringSlice(string) []string   { return nil }
func (bootstrapWorkRecordingTestConfig) GetUintSlice(string) []uint       { return nil }
func (bootstrapWorkRecordingTestConfig) Set(string, interface{})          {}
func (bootstrapWorkRecordingTestConfig) SaveConfig() error                { return nil }

func bootstrapWorkRecordingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bootstrap-work-recording.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	oldDB, oldConfig := app.GormDbMysql, app.ConfigYml
	app.GormDbMysql = db
	app.ConfigYml = bootstrapWorkRecordingTestConfig{}
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml = oldDB, oldConfig
		_ = raw.Close()
	})
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbRecordingSession{}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("bootstrap_work_recording:production_not_found", gormhelper.MaskNotDataError))
	return db
}

func TestBootstrapWorkRecordingQueriesHandleProductionNotFoundHook(t *testing.T) {
	bootstrapWorkRecordingDB(t)
	target := workrecording.MediaTarget{NodeID: 7, VHost: "v", App: "rtp", Stream: "stream", Generation: 1}

	err := checkLegacyRecordingIdle(context.Background(), 3020, target)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	prepare := prepareWorkRecording(nil, play.NewSourceLeaseRegistry())
	_, err = prepare(context.Background(), 3020, uuid.NewString())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestCheckLegacyRecordingIdleUsesRecordingSessionVHostColumn(t *testing.T) {
	db := bootstrapWorkRecordingDB(t)
	channel := models.GbChannel{DeviceID: "device", ChannelID: "channel", CloudRecordingEnabled: false}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecordingSession{
		ChannelID: channel.ID,
		DeviceID:  "device",
		NodeID:    7,
		VHost:     "v",
		App:       "rtp",
		Stream:    "stream",
		State:     models.RecordingSessionStateRecording,
	}).Error)

	err := checkLegacyRecordingIdle(context.Background(), channel.ID, workrecording.MediaTarget{NodeID: 7, VHost: "v", App: "rtp", Stream: "stream", Generation: 1})
	require.ErrorIs(t, err, workrecording.ErrOwnerConflict)
}
