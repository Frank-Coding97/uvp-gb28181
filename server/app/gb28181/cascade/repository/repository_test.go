package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
)

var testDBSequence atomic.Uint64

func newTestRepository(t *testing.T) *GormRepository {
	t.Helper()
	dsn := fmt.Sprintf("file:cascade_repository_%d?mode=memory&cache=shared", testDBSequence.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{SkipDefaultTransaction: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&model.GbCascadePlatform{},
		&model.GbCascadeDeviceProjection{},
		&model.GbCascadeChannelProjection{},
		&model.GbCascadeBusinessNode{},
		&model.GbCascadeBusinessBinding{},
		&model.GbCascadeMediaSession{},
	))
	return NewGormRepository(db)
}

func newPlatform(name, localID string) *model.GbCascadePlatform {
	return &model.GbCascadePlatform{
		Name: name, UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		Host: "192.0.2.1", Port: 5060, LocalDeviceID: localID, LocalDomain: "3402000000",
		LocalSIPIP: "192.0.2.2", LocalSIPPort: 5060, EffectiveVersion: "2016",
		ConfigRevision: 1, Enabled: true,
	}
}

func TestPlatformPersistsUnlimitedStreamsOnCreateAndUpdate(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("unlimited", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Zero(t, stored.MaxStreams)
	platform.MaxStreams = 4
	updated, err := repo.UpdatePlatformConfig(ctx, platform, platform.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, 4, updated.MaxStreams)
	updated.MaxStreams = 0
	_, err = repo.UpdatePlatformConfig(ctx, updated, updated.ConfigRevision)
	require.NoError(t, err)
	stored, err = repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Zero(t, stored.MaxStreams)
}

func TestSetPublishGroupUpdatesOnlyPublicationAndKeepsIndependentRevisions(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("publish-group", "34020000002000000001")
	platform.SecretNonce = []byte("nonce-sentinel")
	platform.SecretCiphertext = []byte("cipher-sentinel")
	platform.SecretAlg = "AES-256-GCM"
	platform.SecretKeyVersion = "key-v3"
	platform.RetryPolicy = `{"initialMs":321}`
	platform.AutoPushCatalog = true
	platform.PublishCivil = true
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 0, []BusinessNodeInput{
		{Code: "34020000002150000001", Name: "业务根", Kind: "group"},
	}, nil))
	before, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)

	updated, err := repo.SetPublishGroup(ctx, platform.ID, before.ConfigRevision, true)
	require.NoError(t, err)
	require.True(t, updated.PublishGroup)
	require.Equal(t, before.ConfigRevision+1, updated.ConfigRevision)
	require.Equal(t, before.ProjectionRevision+1, updated.ProjectionRevision)
	require.Equal(t, before.SecretNonce, updated.SecretNonce)
	require.Equal(t, before.SecretCiphertext, updated.SecretCiphertext)
	require.Equal(t, before.SecretAlg, updated.SecretAlg)
	require.Equal(t, before.SecretKeyVersion, updated.SecretKeyVersion)
	require.Equal(t, before.RetryPolicy, updated.RetryPolicy)
	require.True(t, updated.AutoPushCatalog)
	require.True(t, updated.PublishCivil)

	// 同值提交允许新版本重试运行时同步，但不能伪造投影变化。
	retried, err := repo.SetPublishGroup(ctx, platform.ID, updated.ConfigRevision, true)
	require.NoError(t, err)
	require.Equal(t, updated.ConfigRevision+1, retried.ConfigRevision)
	require.Equal(t, updated.ProjectionRevision, retried.ProjectionRevision)
	require.True(t, retried.PublishGroup)

	closed, err := repo.SetPublishGroup(ctx, platform.ID, retried.ConfigRevision, false)
	require.NoError(t, err)
	require.False(t, closed.PublishGroup)
	require.Equal(t, retried.ProjectionRevision+1, closed.ProjectionRevision)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Nodes, 1, "关闭发布不得删除业务树")
}

func TestSetPublishGroupRequiresValidRootIdentityAndRejectsStaleConfigRevision(t *testing.T) {
	for _, tc := range []struct {
		name      string
		localID   string
		rootNodes []BusinessNodeInput
	}{
		{name: "missing root", localID: "34020000002000000001"},
		{name: "non-system identity", localID: "34020000001320000001", rootNodes: []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}},
		{name: "wrong root code", localID: "34020000002000000001", rootNodes: []BusinessNodeInput{{Code: "34020000002160000001", Name: "根", Kind: "group"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepository(t)
			platform := newPlatform("invalid-root", tc.localID)
			require.NoError(t, repo.CreatePlatform(context.Background(), platform))
			if len(tc.rootNodes) > 0 {
				// 构造错误业务根场景时直接写入底层记录，模拟历史脏数据。
				for _, input := range tc.rootNodes {
					require.NoError(t, repo.db.Create(&model.GbCascadeBusinessNode{PlatformID: platform.ID, Code: input.Code, Name: input.Name, Kind: input.Kind}).Error)
				}
			}
			_, err := repo.SetPublishGroup(context.Background(), platform.ID, platform.ConfigRevision, true)
			require.ErrorIs(t, err, ErrInvalidProjection)
			stored, findErr := repo.FindPlatform(context.Background(), platform.ID)
			require.NoError(t, findErr)
			require.False(t, stored.PublishGroup)
			require.Equal(t, platform.ConfigRevision, stored.ConfigRevision)
		})
	}

	repo := newTestRepository(t)
	platform := newPlatform("stale-publish", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(context.Background(), platform))
	require.NoError(t, repo.ReplaceBusinessGroup(context.Background(), platform.ID, 0, []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, nil))
	_, err := repo.SetPublishGroup(context.Background(), platform.ID, platform.ConfigRevision, true)
	require.NoError(t, err)
	_, err = repo.SetPublishGroup(context.Background(), platform.ID, platform.ConfigRevision, false)
	require.ErrorIs(t, err, ErrRevisionConflict)
}

func TestSetPublishGroupRevalidatesPublishedTreeDuringSameValueRuntimeRetry(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("retry-invalid-root", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 0, []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, nil))
	published, err := repo.SetPublishGroup(ctx, platform.ID, platform.ConfigRevision, true)
	require.NoError(t, err)
	require.NoError(t, repo.db.Where("platform_id = ?", platform.ID).Delete(&model.GbCascadeBusinessNode{}).Error)

	_, err = repo.SetPublishGroup(ctx, platform.ID, published.ConfigRevision, true)
	require.ErrorIs(t, err, ErrInvalidProjection, "same-value retries must not reload an invalid published tree")
	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.True(t, stored.PublishGroup)
	require.Equal(t, published.ConfigRevision, stored.ConfigRevision)
}

func TestSetPublishGroupRacingTreeClearCannotCommitInvalidPublishedState(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("publish-tree-race", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	require.NoError(t, repo.ReplaceBusinessGroup(ctx, platform.ID, 0, []BusinessNodeInput{{Code: "34020000002150000001", Name: "根", Kind: "group"}}, nil))
	current, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	go func() {
		ready.Done()
		<-start
		_, err := repo.SetPublishGroup(ctx, platform.ID, current.ConfigRevision, true)
		results <- err
	}()
	go func() {
		ready.Done()
		<-start
		results <- repo.ReplaceBusinessGroup(ctx, platform.ID, current.ProjectionRevision,
			[]BusinessNodeInput{}, []BusinessBindingInput{})
	}()
	ready.Wait()
	close(start)
	errA, errB := <-results, <-results
	successes := 0
	for _, operationErr := range []error{errA, errB} {
		if operationErr == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "revision checks permit only one operation to commit")

	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	if stored.PublishGroup {
		require.Len(t, snapshot.Nodes, 1, "publication cannot commit after the only root has been cleared")
		require.Equal(t, "group", snapshot.Nodes[0].Kind)
	} else {
		require.Empty(t, snapshot.Nodes, "if tree clearing wins, publication must remain disabled")
	}
}

func TestPlatformOptionsPersistAcrossCreateUpdateReload(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("options", "34020000001320000003")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Equal(t, 1, stored.CatalogBatchSize)
	assertPlatformOptions := func(platform *model.GbCascadePlatform, enabled bool) {
		t.Helper()
		encoded, err := json.Marshal(platform)
		require.NoError(t, err)
		var options map[string]any
		require.NoError(t, json.Unmarshal(encoded, &options))
		for _, name := range []string{"autoPushCatalog", "recordingEnabled", "rtcpKeepalive"} {
			require.Equal(t, enabled, options[name], name)
		}
	}
	assertPlatformOptions(stored, false)
	configured := newPlatform("configured-options", "34020000001320000004")
	configured.AutoPushCatalog = true
	configured.RecordingEnabled = true
	configured.RTCPKeepalive = true
	require.NoError(t, repo.CreatePlatform(ctx, configured))
	created, err := repo.FindPlatform(ctx, configured.ID)
	require.NoError(t, err)
	assertPlatformOptions(created, true)
	for _, enabled := range []bool{true, false} {
		options, err := json.Marshal(map[string]bool{
			"autoPushCatalog": enabled, "recordingEnabled": enabled, "rtcpKeepalive": enabled,
		})
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(options, stored))
		updated, err := repo.UpdatePlatformConfig(ctx, stored, stored.ConfigRevision)
		require.NoError(t, err)
		stored, err = repo.FindPlatform(ctx, updated.ID)
		require.NoError(t, err)
		assertPlatformOptions(stored, enabled)
	}
}

func TestPlatformConfigUpdateUsesRevisionCASAndPlatformScope(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))
	registeredAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.RecordRegistrationSuccess(ctx, platformA.ID, registeredAt, registeredAt.Add(time.Hour)))

	platformA.Host = "198.51.100.10"
	updated, err := repo.UpdatePlatformConfig(ctx, platformA, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.ConfigRevision)
	require.Equal(t, "198.51.100.10", updated.Host)
	require.Equal(t, registeredAt, *updated.RegisterAt, "configuration CAS must not overwrite runtime facts")

	platformA.Host = "203.0.113.20"
	_, err = repo.UpdatePlatformConfig(ctx, platformA, 1)
	require.ErrorIs(t, err, ErrRevisionConflict)

	storedB, err := repo.FindPlatform(ctx, platformB.ID)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.1", storedB.Host)
	require.EqualValues(t, 1, storedB.ConfigRevision)
}

func TestManagementRepositoryListsSoftDeletesAndPreservesCredential(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformA.SecretNonce = []byte("nonce-a")
	platformA.SecretCiphertext = []byte("cipher-a")
	platformA.SecretAlg = "AES-256-GCM"
	platformA.SecretKeyVersion = "v1"
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))

	platformA.Host = "198.51.100.20"
	updated, err := repo.UpdatePlatformConfig(ctx, platformA, platformA.ConfigRevision)
	require.NoError(t, err)
	require.Equal(t, []byte("nonce-a"), updated.SecretNonce)
	require.Equal(t, []byte("cipher-a"), updated.SecretCiphertext)
	require.Equal(t, "AES-256-GCM", updated.SecretAlg)
	require.Equal(t, "v1", updated.SecretKeyVersion)

	platforms, err := repo.ListPlatforms(ctx)
	require.NoError(t, err)
	require.Len(t, platforms, 2)

	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000021"}},
	))
	require.NoError(t, repo.SoftDeletePlatform(ctx, platformA.ID))
	_, err = repo.FindPlatform(ctx, platformA.ID)
	require.ErrorIs(t, err, ErrPlatformNotFound)
	platforms, err = repo.ListPlatforms(ctx)
	require.NoError(t, err)
	require.Len(t, platforms, 1)
	require.Equal(t, platformB.ID, platforms[0].ID)

	var activeDevices, activeChannels int64
	require.NoError(t, repo.db.Model(&model.GbCascadeDeviceProjection{}).Where("platform_id = ? AND active = ?", platformA.ID, true).Count(&activeDevices).Error)
	require.NoError(t, repo.db.Model(&model.GbCascadeChannelProjection{}).Where("platform_id = ? AND active = ?", platformA.ID, true).Count(&activeChannels).Error)
	require.Zero(t, activeDevices)
	require.Zero(t, activeChannels)
}

func TestRuntimeFactsAreIndependentAndNeverOverwriteLastSuccessfulProfile(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	platform.EffectiveVersion = "2022"
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.RecordRegistrationSuccess(ctx, platform.ID, now, now.Add(time.Hour)))
	require.NoError(t, repo.RecordRegistrationFailure(ctx, platform.ID, "401", "invalid credentials", now.Add(time.Minute)))
	require.NoError(t, repo.RecordHeartbeatSuccess(ctx, platform.ID, now.Add(2*time.Minute)))
	require.NoError(t, repo.RecordHeartbeatFailure(ctx, platform.ID, "timeout", "keepalive timeout", now.Add(3*time.Minute)))

	stored, err := repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Equal(t, "2022", stored.EffectiveVersion)
	require.NotNil(t, stored.RegisterAt)
	require.NotNil(t, stored.RegisterExpiresAt)
	require.NotNil(t, stored.HeartbeatAt)
	require.Equal(t, "timeout", stored.LastErrorCode)

	require.NoError(t, repo.RecordRegistrationExpired(ctx, platform.ID, now.Add(4*time.Minute)))
	stored, err = repo.FindPlatform(ctx, platform.ID)
	require.NoError(t, err)
	require.Nil(t, stored.RegisterAt, "Expires=0 must clear registration fact")
	require.Nil(t, stored.RegisterExpiresAt)
	require.NotNil(t, stored.HeartbeatAt, "registration expiry must not erase heartbeat fact")
}

func TestReplaceProjectionIsAtomicAndSnapshotsArePlatformScoped(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platformA := newPlatform("upstream-a", "34020000001320000001")
	platformB := newPlatform("upstream-b", "34020000001320000002")
	require.NoError(t, repo.CreatePlatform(ctx, platformA))
	require.NoError(t, repo.CreatePlatform(ctx, platformB))

	err := repo.ReplaceProjection(ctx, platformA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011", Name: "device-a"}},
		[]ChannelProjectionInput{{SourceDeviceID: 99, SourceChannelID: 2, PublishedChannelID: "34020000001320000021"}},
	)
	require.ErrorIs(t, err, ErrInvalidProjection)

	empty, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.Empty(t, empty.Devices)
	require.Empty(t, empty.Channels)
	require.Zero(t, empty.Revision, "failed projection writes must not advance revision")

	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011", Name: "device-a"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000021", Name: "camera-a"}},
	))
	first, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.Len(t, first.Devices, 1)
	require.Len(t, first.Channels, 1)
	require.EqualValues(t, 1, first.Revision, "first successful replacement must advance revision")
	require.Equal(t, "camera-a", first.Channels[0].Name)

	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, first.Revision,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011", Name: "device-a"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000021", Name: "camera-a-renamed"}},
	))
	second, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, second.Revision)
	require.Equal(t, "camera-a", first.Channels[0].Name, "captured snapshot must remain immutable")
	require.Equal(t, "camera-a-renamed", second.Channels[0].Name)

	require.NoError(t, repo.ReplaceProjection(ctx, platformA.ID, second.Revision, nil, nil))
	cleared, err := repo.ProjectionSnapshot(ctx, platformA.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, cleared.Revision, "clearing all projections must still advance revision")
	require.Empty(t, cleared.Devices)
	require.Empty(t, cleared.Channels)

	other, err := repo.ProjectionSnapshot(ctx, platformB.ID)
	require.NoError(t, err)
	require.Empty(t, other.Devices)
	require.Empty(t, other.Channels)
	require.Zero(t, other.Revision)
}

func TestReplaceProjectionCreatesRowsWhenRecordNotFoundErrorsAreMasked(t *testing.T) {
	repo := newTestRepository(t)
	require.NoError(t, repo.db.Callback().Query().Before("gorm:query").Register("mask_record_not_found_for_test", func(db *gorm.DB) {
		db.Statement.RaiseErrorOnNotFound = false
	}))
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	require.NoError(t, repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{
			{SourceDeviceID: 10, PublishedDeviceID: "34020000001320000010"},
			{SourceDeviceID: 20, PublishedDeviceID: "34020000001320000020"},
		},
		[]ChannelProjectionInput{
			{SourceDeviceID: 10, SourceChannelID: 11, PublishedChannelID: "34020000001320000011"},
			{SourceDeviceID: 20, SourceChannelID: 21, PublishedChannelID: "34020000001320000021"},
		},
	))

	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.Len(t, snapshot.Devices, 2)
	require.Len(t, snapshot.Channels, 2)
	require.EqualValues(t, []uint64{10, 20}, []uint64{snapshot.Devices[0].SourceDeviceID, snapshot.Devices[1].SourceDeviceID})
}

func TestReplaceProjectionAllocatesPublishedIDCollisionAcrossKinds(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))

	err := repo.ReplaceProjection(ctx, platform.ID, 0,
		[]DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011"}},
		[]ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000011"}},
	)
	require.NoError(t, err)
	snapshot, err := repo.ProjectionSnapshot(ctx, platform.ID)
	require.NoError(t, err)
	require.NotEqual(t, snapshot.Devices[0].PublishedDeviceID, snapshot.Channels[0].PublishedChannelID)
}

func TestMediaSessionTerminalStateIsMonotonicAndNonterminalScanExcludesClosed(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	platform := newPlatform("upstream-a", "34020000001320000001")
	require.NoError(t, repo.CreatePlatform(ctx, platform))
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	session := &model.GbCascadeMediaSession{
		PlatformID: platform.ID, DialogKey: "call-a/from-a/to-a", CallID: "call-a",
		State: model.CascadeMediaSessionStateReceived, ReceivedAt: &now,
	}
	require.NoError(t, repo.CreateMediaSession(ctx, session))

	changed, err := repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateReceived, model.CascadeMediaSessionStateProvisioning, now.Add(time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateProvisioning, model.CascadeMediaSessionStateAnswered, now.Add(2*time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateAnswered, model.CascadeMediaSessionStateActive, now.Add(3*time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateActive, model.CascadeMediaSessionStateClosing, now.Add(4*time.Second))
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateClosing, model.CascadeMediaSessionStateClosed, now.Add(5*time.Second))
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = repo.TransitionMediaSession(ctx, session.DialogKey, model.CascadeMediaSessionStateClosed, model.CascadeMediaSessionStateFailed, now.Add(6*time.Second))
	require.ErrorIs(t, err, ErrInvalidMediaTransition)
	require.False(t, changed)

	unfinished, err := repo.ListNonterminalMediaSessions(ctx, platform.ID)
	require.NoError(t, err)
	require.Empty(t, unfinished)

	stored, err := repo.FindMediaSessionByDialog(ctx, session.DialogKey)
	require.NoError(t, err)
	require.Equal(t, model.CascadeMediaSessionStateClosed, stored.State)
	require.NotNil(t, stored.ClosedAt)
}

func TestSameLocalIdentityAcrossUpstreamPorts(t *testing.T) {
	repo := newTestRepository(t)
	a := newPlatform("upstream-a", "34020000002000000002")
	a.Port = 15060
	require.NoError(t, repo.CreatePlatform(context.Background(), a))
	b := newPlatform("upstream-b", a.LocalDeviceID)
	b.Port = 16060
	require.NoError(t, repo.CreatePlatform(context.Background(), b))
	duplicate := newPlatform("different-name", a.LocalDeviceID)
	duplicate.Port = 15060
	require.Error(t, repo.CreatePlatform(context.Background(), duplicate))
}

func TestAllocatedChannelIDSurvivesRemovalReselectionAndOrderChange(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	p := newPlatform("stable-ids", "34020000002000000001")
	require.NoError(t, repo.CreatePlatform(ctx, p))
	devices := []DeviceProjectionInput{{SourceDeviceID: 1, PublishedDeviceID: "34020000001180000001"}, {SourceDeviceID: 2, PublishedDeviceID: "34020000001180000002"}}
	channels := []ChannelProjectionInput{{SourceDeviceID: 1, SourceChannelID: 11, PublishedChannelID: "34020000001320000001"}, {SourceDeviceID: 2, SourceChannelID: 12, PublishedChannelID: "34020000001320000001"}}
	require.NoError(t, repo.ReplaceProjection(ctx, p.ID, 0, devices, channels))
	snapshot, err := repo.ProjectionSnapshot(ctx, p.ID)
	require.NoError(t, err)
	ids := map[uint64]string{}
	for _, c := range snapshot.Channels {
		ids[c.SourceChannelID] = c.PublishedChannelID
	}
	require.NotEqual(t, ids[11], ids[12])
	require.Equal(t, ids[11][:14], ids[12][:14])
	require.NoError(t, repo.ReplaceProjection(ctx, p.ID, 1, nil, nil))
	channels[0], channels[1] = channels[1], channels[0]
	require.NoError(t, repo.ReplaceProjection(ctx, p.ID, 2, devices, channels))
	snapshot, err = repo.ProjectionSnapshot(ctx, p.ID)
	require.NoError(t, err)
	for _, c := range snapshot.Channels {
		require.Equal(t, ids[c.SourceChannelID], c.PublishedChannelID)
	}
}
