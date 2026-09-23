package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

func newLifecycleStoreDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPlayAttempt{}, &gbmodels.GbPlayLifecycleEvent{}))
	return db
}

func TestPlayLifecycleStoreBeginAppendAndIdempotency(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	now := time.Date(2026, 9, 21, 1, 2, 3, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	event := play.LifecycleEvent{EventID: "media-1", EventAt: now.Add(time.Second), Stage: play.StageMedia, EventName: play.EventMediaReady, FactState: play.FactConfirmed, Source: play.SourcePlayService, StreamID: "S1", NodeID: 2}
	require.NoError(t, store.Append(context.Background(), id, event))
	require.NoError(t, store.Append(context.Background(), id, event))

	var attempt gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", id).First(&attempt).Error)
	require.Equal(t, string(play.MediaStateReady), attempt.MediaState)
	require.Equal(t, PlayOutcomeSuccess, attempt.Outcome)
	require.Equal(t, "S1", attempt.StreamID)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPlayLifecycleEvent{}).Where("lifecycle_id = ?", id).Count(&count).Error)
	require.EqualValues(t, 2, count, "request_received plus one idempotent media event")
}

func TestPlayLifecycleStorePersistsControlledEventMetadata(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]int64{"clientElapsedMs": 321})
	require.NoError(t, err)

	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "first-frame", Stage: play.StageClient, EventName: play.EventFirstFrame,
		FactState: play.FactConfirmed, Source: play.SourceClient, MetadataJSON: metadata,
	}))

	var event gbmodels.GbPlayLifecycleEvent
	require.NoError(t, db.Where("event_id = ?", "first-frame").First(&event).Error)
	require.JSONEq(t, `{"clientElapsedMs":321}`, string(event.MetadataJSON))
}

func TestPlayLifecycleStoreFailureKeepsStageAndReason(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "timeout-1", Stage: play.StageMedia, EventName: play.EventMediaTimeout, FactState: play.FactFailed,
		Source: play.SourcePlayService, ReasonCode: play.ReasonMediaTimeout,
	}))
	var attempt gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", id).First(&attempt).Error)
	require.Equal(t, PlayOutcomeFailure, attempt.Outcome)
	require.Equal(t, string(play.StageMedia), attempt.FailureStage)
	require.Equal(t, play.ReasonMediaTimeout, attempt.ReasonCode)
}

func TestPlayLifecycleStoreAppendByStreamDoesNotCreateOrphan(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)

	appended, err := store.AppendByStream(context.Background(), "missing-stream", 0, play.LifecycleEvent{
		EventID: "missing-stop", Stage: play.StageStop, EventName: play.EventStopRequested,
		FactState: play.FactConfirmed, Source: play.SourcePlayService,
	})
	require.NoError(t, err)
	require.False(t, appended)

	var attempts, events int64
	require.NoError(t, db.Model(&gbmodels.GbPlayAttempt{}).Count(&attempts).Error)
	require.NoError(t, db.Model(&gbmodels.GbPlayLifecycleEvent{}).Count(&events).Error)
	require.Zero(t, attempts)
	require.Zero(t, events)
}

func TestPlayLifecycleStoreAppendByStreamAndMarkStale(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "ready-stream", EventAt: now.Add(time.Second), Stage: play.StageMedia,
		EventName: play.EventMediaReady, FactState: play.FactConfirmed, Source: play.SourcePlayService,
		StreamID: "S1", NodeID: 9,
	}))

	appended, err := store.AppendByStream(context.Background(), "S1", 9, play.LifecycleEvent{
		EventID: "stop-stream", EventAt: now.Add(2 * time.Second), Stage: play.StageStop,
		EventName: play.EventStopRequested, FactState: play.FactConfirmed, Source: play.SourcePlayService,
	})
	require.NoError(t, err)
	require.True(t, appended)

	store.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	stale, err := store.MarkStale(context.Background(), now.Add(time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 1, stale)

	var attempt gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", id).First(&attempt).Error)
	require.Equal(t, string(play.LifecycleStateStale), attempt.LifecycleState)
	require.Equal(t, string(play.StageStop), attempt.CurrentStage, "stale marker must preserve the last business stage")
}

func TestPlayLifecycleStoreAppendByStreamPreservesTerminalFailure(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "media-failed", Stage: play.StageMedia, EventName: play.EventMediaTimeout,
		FactState: play.FactFailed, Source: play.SourcePlayService, ReasonCode: play.ReasonMediaTimeout, StreamID: "S1",
	}))

	appended, err := store.AppendByStream(context.Background(), "S1", 0, play.LifecycleEvent{
		EventID: "cleanup-after-failure", Stage: play.StageCleanup, EventName: play.EventCleanupCompleted,
		FactState: play.FactConfirmed, Source: play.SourcePlayService,
	})
	require.NoError(t, err)
	require.True(t, appended)

	var attempt gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", id).First(&attempt).Error)
	require.Equal(t, string(play.LifecycleStateFailed), attempt.LifecycleState)
	require.Equal(t, string(play.StageMedia), attempt.FailureStage)
	require.Equal(t, play.ReasonMediaTimeout, attempt.ReasonCode)
}

func TestPlayLifecycleStoreAppendClientEventIsIdempotentAndKeepsMediaFact(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "ready-client", Stage: play.StageMedia, EventName: play.EventMediaReady,
		FactState: play.FactConfirmed, Source: play.SourcePlayService, StreamID: "S1",
	}))
	firstFrame := play.LifecycleEvent{Stage: play.StageClient, EventName: play.EventFirstFrame, FactState: play.FactConfirmed, Source: play.SourceClient}
	require.NoError(t, store.AppendClientEvent(context.Background(), id, 7, "D1", "C1", firstFrame))
	require.NoError(t, store.AppendClientEvent(context.Background(), id, 7, "D1", "C1", firstFrame))
	require.NoError(t, store.AppendClientEvent(context.Background(), id, 7, "D1", "C1", play.LifecycleEvent{
		Stage: play.StageClient, EventName: play.EventPlayerError, FactState: play.FactFailed,
		Source: play.SourceClient, ReasonCode: play.ReasonPlayerTimeout,
	}))

	var attempt gbmodels.GbPlayAttempt
	require.NoError(t, db.Where("correlation_id = ?", id).First(&attempt).Error)
	require.Equal(t, string(play.MediaStateReady), attempt.MediaState)
	require.Equal(t, string(play.ClientStateFailed), attempt.ClientState)
	require.Equal(t, string(play.LifecycleStateInProgress), attempt.LifecycleState)
	require.Equal(t, PlayOutcomeSuccess, attempt.Outcome)
	var firstFrameCount int64
	require.NoError(t, db.Model(&gbmodels.GbPlayLifecycleEvent{}).
		Where("lifecycle_id = ? AND event_name = ?", id, play.EventFirstFrame).Count(&firstFrameCount).Error)
	require.EqualValues(t, 1, firstFrameCount)
}

func TestPlayLifecycleStoreAppendClientEventRejectsIdentityMismatch(t *testing.T) {
	db := newLifecycleStoreDB(t)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	err = store.AppendClientEvent(context.Background(), id, 8, "D1", "C1", play.LifecycleEvent{
		Stage: play.StageClient, EventName: play.EventFirstFrame, FactState: play.FactConfirmed, Source: play.SourceClient,
	})
	require.ErrorIs(t, err, ErrPlayLifecycleNotFound)
}

func TestPlayLifecycleStoreListDefaultsToTenAndAppliesScope(t *testing.T) {
	db := newLifecycleStoreDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	require.NoError(t, db.Create(&[]gbmodels.GbDevice{
		{DeviceID: "D1", Name: "visible"}, {DeviceID: "D2", Name: "hidden"},
	}).Error)
	base := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 15; i++ {
		device := "D1"
		if i == 14 {
			device = "D2"
		}
		at := base.Add(time.Duration(i) * time.Minute)
		require.NoError(t, db.Create(&gbmodels.GbPlayAttempt{
			CorrelationID: fmt.Sprintf("life-%02d", i), DeviceCode: device, ChannelCode: "C1",
			NodeID: 7, StreamID: "stream-filter", MediaState: string(play.MediaStateReady),
			ClientState: string(play.ClientStateUnknown), LifecycleState: string(play.LifecycleStateInProgress),
			Outcome: PlayOutcomeSuccess, CurrentStage: string(play.StageMedia), StartedAt: at, LastEventAt: &at,
		}).Error)
	}

	page, err := NewPlayLifecycleStore(db).List(context.Background(), PlayLifecycleQuery{
		NodeID: 7, StreamID: "stream-filter", MediaState: string(play.MediaStateReady),
	}, func(query *gorm.DB) *gorm.DB { return query.Where("gb_device.device_id = ?", "D1") })
	require.NoError(t, err)
	require.EqualValues(t, 14, page.Total)
	require.Equal(t, 10, page.PageSize)
	require.Len(t, page.List, 10)
	require.Equal(t, "life-13", page.List[0].LifecycleID)
}

func TestPlayLifecycleStoreDetailUsesStableSequenceAndScope(t *testing.T) {
	db := newLifecycleStoreDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "D1", Name: "visible"}).Error)
	store := NewPlayLifecycleStore(db)
	id, err := store.Begin(context.Background(), 7, "D1", "C1")
	require.NoError(t, err)
	require.NoError(t, store.Append(context.Background(), id, play.LifecycleEvent{
		EventID: "second", Stage: play.StageMedia, EventName: play.EventMediaReady,
		FactState: play.FactConfirmed, Source: play.SourcePlayService,
	}))

	detail, err := store.Detail(context.Background(), id, func(query *gorm.DB) *gorm.DB {
		return query.Where("gb_device.device_id = ?", "D1")
	})
	require.NoError(t, err)
	require.Equal(t, id, detail.Lifecycle.LifecycleID)
	require.Len(t, detail.Events, 2)
	require.EqualValues(t, 1, detail.Events[0].Sequence)
	require.EqualValues(t, 2, detail.Events[1].Sequence)
	require.Empty(t, detail.Events[0].ReasonMessage)

	_, err = store.Detail(context.Background(), id, func(query *gorm.DB) *gorm.DB {
		return query.Where("gb_device.device_id = ?", "D2")
	})
	require.ErrorIs(t, err, ErrPlayLifecycleNotFound)
}
