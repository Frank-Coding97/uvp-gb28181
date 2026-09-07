package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newHomePositionTestService(t *testing.T, current *time.Time) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPTZOperation{},
		&gbmodels.GbPTZHomePosition{},
		&gbmodels.GbPTZState{},
	))
	service, err := NewService(db, &fakeTrackedSender{}, func() time.Time { return *current })
	require.NoError(t, err)
	return service, db
}

func intValue(value int) *int { return &value }

func boolValue(value bool) *bool { return &value }

func stringValue(value string) *string { return &value }

func homePositionUpdate(sequence uint, confirmedAt time.Time) HomePositionUpdate {
	operationID := "operation-source"
	return HomePositionUpdate{
		DeviceID:           2,
		ChannelID:          1,
		ChannelCode:        "C",
		Enabled:            false,
		ResetTime:          intValue(0),
		PresetID:           intValue(0),
		EnabledEncoding:    gbmodels.PTZHomePositionEnabledNumeric,
		ConfirmedAt:        confirmedAt,
		Source:             gbmodels.PTZHomePositionSourceDeviceQuery,
		Verification:       gbmodels.PTZHomePositionVerificationVerified,
		SourceSN:           int(sequence),
		SourceOperationID:  operationID,
		SourceOperationSeq: sequence,
		RawSummary:         "home-position",
	}
}

func TestHomePositionCacheStoresZeroValuesWithoutTouchingPreciseState(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, db := newHomePositionTestService(t, &now)
	pan := 12.5
	precise, err := service.ApplyPreciseNotify(context.Background(), PreciseNotify{
		DeviceID: 2, DeviceCode: "D", ChannelID: 1, ChannelCode: "C",
		Pan: &pan, ReceivedAt: now.Add(-time.Minute), DedupeKey: "precise",
	})
	require.NoError(t, err)

	home, applied, err := service.ApplyHomePosition(context.Background(), homePositionUpdate(10, now))
	require.NoError(t, err)
	require.True(t, applied)
	require.False(t, home.Enabled)
	require.NotNil(t, home.ResetTime)
	require.Zero(t, *home.ResetTime)
	require.NotNil(t, home.PresetID)
	require.Zero(t, *home.PresetID)

	var storedPrecise gbmodels.GbPTZState
	require.NoError(t, db.First(&storedPrecise, precise.ID).Error)
	require.Equal(t, pan, *storedPrecise.Pan)
	require.Equal(t, precise.ReceivedAt, storedPrecise.ReceivedAt)
}

func TestHomePositionCacheCausalCASRejectsEqualAndOlderSources(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, _ := newHomePositionTestService(t, &now)
	initial, applied, err := service.ApplyHomePosition(context.Background(), homePositionUpdate(10, now))
	require.NoError(t, err)
	require.True(t, applied)

	for _, sequence := range []uint{10, 9} {
		candidate := homePositionUpdate(sequence, now.Add(time.Minute))
		candidate.Enabled = true
		got, candidateApplied, candidateErr := service.ApplyHomePosition(context.Background(), candidate)
		require.NoError(t, candidateErr)
		require.False(t, candidateApplied)
		require.False(t, got.Enabled)
		require.Equal(t, initial.ConfirmedAt, got.ConfirmedAt, "equal/older source must not renew freshness")
		require.Equal(t, uint(10), got.SourceOperationSeq)
	}

	newer := homePositionUpdate(11, now.Add(2*time.Minute))
	newer.Enabled = true
	got, applied, err := service.ApplyHomePosition(context.Background(), newer)
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, got.Enabled)
	require.Equal(t, uint(11), got.SourceOperationSeq)
	require.Equal(t, newer.ConfirmedAt, got.ConfirmedAt)
}

func TestHomePositionFreshnessUsesOnlyHomeConfirmationAndNewerQueries(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	home := &gbmodels.GbPTZHomePosition{ConfirmedAt: now.Add(-59 * time.Second), SourceOperationSeq: 10}
	require.Equal(t, gbmodels.PTZFreshnessFresh, HomePositionFreshness(home, nil, now))
	home.ConfirmedAt = now.Add(-61 * time.Second)
	require.Equal(t, gbmodels.PTZFreshnessStale, HomePositionFreshness(home, nil, now))
	home.ConfirmedAt = time.Time{}
	require.Equal(t, gbmodels.PTZFreshnessUnknown, HomePositionFreshness(home, nil, now))
	require.Equal(t, gbmodels.PTZFreshnessUnknown, HomePositionFreshness(nil, nil, now))

	home.ConfirmedAt = now
	noData := &gbmodels.GbPTZOperation{ID: 11, Status: gbmodels.PTZOperationAccepted, ResponseHasData: boolValue(false)}
	require.Equal(t, gbmodels.PTZFreshnessStale, HomePositionFreshness(home, noData, now))
	failed := &gbmodels.GbPTZOperation{ID: 12, Status: gbmodels.PTZOperationTimeout}
	require.Equal(t, gbmodels.PTZFreshnessStale, HomePositionFreshness(home, failed, now))
	olderFailure := &gbmodels.GbPTZOperation{ID: 9, Status: gbmodels.PTZOperationTimeout}
	require.Equal(t, gbmodels.PTZFreshnessFresh, HomePositionFreshness(home, olderFailure, now))
}

func TestHomePositionReadModelPreservesConfigAfterNoDataQuery(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, db := newHomePositionTestService(t, &now)
	_, applied, err := service.ApplyHomePosition(context.Background(), homePositionUpdate(1, now))
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		ID: 2, OperationID: "no-data", IdempotencyKey: "no-data", DeviceID: 2, DeviceCode: "D",
		ChannelID: 1, ChannelCode: "C", CmdType: "HomePositionQuery", Action: "refresh_home_position",
		SN: 2, Status: gbmodels.PTZOperationAccepted, ResponseRequired: true, MaxAttempts: 3,
		ResponseHasData: boolValue(false), CreatedAt: now,
	}).Error)

	model, err := service.GetHomePositionReadModel(context.Background(), 1, nil)
	require.NoError(t, err)
	require.NotNil(t, model.HomePosition)
	require.False(t, model.HomePosition.Enabled)
	require.Equal(t, gbmodels.PTZFreshnessStale, model.Freshness)
	require.Equal(t, HomePositionRefreshSucceededNoData, model.Refresh.Status)
	require.NotNil(t, model.Refresh.OperationID)
	require.Equal(t, "no-data", *model.Refresh.OperationID)
}

func TestHomePositionCapabilitiesResolveProfileThenAcceptedHistory(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, db := newHomePositionTestService(t, &now)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		OperationID: "control-ok", IdempotencyKey: "control-ok", ChannelID: 1,
		CmdType: "DeviceControl", Action: "home_position", SN: 1,
		Status: gbmodels.PTZOperationAccepted, CreatedAt: now,
	}).Error)

	fromHistory, err := service.ResolveHomePositionCapabilities(context.Background(), 1, nil)
	require.NoError(t, err)
	require.Equal(t, HomePositionCapabilitySupported, fromHistory.Control.Status)
	require.Equal(t, HomePositionCapabilityUnknown, fromHistory.Query.Status)

	profile := `{"home_position_control":false,"homePositionQuery":true}`
	fromProfile, err := service.ResolveHomePositionCapabilities(context.Background(), 1, &profile)
	require.NoError(t, err)
	require.Equal(t, HomePositionCapabilityUnsupported, fromProfile.Control.Status)
	require.Equal(t, HomePositionCapabilitySupported, fromProfile.Query.Status)
	require.NotEmpty(t, fromProfile.Control.Reason)
	require.NotEmpty(t, fromProfile.Query.Reason)
}

func TestHomePositionUnsupportedCapabilityDoesNotBlockManualOperation(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, _ := newHomePositionTestService(t, &now)
	profile := `{"homePositionControl":false,"home_position_query":false}`
	capabilities, err := service.ResolveHomePositionCapabilities(context.Background(), 1, &profile)
	require.NoError(t, err)
	require.Equal(t, HomePositionCapabilityUnsupported, capabilities.Control.Status)
	require.Equal(t, HomePositionCapabilityUnsupported, capabilities.Query.Status)

	operation, err := service.Execute(context.Background(), testTarget(), responseRequiredCommand("manual-despite-capability"))
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
}

func TestHomePositionReadModelUsesExactReconcileOperation(t *testing.T) {
	now := time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC)
	service, db := newHomePositionTestService(t, &now)
	reconcileID := "reconcile-exact"
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		ID: 10, OperationID: "control", IdempotencyKey: "control", ChannelID: 1,
		CmdType: "DeviceControl", Action: "home_position", SN: 10,
		Status: gbmodels.PTZOperationAccepted, ReconcileOperationID: &reconcileID, CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		ID: 11, OperationID: reconcileID, IdempotencyKey: "reconcile", ChannelID: 1,
		CmdType: "HomePositionQuery", Action: "refresh_home_position", SN: 11,
		Status: gbmodels.PTZOperationAccepted, ResponseHasData: boolValue(true), CreatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		ID: 12, OperationID: "unrelated-newer", IdempotencyKey: "unrelated", ChannelID: 1,
		CmdType: "HomePositionQuery", Action: "refresh_home_position", SN: 12,
		Status: gbmodels.PTZOperationTimeout, CreatedAt: now,
	}).Error)
	update := homePositionUpdate(10, now)
	update.Source = gbmodels.PTZHomePositionSourceControlACK
	update.Verification = gbmodels.PTZHomePositionVerificationUnverified
	update.SourceOperationID = "control"
	_, applied, err := service.ApplyHomePosition(context.Background(), update)
	require.NoError(t, err)
	require.True(t, applied)

	model, err := service.GetHomePositionReadModel(context.Background(), 1, nil)
	require.NoError(t, err)
	require.Equal(t, HomePositionControlAccepted, model.Control.Status)
	require.NotNil(t, model.Refresh.OperationID)
	require.Equal(t, reconcileID, *model.Refresh.OperationID)
	require.Equal(t, HomePositionRefreshSucceeded, model.Refresh.Status)
}

func TestBuildPTZOperationReadModelUsesDynamicDeadlinePriority(t *testing.T) {
	queue := time.Date(2026, 7, 24, 11, 0, 5, 0, time.UTC)
	transport := queue.Add(10 * time.Second)
	application := transport.Add(15 * time.Second)
	tests := []struct {
		name      string
		operation gbmodels.GbPTZOperation
		want      *time.Time
	}{
		{name: "none", operation: gbmodels.GbPTZOperation{}, want: nil},
		{name: "queue", operation: gbmodels.GbPTZOperation{QueueDeadlineAt: &queue}, want: &queue},
		{name: "transport", operation: gbmodels.GbPTZOperation{QueueDeadlineAt: &queue, TransportDeadlineAt: &transport}, want: &transport},
		{name: "application", operation: gbmodels.GbPTZOperation{QueueDeadlineAt: &queue, TransportDeadlineAt: &transport, DeadlineAt: &application}, want: &application},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := BuildPTZOperationReadModel(test.operation)
			require.Equal(t, test.want, model.DeadlineAt)
			require.Nil(t, model.ErrorCode)
			require.Nil(t, model.ErrorMessage)
		})
	}
}
