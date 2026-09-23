package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordingModelsSchemaAndDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbChannel{}, &GbRecordingSession{}, &GbRecordingFile{}))

	for _, column := range []string{"cloud_recording_enabled", "cloud_recording_state", "cloud_recording_error", "cloud_recording_updated_at"} {
		require.True(t, db.Migrator().HasColumn(&GbChannel{}, column), column)
	}

	channel := &GbChannel{DeviceID: "device-1", ChannelID: "channel-1"}
	require.NoError(t, db.Create(channel).Error)
	require.False(t, channel.CloudRecordingEnabled)
	require.Equal(t, CloudRecordingStateDisabled, channel.CloudRecordingState)
	require.Empty(t, channel.CloudRecordingError)
}

func TestRecordingModelsUniqueKeys(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbRecordingSession{}, &GbRecordingFile{}))

	now := time.Now()
	session := &GbRecordingSession{ChannelID: 1, DeviceID: "device", NodeID: 2, VHost: DefaultRecordingVHost, App: DefaultRecordingApp, Stream: "stream", State: RecordingSessionStateRecording, StartedAt: &now}
	require.NoError(t, db.Create(session).Error)
	duplicateSession := *session
	duplicateSession.ID = 0
	require.Error(t, db.Create(&duplicateSession).Error)

	file := &GbRecordingFile{ChannelID: 1, DeviceID: "device", NodeID: 2, VHost: DefaultRecordingVHost, App: DefaultRecordingApp, Stream: "stream", FilePath: "/record/one.mp4", StartTime: &now}
	require.NoError(t, db.Create(file).Error)
	duplicateFile := *file
	duplicateFile.ID = 0
	require.Error(t, db.Create(&duplicateFile).Error)
}

func TestRecordingFileCatalogFieldsKeepUnknownMetadataNullableAndInternalPathsPrivate(t *testing.T) {
	typeOfFile := reflect.TypeOf(GbRecordingFile{})
	for _, name := range []string{
		"FileKey", "ChannelCode", "ChannelName", "DeviceName", "OwnerDeptID", "Source", "MetadataState",
		"RecordDate", "DiscoveredAt", "LastSeenAt", "MissingAt", "ReconcileMissCount", "UpdatedAt",
	} {
		if _, ok := typeOfFile.FieldByName(name); !ok {
			t.Errorf("GbRecordingFile missing catalog field %s", name)
		}
	}
	for _, name := range []string{"StartTime", "TimeLen", "FileSize"} {
		field, ok := typeOfFile.FieldByName(name)
		if !ok {
			t.Errorf("GbRecordingFile missing nullable metadata field %s", name)
			continue
		}
		if field.Type.Kind() != reflect.Ptr {
			t.Errorf("GbRecordingFile.%s must be nullable, got %s", name, field.Type)
		}
	}

	body, err := json.Marshal(GbRecordingFile{FilePath: "/private/a.mp4", Folder: "/private", URL: "http://internal/a.mp4"})
	require.NoError(t, err)
	require.NotContains(t, string(body), "filePath")
	require.NotContains(t, string(body), "folder")
	require.NotContains(t, string(body), "url")
}
