package models

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPlayLifecycleModelsAutoMigrateContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbPlayAttempt{}, &GbPlayLifecycleEvent{}))

	for _, column := range []string{
		"stream_id", "ssrc", "call_id", "cseq", "current_stage", "media_state", "client_state",
		"lifecycle_state", "reason_code", "reason_message", "client_first_frame_at", "client_error_at",
		"client_error_code", "last_event_at",
	} {
		require.Truef(t, db.Migrator().HasColumn(&GbPlayAttempt{}, column), "missing attempt column %s", column)
	}
	for _, column := range []string{
		"event_id", "lifecycle_id", "sequence", "event_at", "elapsed_ms", "stage", "event_name",
		"fact_state", "source", "device_code", "channel_code", "stream_id", "node_id", "ssrc",
		"reused", "call_id", "cseq", "reason_code", "reason_message", "metadata_json", "created_at",
	} {
		require.Truef(t, db.Migrator().HasColumn(&GbPlayLifecycleEvent{}, column), "missing event column %s", column)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	event := GbPlayLifecycleEvent{
		EventID: "event-1", LifecycleID: "life-1", Sequence: 1, EventAt: now, Stage: "request",
		EventName: "request_received", FactState: "confirmed", Source: "http", MetadataJSON: []byte(`{"trigger":"explicit"}`),
	}
	require.NoError(t, db.Create(&event).Error)
	var loaded GbPlayLifecycleEvent
	require.NoError(t, db.Where("event_id = ?", event.EventID).First(&loaded).Error)
	require.JSONEq(t, string(event.MetadataJSON), string(loaded.MetadataJSON))
	require.Error(t, db.Create(&GbPlayLifecycleEvent{EventID: event.EventID, LifecycleID: "life-2", Sequence: 1, EventAt: now}).Error)
}
