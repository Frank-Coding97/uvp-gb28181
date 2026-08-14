package diagnosis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

var ErrRepositoryUnavailable = errors.New("diagnosis repository unavailable")

type Repository interface {
	UpsertBatch(context.Context, []Record) error
	Query(context.Context, DiagnosisFilter) ([]Record, error)
	FindByEvidence(context.Context, time.Time, time.Time, string, uint32) ([]Record, error)
	Prune(context.Context, time.Time, int) (int64, error)
}

type DiagnosisFilter struct {
	From     time.Time
	To       time.Time
	DeviceID string
	Category Category
	Code     Code
	State    State
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) (*GormRepository, error) {
	if db == nil {
		return nil, ErrRepositoryUnavailable
	}
	return &GormRepository{db: db}, nil
}

func (repository *GormRepository) UpsertBatch(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	for i := range records {
		if err := records[i].Validate(); err != nil {
			return fmt.Errorf("validate diagnosis record %d: %w", i, err)
		}
	}
	if err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, record := range records {
			if err := upsertRecord(tx, record); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("upsert diagnosis batch: %w", err)
	}
	return nil
}

func upsertRecord(tx *gorm.DB, record Record) error {
	row := modelFromRecord(record)
	var current gbmodels.GbSipTraceSessionDiagnosis
	err := tx.Where("session_day = ? AND category = ? AND correlation_key = ?",
		row.SessionDay, row.Category, row.CorrelationKey).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Create(&row).Error
	}
	if err != nil {
		return err
	}
	if !diagnosisRecordWins(record, recordFromModel(current)) {
		return nil
	}
	row.ID = current.ID
	return tx.Model(&current).Select("*").Updates(&row).Error
}

func diagnosisRecordWins(incoming, current Record) bool {
	if incoming.ObservedAt.After(current.ObservedAt) {
		return true
	}
	if incoming.ObservedAt.Before(current.ObservedAt) {
		return false
	}
	return incoming.State == StateResolved && current.State != StateResolved
}

func (repository *GormRepository) Query(ctx context.Context, filter DiagnosisFilter) ([]Record, error) {
	query := repository.db.WithContext(ctx).Model(&gbmodels.GbSipTraceSessionDiagnosis{})
	if !filter.From.IsZero() {
		query = query.Where("observed_at >= ?", filter.From.UTC())
	}
	if !filter.To.IsZero() {
		query = query.Where("observed_at <= ?", filter.To.UTC())
	}
	if filter.DeviceID != "" {
		query = query.Where("device_id = ?", filter.DeviceID)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Code != "" {
		query = query.Where("code = ?", filter.Code)
	}
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	var rows []gbmodels.GbSipTraceSessionDiagnosis
	if err := query.Order("observed_at DESC").Order("id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query diagnosis: %w", err)
	}
	return recordsFromModels(rows), nil
}

func (repository *GormRepository) FindByEvidence(ctx context.Context, from, to time.Time, callID string, cseq uint32) ([]Record, error) {
	query := repository.db.WithContext(ctx).Model(&gbmodels.GbSipTraceSessionDiagnosis{}).
		Where("call_id = ? AND cseq = ?", callID, cseq)
	if !from.IsZero() {
		query = query.Where("observed_at >= ?", from.UTC())
	}
	if !to.IsZero() {
		query = query.Where("observed_at <= ?", to.UTC())
	}
	var rows []gbmodels.GbSipTraceSessionDiagnosis
	if err := query.Order("observed_at ASC").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("find diagnosis evidence: %w", err)
	}
	return recordsFromModels(rows), nil
}

func (repository *GormRepository) Prune(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		return 0, errors.New("diagnosis prune batch size must be positive")
	}
	var deleted int64
	err := repository.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint64
		if err := tx.Model(&gbmodels.GbSipTraceSessionDiagnosis{}).
			Where("observed_at < ?", cutoff.UTC()).
			Order("observed_at ASC").Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("id IN ?", ids).Delete(&gbmodels.GbSipTraceSessionDiagnosis{})
		deleted = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return deleted, fmt.Errorf("prune diagnosis: %w", err)
	}
	return deleted, nil
}

func modelFromRecord(record Record) gbmodels.GbSipTraceSessionDiagnosis {
	observedAt := record.ObservedAt.UTC()
	resolvedAt := cloneUTC(record.ResolvedAt)
	return gbmodels.GbSipTraceSessionDiagnosis{
		ID: record.ID, SessionDay: dayUTC(observedAt), CorrelationKey: record.CorrelationKey,
		ObservedAt: observedAt, State: string(record.State), Category: string(record.Category),
		Code: string(record.Code), Stage: string(record.Stage), Source: string(record.Source),
		DeviceID: record.DeviceID, ChannelID: record.ChannelID, CallID: record.CallID,
		CSeq: record.CSeq, Method: record.Method, StatusCode: record.StatusCode,
		StreamID: record.StreamID, ResolvedAt: resolvedAt,
	}
}

func recordFromModel(row gbmodels.GbSipTraceSessionDiagnosis) Record {
	return Record{
		ID: row.ID, SessionDay: row.SessionDay.UTC(), CorrelationKey: row.CorrelationKey,
		ObservedAt: row.ObservedAt.UTC(), State: State(row.State), Category: Category(row.Category),
		Code: Code(row.Code), Stage: Stage(row.Stage), Source: Source(row.Source),
		DeviceID: row.DeviceID, ChannelID: row.ChannelID, CallID: row.CallID,
		CSeq: row.CSeq, Method: row.Method, StatusCode: row.StatusCode,
		StreamID: row.StreamID, ResolvedAt: cloneUTC(row.ResolvedAt),
	}
}

func recordsFromModels(rows []gbmodels.GbSipTraceSessionDiagnosis) []Record {
	records := make([]Record, len(rows))
	for i := range rows {
		records[i] = recordFromModel(rows[i])
	}
	return records
}

func dayUTC(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func cloneUTC(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
