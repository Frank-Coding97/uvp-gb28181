package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

var ErrPlayLifecycleNotFound = errors.New("play lifecycle not found")

type PlayLifecycleStore struct {
	db    *gorm.DB
	clock func() time.Time
}

type PlayLifecycleQuery struct {
	Page           int
	PageSize       int
	From           *time.Time
	To             *time.Time
	DeviceCode     string
	ChannelCode    string
	StreamID       string
	NodeID         int64
	LifecycleState string
	MediaState     string
	ClientState    string
	FailureStage   string
}

type PlayLifecycleSummary struct {
	LifecycleID    string     `json:"lifecycleId" gorm:"column:correlation_id"`
	DeviceCode     string     `json:"deviceCode" gorm:"column:device_code"`
	ChannelCode    string     `json:"channelCode" gorm:"column:channel_code"`
	NodeID         int64      `json:"nodeId" gorm:"column:node_id"`
	Reused         bool       `json:"reused" gorm:"column:reused"`
	StreamID       string     `json:"streamId" gorm:"column:stream_id"`
	SSRC           string     `json:"ssrc" gorm:"column:ssrc"`
	CurrentStage   string     `json:"currentStage" gorm:"column:current_stage"`
	MediaState     string     `json:"mediaState" gorm:"column:media_state"`
	ClientState    string     `json:"clientState" gorm:"column:client_state"`
	LifecycleState string     `json:"lifecycleState" gorm:"column:lifecycle_state"`
	FailureStage   string     `json:"failureStage,omitempty" gorm:"column:failure_stage"`
	ReasonCode     string     `json:"reasonCode,omitempty" gorm:"column:reason_code"`
	ReasonMessage  string     `json:"reasonMessage,omitempty" gorm:"column:reason_message"`
	StartedAt      time.Time  `json:"startedAt" gorm:"column:started_at"`
	LastEventAt    *time.Time `json:"lastEventAt,omitempty" gorm:"column:last_event_at"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty" gorm:"column:finished_at"`
}

type PlayLifecyclePage struct {
	List     []PlayLifecycleSummary `json:"list"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

type PlayLifecycleEventView struct {
	Sequence      int64     `json:"sequence" gorm:"column:sequence"`
	EventAt       time.Time `json:"eventAt" gorm:"column:event_at"`
	ElapsedMS     int64     `json:"elapsedMs" gorm:"column:elapsed_ms"`
	Stage         string    `json:"stage" gorm:"column:stage"`
	EventName     string    `json:"eventName" gorm:"column:event_name"`
	FactState     string    `json:"factState" gorm:"column:fact_state"`
	Source        string    `json:"source" gorm:"column:source"`
	StreamID      string    `json:"streamId,omitempty" gorm:"column:stream_id"`
	NodeID        int64     `json:"nodeId,omitempty" gorm:"column:node_id"`
	SSRC          string    `json:"ssrc,omitempty" gorm:"column:ssrc"`
	Reused        bool      `json:"reused" gorm:"column:reused"`
	CallID        string    `json:"callId,omitempty" gorm:"column:call_id"`
	CSeq          string    `json:"cseq,omitempty" gorm:"column:cseq"`
	ReasonCode    string    `json:"reasonCode,omitempty" gorm:"column:reason_code"`
	ReasonMessage string    `json:"reasonMessage,omitempty" gorm:"column:reason_message"`
}

type PlayLifecycleDetail struct {
	Lifecycle PlayLifecycleSummary     `json:"lifecycle"`
	Events    []PlayLifecycleEventView `json:"events"`
}

func NewPlayLifecycleStore(db *gorm.DB) *PlayLifecycleStore {
	return &PlayLifecycleStore{db: db, clock: time.Now}
}

func (store *PlayLifecycleStore) SetClock(clock func() time.Time) {
	if clock != nil {
		store.clock = clock
	}
}

func (store *PlayLifecycleStore) Begin(ctx context.Context, userID uint, deviceID, channelID string) (string, error) {
	id, err := newLifecycleID()
	if err != nil {
		return "", err
	}
	now := store.clock()
	attempt := gbmodels.GbPlayAttempt{
		CorrelationID: id, UserID: userID, DeviceCode: deviceID, ChannelCode: channelID,
		Outcome: PlayOutcomeStarted, CurrentStage: string(play.StageRequest), MediaState: string(play.MediaStateUnknown),
		ClientState: string(play.ClientStateUnknown), LifecycleState: string(play.LifecycleStateInProgress),
		StartedAt: now, LastEventAt: &now,
	}
	event := play.LifecycleEvent{
		EventID: id + ":request", EventAt: now, Stage: play.StageRequest, EventName: play.EventRequestReceived,
		FactState: play.FactConfirmed, Source: play.SourceHTTP, DeviceCode: deviceID, ChannelCode: channelID,
	}
	err = store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		return tx.Create(lifecycleEventModel(id, 1, event, now)).Error
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (store *PlayLifecycleStore) Append(ctx context.Context, lifecycleID string, event play.LifecycleEvent) error {
	if lifecycleID == "" || event.EventID == "" {
		return errors.New("lifecycle_id and event_id are required")
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var duplicate int64
		if err := tx.Model(&gbmodels.GbPlayLifecycleEvent{}).Where("event_id = ?", event.EventID).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return nil
		}

		var attempt gbmodels.GbPlayAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("correlation_id = ?", lifecycleID).First(&attempt).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrPlayLifecycleNotFound
			}
			return err
		}
		snapshot := snapshotFromAttempt(attempt)
		if event.EventAt.IsZero() {
			event.EventAt = store.clock()
		}
		if event.DeviceCode == "" {
			event.DeviceCode = attempt.DeviceCode
		}
		if event.ChannelCode == "" {
			event.ChannelCode = attempt.ChannelCode
		}
		if err := snapshot.Apply(event); err != nil {
			return err
		}
		normalized := snapshot.Events[len(snapshot.Events)-1]
		var sequence int64
		if err := tx.Model(&gbmodels.GbPlayLifecycleEvent{}).Where("lifecycle_id = ?", lifecycleID).Select("COALESCE(MAX(sequence), 0)").Scan(&sequence).Error; err != nil {
			return err
		}
		if err := tx.Create(lifecycleEventModel(lifecycleID, sequence+1, normalized, store.clock())).Error; err != nil {
			return err
		}
		return updateAttemptSnapshot(tx, &attempt, snapshot, normalized)
	})
}

func (store *PlayLifecycleStore) AppendClientEvent(
	ctx context.Context,
	lifecycleID string,
	userID uint,
	deviceID, channelID string,
	event play.LifecycleEvent,
) error {
	var attempt gbmodels.GbPlayAttempt
	err := store.db.WithContext(ctx).
		Where("correlation_id = ? AND user_id = ? AND device_code = ? AND channel_code = ?", lifecycleID, userID, deviceID, channelID).
		First(&attempt).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPlayLifecycleNotFound
		}
		return err
	}
	if event.EventID == "" {
		event.EventID = lifecycleID + ":" + event.EventName
	}
	event.DeviceCode, event.ChannelCode = deviceID, channelID
	return store.Append(ctx, lifecycleID, event)
}

func (store *PlayLifecycleStore) List(ctx context.Context, request PlayLifecycleQuery, scope QueryScope) (PlayLifecyclePage, error) {
	request.Page, request.PageSize = normalizeLifecyclePage(request.Page, request.PageSize)
	query := store.db.WithContext(ctx).Table("gb_play_attempt AS attempt").
		Joins("JOIN gb_device ON gb_device.device_id = attempt.device_code AND gb_device.deleted_at IS NULL")
	if scope != nil {
		query = scope(query)
	}
	query = applyLifecycleFilters(query, request)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return PlayLifecyclePage{}, err
	}
	list := make([]PlayLifecycleSummary, 0, request.PageSize)
	if err := query.Select("attempt.correlation_id, attempt.device_code, attempt.channel_code, attempt.node_id, attempt.reused, attempt.stream_id, attempt.ssrc, attempt.current_stage, attempt.media_state, attempt.client_state, attempt.lifecycle_state, attempt.failure_stage, attempt.reason_code, attempt.reason_message, attempt.started_at, attempt.last_event_at, attempt.finished_at").
		Order("COALESCE(attempt.last_event_at, attempt.started_at) DESC, attempt.id DESC").
		Offset((request.Page - 1) * request.PageSize).Limit(request.PageSize).Scan(&list).Error; err != nil {
		return PlayLifecyclePage{}, err
	}
	return PlayLifecyclePage{List: list, Total: total, Page: request.Page, PageSize: request.PageSize}, nil
}

func (store *PlayLifecycleStore) Detail(ctx context.Context, lifecycleID string, scope QueryScope) (PlayLifecycleDetail, error) {
	query := store.db.WithContext(ctx).Table("gb_play_attempt AS attempt").
		Joins("JOIN gb_device ON gb_device.device_id = attempt.device_code AND gb_device.deleted_at IS NULL")
	if scope != nil {
		query = scope(query)
	}
	var lifecycle PlayLifecycleSummary
	err := query.Select("attempt.correlation_id, attempt.device_code, attempt.channel_code, attempt.node_id, attempt.reused, attempt.stream_id, attempt.ssrc, attempt.current_stage, attempt.media_state, attempt.client_state, attempt.lifecycle_state, attempt.failure_stage, attempt.reason_code, attempt.reason_message, attempt.started_at, attempt.last_event_at, attempt.finished_at").
		Where("attempt.correlation_id = ?", lifecycleID).Limit(1).Scan(&lifecycle).Error
	if err != nil {
		return PlayLifecycleDetail{}, err
	}
	if lifecycle.LifecycleID == "" {
		return PlayLifecycleDetail{}, ErrPlayLifecycleNotFound
	}
	events := make([]PlayLifecycleEventView, 0)
	err = store.db.WithContext(ctx).Table("gb_play_lifecycle_event").
		Select("sequence, event_at, elapsed_ms, stage, event_name, fact_state, source, stream_id, node_id, ssrc, reused, call_id, cseq, reason_code, reason_message").
		Where("lifecycle_id = ?", lifecycleID).Order("sequence ASC, event_at ASC, id ASC").Scan(&events).Error
	if err != nil {
		return PlayLifecycleDetail{}, err
	}
	return PlayLifecycleDetail{Lifecycle: lifecycle, Events: events}, nil
}

func normalizeLifecyclePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func applyLifecycleFilters(query *gorm.DB, request PlayLifecycleQuery) *gorm.DB {
	if request.From != nil {
		query = query.Where("attempt.started_at >= ?", *request.From)
	}
	if request.To != nil {
		query = query.Where("attempt.started_at <= ?", *request.To)
	}
	for column, value := range map[string]string{
		"attempt.device_code": request.DeviceCode, "attempt.channel_code": request.ChannelCode,
		"attempt.stream_id": request.StreamID, "attempt.lifecycle_state": request.LifecycleState,
		"attempt.media_state": request.MediaState, "attempt.client_state": request.ClientState,
		"attempt.failure_stage": request.FailureStage,
	} {
		if value != "" {
			query = query.Where(column+" = ?", value)
		}
	}
	if request.NodeID != 0 {
		query = query.Where("attempt.node_id = ?", request.NodeID)
	}
	return query
}

// AppendByStream attaches an event to the latest still-running lifecycle for a
// stream. A missing stream is intentionally a no-op: stop/cleanup callbacks can
// arrive after their lifecycle has already been removed or never recorded.
func (store *PlayLifecycleStore) AppendByStream(ctx context.Context, streamID string, nodeID int64, event play.LifecycleEvent) (bool, error) {
	if streamID == "" {
		return false, nil
	}
	query := store.db.WithContext(ctx).Where(
		"stream_id = ? AND lifecycle_state IN ?",
		streamID,
		[]string{string(play.LifecycleStateInProgress), string(play.LifecycleStateFailed)},
	)
	if nodeID != 0 {
		query = query.Where("node_id = ?", nodeID)
	}
	var attempt gbmodels.GbPlayAttempt
	if err := query.Order("COALESCE(last_event_at, started_at) DESC, id DESC").First(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if event.StreamID == "" {
		event.StreamID = streamID
	}
	if event.EventID == "" {
		event.EventID = attempt.CorrelationID + ":" + event.EventName
	}
	if event.NodeID == 0 {
		event.NodeID = attempt.NodeID
	}
	if err := store.Append(ctx, attempt.CorrelationID, event); err != nil {
		return false, err
	}
	return true, nil
}

// MarkStale closes lifecycles that have not produced an event since cutoff.
// The stale event uses the last business stage so operational cleanup does not
// overwrite the stage at which the playback actually stopped progressing.
func (store *PlayLifecycleStore) MarkStale(ctx context.Context, cutoff time.Time) (int64, error) {
	var attempts []gbmodels.GbPlayAttempt
	query := store.db.WithContext(ctx).Where(
		"lifecycle_state = ? AND COALESCE(last_event_at, started_at) < ?",
		string(play.LifecycleStateInProgress), cutoff,
	)
	if store.db.Migrator().HasTable(&gbmodels.GbChannel{}) {
		query = query.Where("(stream_id = '' OR NOT EXISTS (SELECT 1 FROM gb_channel WHERE gb_channel.stream_id = gb_play_attempt.stream_id))")
	}
	if err := query.Order("id ASC").Find(&attempts).Error; err != nil {
		return 0, err
	}
	var marked int64
	for _, attempt := range attempts {
		eventAt := store.clock()
		event := play.LifecycleEvent{
			EventID:     "stale:" + attempt.CorrelationID,
			EventAt:     eventAt,
			Stage:       play.LifecycleStage(attempt.CurrentStage),
			EventName:   play.EventStaleInProgress,
			FactState:   play.FactUnknown,
			Source:      play.SourceRetention,
			DeviceCode:  attempt.DeviceCode,
			ChannelCode: attempt.ChannelCode,
			StreamID:    attempt.StreamID,
			NodeID:      attempt.NodeID,
			ReasonCode:  play.ReasonStaleInProgress,
		}
		if err := store.Append(ctx, attempt.CorrelationID, event); err != nil {
			return marked, err
		}
		marked++
	}
	return marked, nil
}

func snapshotFromAttempt(attempt gbmodels.GbPlayAttempt) *play.LifecycleSnapshot {
	snapshot := play.NewLifecycleSnapshot(attempt.CorrelationID, attempt.UserID, attempt.DeviceCode, attempt.ChannelCode)
	snapshot.StreamID, snapshot.NodeID, snapshot.SSRC = attempt.StreamID, attempt.NodeID, attempt.SSRC
	snapshot.Reused, snapshot.CallID, snapshot.CSeq = attempt.Reused, attempt.CallID, attempt.CSeq
	snapshot.CurrentStage, snapshot.FailureStage = play.LifecycleStage(attempt.CurrentStage), play.LifecycleStage(attempt.FailureStage)
	snapshot.ReasonCode, snapshot.ReasonMessage = attempt.ReasonCode, attempt.ReasonMessage
	snapshot.LifecycleState = play.LifecycleState(attempt.LifecycleState)
	if snapshot.LifecycleState == "" {
		snapshot.LifecycleState = play.LifecycleStateInProgress
	}
	snapshot.MediaState, snapshot.ClientState = play.MediaState(attempt.MediaState), play.ClientState(attempt.ClientState)
	snapshot.StartedAt = attempt.StartedAt
	return snapshot
}

func lifecycleEventModel(lifecycleID string, sequence int64, event play.LifecycleEvent, createdAt time.Time) *gbmodels.GbPlayLifecycleEvent {
	return &gbmodels.GbPlayLifecycleEvent{
		EventID: event.EventID, LifecycleID: lifecycleID, Sequence: sequence, EventAt: event.EventAt, ElapsedMS: event.ElapsedMS,
		Stage: string(event.Stage), EventName: event.EventName, FactState: string(event.FactState), Source: string(event.Source),
		DeviceCode: event.DeviceCode, ChannelCode: event.ChannelCode, StreamID: event.StreamID, NodeID: event.NodeID,
		SSRC: event.SSRC, Reused: event.Reused, CallID: event.CallID, CSeq: event.CSeq,
		ReasonCode: event.ReasonCode, ReasonMessage: event.ReasonMessage, MetadataJSON: event.MetadataJSON, CreatedAt: createdAt,
	}
}

func updateAttemptSnapshot(tx *gorm.DB, attempt *gbmodels.GbPlayAttempt, snapshot *play.LifecycleSnapshot, event play.LifecycleEvent) error {
	outcome := attempt.Outcome
	if event.EventName == play.EventMediaReady || event.EventName == play.EventReuseMediaReady {
		outcome = PlayOutcomeSuccess
	} else if event.FactState == play.FactFailed && event.Stage != play.StageClient &&
		attempt.LifecycleState != string(play.LifecycleStateCompleted) && attempt.LifecycleState != string(play.LifecycleStateStale) {
		outcome = PlayOutcomeFailure
	}
	updates := map[string]any{
		"stream_id": snapshot.StreamID, "node_id": snapshot.NodeID, "ssrc": snapshot.SSRC, "reused": snapshot.Reused,
		"call_id": snapshot.CallID, "cseq": snapshot.CSeq, "current_stage": snapshot.CurrentStage,
		"media_state": snapshot.MediaState, "client_state": snapshot.ClientState, "lifecycle_state": snapshot.LifecycleState,
		"failure_stage": snapshot.FailureStage, "reason_code": snapshot.ReasonCode, "reason_message": snapshot.ReasonMessage,
		"last_event_at": event.EventAt, "outcome": outcome,
	}
	if event.EventName == play.EventFirstFrame {
		updates["client_first_frame_at"] = event.EventAt
	}
	if event.EventName == play.EventPlayerError {
		updates["client_error_at"] = event.EventAt
		updates["client_error_code"] = event.ReasonCode
	}
	if attempt.LifecycleState == string(play.LifecycleStateInProgress) && snapshot.LifecycleState != play.LifecycleStateInProgress {
		updates["finished_at"] = event.EventAt
	}
	return tx.Model(attempt).Updates(updates).Error
}

func newLifecycleID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
