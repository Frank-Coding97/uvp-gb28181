package dashboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

const (
	PlayOutcomeStarted = "started"
	PlayOutcomeSuccess = "success"
	PlayOutcomeFailure = "failure"
)

type PlayAttemptStore struct {
	db    *gorm.DB
	clock func() time.Time
}

func NewPlayAttemptStore(db *gorm.DB) *PlayAttemptStore {
	return &PlayAttemptStore{db: db, clock: time.Now}
}
func (store *PlayAttemptStore) SetClock(clock func() time.Time) { store.clock = clock }

func (store *PlayAttemptStore) Begin(ctx context.Context, userID uint, deviceID, channelID string) (string, error) {
	correlationID, err := newAttemptCorrelationID()
	if err != nil {
		return "", err
	}
	record := gbmodels.GbPlayAttempt{CorrelationID: correlationID, UserID: userID, DeviceCode: deviceID, ChannelCode: channelID, Outcome: PlayOutcomeStarted, StartedAt: store.clock()}
	if err := store.db.WithContext(ctx).Create(&record).Error; err != nil {
		return "", err
	}
	return correlationID, nil
}

func (store *PlayAttemptStore) Finish(ctx context.Context, correlationID, outcome, failureStage string, nodeID int64, reused bool) error {
	if correlationID == "" {
		return nil
	}
	finishedAt := store.clock()
	return store.db.WithContext(ctx).Model(&gbmodels.GbPlayAttempt{}).Where("correlation_id = ? AND outcome = ?", correlationID, PlayOutcomeStarted).Updates(map[string]any{
		"outcome": outcome, "failure_stage": failureStage, "node_id": nodeID, "reused": reused, "finished_at": finishedAt, "updated_at": finishedAt,
	}).Error
}

type PlaySuccessSummary struct {
	Attempts uint64        `json:"attempts"`
	Success  uint64        `json:"success"`
	Failure  uint64        `json:"failure"`
	Rate     *float64      `json:"rate"`
	Status   SectionStatus `json:"status"`
	Coverage Coverage      `json:"coverage"`
	AsOf     time.Time     `json:"asOf"`
}

func (store *PlayAttemptStore) Last24Hours(ctx context.Context, now time.Time) (PlaySuccessSummary, error) {
	return store.last24Hours(ctx, now, store.db.WithContext(ctx).Model(&gbmodels.GbPlayAttempt{}))
}

func (store *PlayAttemptStore) Last24HoursScoped(ctx context.Context, now time.Time, scope QueryScope) (PlaySuccessSummary, error) {
	query := store.db.WithContext(ctx).
		Table("gb_play_attempt AS attempt").
		Joins("JOIN gb_device ON gb_device.device_id = attempt.device_code AND gb_device.deleted_at IS NULL")
	if scope != nil {
		query = scope(query)
	}
	return store.last24Hours(ctx, now, query)
}

func (store *PlayAttemptStore) last24Hours(ctx context.Context, now time.Time, query *gorm.DB) (PlaySuccessSummary, error) {
	var rows []struct {
		Outcome string
		Count   uint64
	}
	if err := query.Select("outcome, COUNT(*) AS count").Where("started_at >= ? AND started_at <= ?", now.Add(-24*time.Hour), now).Where("outcome IN ?", []string{PlayOutcomeSuccess, PlayOutcomeFailure}).Group("outcome").Scan(&rows).Error; err != nil {
		return PlaySuccessSummary{}, err
	}
	result := PlaySuccessSummary{Status: StatusEmpty, Coverage: CoverageNotStarted, AsOf: now}
	for _, row := range rows {
		if row.Outcome == PlayOutcomeSuccess {
			result.Success = row.Count
		} else {
			result.Failure = row.Count
		}
	}
	result.Attempts = result.Success + result.Failure
	if result.Attempts > 0 {
		rate := float64(result.Success) / float64(result.Attempts)
		result.Rate = &rate
		result.Status, result.Coverage = StatusOK, CoverageComplete
	}
	return result, nil
}

func newAttemptCorrelationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
