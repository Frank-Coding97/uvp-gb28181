package diagnosis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDiagnosisRepositoryUpsertOrderingAndIdempotency(t *testing.T) {
	repo, db := newDiagnosisRepository(t)
	ctx := context.Background()
	base := diagnosisRecord("key-1", time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC))

	require.NoError(t, repo.UpsertBatch(ctx, []Record{base, base}))
	require.Equal(t, int64(1), diagnosisRowCount(t, db))

	newer := base
	newer.ObservedAt = base.ObservedAt.Add(time.Minute)
	newer.Code = CodeNonceExpired
	require.NoError(t, repo.UpsertBatch(ctx, []Record{newer}))

	older := base
	older.Code = CodeNonceReplay
	require.NoError(t, repo.UpsertBatch(ctx, []Record{older}))

	rows, err := repo.Query(ctx, DiagnosisFilter{Category: CategoryRegisterFailure})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, CodeNonceExpired, rows[0].Code)
	require.Equal(t, newer.ObservedAt, rows[0].ObservedAt)

	resolved := newer
	resolved.State = StateResolved
	resolvedAt := resolved.ObservedAt
	resolved.ResolvedAt = &resolvedAt
	require.NoError(t, repo.UpsertBatch(ctx, []Record{resolved}))

	rows, err = repo.Query(ctx, DiagnosisFilter{Category: CategoryRegisterFailure})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, StateResolved, rows[0].State)
}

func TestDiagnosisRepositoryKeepsIndependentAttempts(t *testing.T) {
	repo, db := newDiagnosisRepository(t)
	first := diagnosisRecord("key-1", time.Now().UTC())
	second := first
	second.CorrelationKey = "key-2"
	second.CSeq = 3
	require.NoError(t, repo.UpsertBatch(context.Background(), []Record{first, second}))
	require.Equal(t, int64(2), diagnosisRowCount(t, db))
}

func TestDiagnosisRepositoryQueryAndEvidence(t *testing.T) {
	repo, _ := newDiagnosisRepository(t)
	ctx := context.Background()
	base := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	register := diagnosisRecord("register", base)
	media := Record{
		ObservedAt: base.Add(time.Minute), CorrelationKey: "play", State: StateActive,
		Category: CategoryPlayStuck, Code: CodeMediaTimeout, Stage: StageMedia,
		Source: SourceRuntime, DeviceID: "device-2", ChannelID: "channel-2",
		CallID: "play-call", CSeq: 9, Method: "INVITE", StreamID: "stream-2",
	}
	require.NoError(t, repo.UpsertBatch(ctx, []Record{register, media}))

	rows, err := repo.Query(ctx, DiagnosisFilter{
		From: base.Add(30 * time.Second), To: base.Add(2 * time.Minute),
		DeviceID: "device-2", Category: CategoryPlayStuck, Code: CodeMediaTimeout,
		State: StateActive,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "play", rows[0].CorrelationKey)

	evidence, err := repo.FindByEvidence(ctx, media.ObservedAt.Add(-time.Hour), media.ObservedAt.Add(time.Hour), "play-call", 9)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, "play", evidence[0].CorrelationKey)

	evidence, err = repo.FindByEvidence(ctx, media.ObservedAt.Add(-time.Hour), media.ObservedAt.Add(time.Hour), "play-call", 10)
	require.NoError(t, err)
	require.Empty(t, evidence)
}

func TestDiagnosisRepositoryPruneIsBounded(t *testing.T) {
	repo, db := newDiagnosisRepository(t)
	ctx := context.Background()
	cutoff := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	rows := []Record{
		diagnosisRecord("oldest", cutoff.Add(-2*time.Hour)),
		diagnosisRecord("old", cutoff.Add(-time.Hour)),
		diagnosisRecord("fresh", cutoff.Add(time.Hour)),
	}
	require.NoError(t, repo.UpsertBatch(ctx, rows))

	deleted, err := repo.Prune(ctx, cutoff, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Equal(t, int64(2), diagnosisRowCount(t, db))

	remaining, err := repo.Query(ctx, DiagnosisFilter{})
	require.NoError(t, err)
	require.Equal(t, "old", remaining[1].CorrelationKey)
}

func newDiagnosisRepository(t *testing.T) (*GormRepository, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbSipTraceSessionDiagnosis{}))
	repo, err := NewGormRepository(db)
	require.NoError(t, err)
	return repo, db
}

func diagnosisRowCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbSipTraceSessionDiagnosis{}).Count(&count).Error)
	return count
}

func diagnosisRecord(key string, at time.Time) Record {
	return Record{
		ObservedAt: at, CorrelationKey: key, State: StateActive,
		Category: CategoryRegisterFailure, Code: CodeDigestFailure, Stage: StageRegister,
		Source: SourceRuntime, DeviceID: "device-1", CallID: "register-call", CSeq: 2,
		Method: "REGISTER", StatusCode: 401,
	}
}
