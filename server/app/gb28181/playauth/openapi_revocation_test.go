package playauth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

var openAPIRevocationTestDBID atomic.Int64

type revocationAllowAllBoundary struct{}

func (revocationAllowAllBoundary) AuthorizeCreate(context.Context, uint, uint) error {
	return nil
}

func (revocationAllowAllBoundary) AuthorizeClient(context.Context, uint, string, uint) error {
	return nil
}

type revocationFixture struct {
	db    *gorm.DB
	store *OpenAPIRevocationStore
	clock time.Time
}

func newOpenAPIRevocationFixture(t *testing.T) revocationFixture {
	t.Helper()
	clock := time.Date(2026, 9, 6, 4, 0, 0, 123456000, time.UTC)
	dsn := fmt.Sprintf("file:openapi_revocation_%d?mode=memory&cache=shared", openAPIRevocationTestDBID.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&models.Client{}, &models.ClientScope{}, &models.Audit{},
		&models.PlayGrant{}, &models.Viewer{}, &appmodels.SysOperationLog{},
	))
	fixture := revocationFixture{db: db, clock: clock}
	fixture.store = NewOpenAPIRevocationStore(db, func() time.Time { return fixture.clock })
	return fixture
}

func (f revocationFixture) newService(t *testing.T, store openapiclient.RevocationIntentStore) *openapiclient.Service {
	t.Helper()
	secretManager, err := openapiclient.NewSecretManager(bytes.Repeat([]byte{0xA5}, 32), "revocation-test-key")
	require.NoError(t, err)
	service, err := openapiclient.NewService(f.db, secretManager,
		openapiclient.WithClock(func() time.Time { return f.clock }),
		openapiclient.WithRevocationIntentStore(store),
		openapiclient.WithManagementBoundary(revocationAllowAllBoundary{}),
	)
	require.NoError(t, err)
	return service
}

func (f revocationFixture) createClient(t *testing.T, service *openapiclient.Service, name string) openapiclient.ClientView {
	t.Helper()
	view, _, err := service.Create(context.Background(), openapiclient.CreateRequest{
		Name: name, OwnerDeptID: 10, ResponsibleUserID: 101, CreatedBy: 7,
	})
	require.NoError(t, err)
	return view
}

func (f revocationFixture) enableScope(t *testing.T, service *openapiclient.Service, view openapiclient.ClientView, scope string) openapiclient.ClientView {
	t.Helper()
	updated, err := service.SetScope(context.Background(), view.ID, scope, true, view.RowVersion, 7)
	require.NoError(t, err)
	return updated
}

func revocationGrant(id string, clientID int64, scope string, clientEpoch, scopeEpoch int64, state models.GrantState, now time.Time) models.PlayGrant {
	deviceID := "34020000001320000001"
	channelID := "34020000001310000001"
	nodeUUID := "node-a"
	bootNonce := "0123456789abcdef0123456789abcdef"
	schema := "rtmp"
	vhost := "__defaultVhost__"
	app := "live"
	stream := "stream-a"
	protocol := "https-flv"
	generation := uint64(1)
	return models.PlayGrant{
		GrantID: id, ClientID: clientID, Scope: scope,
		DeviceID: &deviceID, ChannelID: &channelID,
		ClientEpoch: clientEpoch, ScopeEpoch: scopeEpoch, DeviceEpoch: 1,
		NodeUUID: &nodeUUID, BootNonce: &bootNonce, Schema: &schema,
		VHost: &vhost, App: &app, Stream: &stream, MediaGeneration: &generation,
		Protocol: &protocol, IssuedAt: now, ExpiresAt: now.Add(time.Minute),
		State: state, CreatedAt: now, UpdatedAt: now,
	}
}

func revocationViewer(grantID string, id int64, state models.ViewerState, now time.Time) models.Viewer {
	lastSeen := now
	return models.Viewer{
		ID: id, GrantID: grantID, NodeUUID: "node-a", BootNonce: "0123456789abcdef0123456789abcdef",
		Identifier: fmt.Sprintf("viewer-%d", id), Schema: "rtmp", VHost: "__defaultVhost__",
		App: "live", Stream: "stream-a", MediaGeneration: 1, State: state,
		LastSeenAt: &lastSeen, Attempts: 3, LastErrorClass: "worker_retry",
		CreatedAt: now, UpdatedAt: now,
	}
}

func requireGrant(t *testing.T, db *gorm.DB, id string) models.PlayGrant {
	t.Helper()
	var row models.PlayGrant
	require.NoError(t, db.First(&row, "grant_id = ?", id).Error)
	return row
}

func requireViewer(t *testing.T, db *gorm.DB, grantID string) models.Viewer {
	t.Helper()
	var row models.Viewer
	require.NoError(t, db.First(&row, "grant_id = ?", grantID).Error)
	return row
}

func TestOpenAPIRevocationStoreClientWideOnlyTouchesTargetClientAndKnownLiveRows(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	service := f.newService(t, f.store)
	a := f.createClient(t, service, "A")
	b := f.createClient(t, service, "B")
	u := f.createClient(t, service, "U")
	a = f.enableScope(t, service, a, openAPIPlayScope)
	b = f.enableScope(t, service, b, openAPIPlayScope)
	u = f.enableScope(t, service, u, openAPIPlayScope)

	grantIDs := []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000005", "00000000-0000-4000-8000-000000000007"}
	require.NoError(t, f.db.Create(&[]models.PlayGrant{
		revocationGrant(grantIDs[0], a.ID, openAPIPlayScope, 1, 1, models.GrantStatePending, f.clock),
		revocationGrant(grantIDs[1], a.ID, openAPIPlayScope, 1, 1, models.GrantStateIssued, f.clock),
		revocationGrant(grantIDs[2], a.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock),
		revocationGrant(grantIDs[3], a.ID, openAPIPlayScope, 1, 1, models.GrantStateExpired, f.clock),
		revocationGrant(grantIDs[4], b.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock),
		revocationGrant(grantIDs[5], u.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock),
	}).Error)
	require.NoError(t, f.db.Create(&[]models.Viewer{
		revocationViewer(grantIDs[1], 101, models.ViewerStateActive, f.clock),
		revocationViewer(grantIDs[2], 102, models.ViewerStatePending, f.clock),
		revocationViewer(grantIDs[3], 103, models.ViewerStateClosed, f.clock),
		revocationViewer(grantIDs[4], 104, models.ViewerStateActive, f.clock),
		revocationViewer(grantIDs[5], 106, models.ViewerStateActive, f.clock),
	}).Error)
	// Existing worker state is a durable retry record and must not be reset by
	// a repeated intent. The grant itself is bound and will be captured below.
	retryGrant := revocationGrant("00000000-0000-4000-8000-000000000006", a.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&retryGrant).Error)
	retryViewer := revocationViewer(retryGrant.GrantID, 105, models.ViewerStateRevokePending, f.clock)
	retryAt := f.clock.Add(-time.Second)
	retryViewer.RetryAt = &retryAt
	retryViewer.Attempts = 4
	retryViewer.LastErrorClass = "worker_timeout"
	require.NoError(t, f.db.Create(&retryViewer).Error)

	disabled, err := service.SetStatus(context.Background(), a.ID, models.StatusDisabled, a.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, a.AuthEpoch+1, disabled.AuthEpoch)
	for _, id := range grantIDs[:3] {
		row := requireGrant(t, f.db, id)
		require.Equal(t, models.GrantStateRevoked, row.State, id)
		require.Equal(t, "client.disabled", row.Reason, id)
		require.Equal(t, f.clock, row.UpdatedAt, id)
	}
	require.Equal(t, models.GrantStateExpired, requireGrant(t, f.db, grantIDs[3]).State)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, grantIDs[4]).State)
	require.Equal(t, models.ViewerStateRevokePending, requireViewer(t, f.db, grantIDs[1]).State)
	require.Equal(t, models.ViewerStateRevokePending, requireViewer(t, f.db, grantIDs[2]).State)
	require.Equal(t, f.clock, requireViewer(t, f.db, grantIDs[1]).RetryAt.UTC())
	require.Equal(t, 0, requireViewer(t, f.db, grantIDs[1]).Attempts)
	require.Equal(t, openAPIRevocationRetryError, requireViewer(t, f.db, grantIDs[1]).LastErrorClass)
	require.Equal(t, models.ViewerStateClosed, requireViewer(t, f.db, grantIDs[3]).State)
	require.Equal(t, models.ViewerStateRevokePending, requireViewer(t, f.db, retryGrant.GrantID).State)
	require.Equal(t, 4, requireViewer(t, f.db, retryGrant.GrantID).Attempts)
	require.Equal(t, "worker_timeout", requireViewer(t, f.db, retryGrant.GrantID).LastErrorClass)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, grantIDs[4]).State)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, grantIDs[5]).State)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, grantIDs[5]).State)

	progress, err := f.store.Progress(context.Background(), a.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, progress.Pending)
	require.EqualValues(t, 0, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusPending, progress.Status)
}

func TestOpenAPIRevocationStoreScopeOnlyAndNonMediaScopeAreExact(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	service := f.newService(t, f.store)
	view := f.createClient(t, service, "scope-client")
	view = f.enableScope(t, service, view, "device:list")
	view = f.enableScope(t, service, view, openAPIPlayScope)

	deviceGrant := revocationGrant("00000000-0000-4000-8000-000000000011", view.ID, "device:list", 1, 1, models.GrantStateBound, f.clock)
	playOld := revocationGrant("00000000-0000-4000-8000-000000000012", view.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	playCurrent := revocationGrant("00000000-0000-4000-8000-000000000013", view.ID, openAPIPlayScope, 1, 2, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&[]models.PlayGrant{deviceGrant, playOld, playCurrent}).Error)
	require.NoError(t, f.db.Create(&[]models.Viewer{
		revocationViewer(deviceGrant.GrantID, 111, models.ViewerStateActive, f.clock),
		revocationViewer(playOld.GrantID, 112, models.ViewerStateActive, f.clock),
		revocationViewer(playCurrent.GrantID, 113, models.ViewerStateActive, f.clock),
	}).Error)

	withoutDevice, err := service.SetScope(context.Background(), view.ID, "device:list", false, view.RowVersion, 7)
	require.NoError(t, err, "non-media scope revocation is a validated no-op for media")
	deviceAfter := requireGrant(t, f.db, deviceGrant.GrantID)
	require.Equal(t, models.GrantStateBound, deviceAfter.State)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, deviceGrant.GrantID).State)
	require.Equal(t, view.AuthEpoch, withoutDevice.AuthEpoch)

	// The play scope epoch is still 1. Revoke it and ensure only the older
	// play grant is captured; a grant from a newer scope epoch is untouched.
	withoutPlay, err := service.SetScope(context.Background(), view.ID, openAPIPlayScope, false, withoutDevice.RowVersion, 7)
	require.NoError(t, err)
	require.Equal(t, models.GrantStateRevoked, requireGrant(t, f.db, playOld.GrantID).State)
	require.Equal(t, "scope.revoke", requireGrant(t, f.db, playOld.GrantID).Reason)
	require.Equal(t, models.ViewerStateRevokePending, requireViewer(t, f.db, playOld.GrantID).State)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, playCurrent.GrantID).State)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, playCurrent.GrantID).State)
	require.Equal(t, view.AuthEpoch, withoutPlay.AuthEpoch)

	var intent openapiclient.RevocationIntent
	intent = openapiclient.RevocationIntent{ClientID: view.ID, ClientEpoch: view.AuthEpoch, Scope: "not-a-scope", ScopeEpoch: 1, Reason: "scope.revoke", CreatedAt: f.clock}
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		return f.store.RecordRevocationIntent(context.Background(), tx, intent)
	}), ErrOpenAPIRevocationInvalid)
}

func TestOpenAPIRevocationStoreRejectsStaleOrMismatchedIntentWithoutMediaChanges(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	service := f.newService(t, f.store)
	view := f.createClient(t, service, "stale-client")
	view = f.enableScope(t, service, view, openAPIPlayScope)
	grant := revocationGrant("00000000-0000-4000-8000-000000000021", view.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := revocationViewer(grant.GrantID, 121, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&viewer).Error)

	stale := openapiclient.RevocationIntent{ClientID: view.ID, ClientEpoch: view.AuthEpoch + 1, Reason: "client.disabled", CreatedAt: f.clock}
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		return f.store.RecordRevocationIntent(context.Background(), tx, stale)
	}), ErrOpenAPIRevocationStale)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, grant.GrantID).State)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, grant.GrantID).State)

	mismatchedReason := openapiclient.RevocationIntent{ClientID: view.ID, ClientEpoch: view.AuthEpoch, Reason: "client.disabled", CreatedAt: f.clock}
	// The client is active, so even an otherwise well-formed client intent is
	// rejected before any grant is selected.
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		return f.store.RecordRevocationIntent(context.Background(), tx, mismatchedReason)
	}), ErrOpenAPIRevocationStale)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, grant.GrantID).State)

	unknownReason := mismatchedReason
	unknownReason.Reason = "client.something_else"
	require.ErrorIs(t, f.db.Transaction(func(tx *gorm.DB) error {
		return f.store.RecordRevocationIntent(context.Background(), tx, unknownReason)
	}), ErrOpenAPIRevocationInvalid)
}

func TestOpenAPIRevocationStoreReplayPreservesGrantTombstoneAndWorkerRetry(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	service := f.newService(t, f.store)
	view := f.createClient(t, service, "replay-client")
	view = f.enableScope(t, service, view, openAPIPlayScope)
	grant := revocationGrant("00000000-0000-4000-8000-000000000031", view.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := revocationViewer(grant.GrantID, 131, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&viewer).Error)

	disabled, err := service.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.NoError(t, err)
	firstGrant := requireGrant(t, f.db, grant.GrantID)
	firstViewer := requireViewer(t, f.db, grant.GrantID)
	firstUpdatedAt := firstGrant.UpdatedAt
	firstRetryAt := firstViewer.RetryAt
	require.NotNil(t, firstRetryAt)
	intent := openapiclient.RevocationIntent{ClientID: disabled.ID, ClientEpoch: disabled.AuthEpoch, Reason: "client.disabled", CreatedAt: f.clock}
	f.clock = f.clock.Add(time.Minute)
	require.NotEqual(t, f.clock, firstUpdatedAt)
	require.NotEqual(t, f.clock, firstRetryAt.UTC())

	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		return f.store.RecordRevocationIntent(context.Background(), tx, intent)
	}))
	secondGrant := requireGrant(t, f.db, grant.GrantID)
	secondViewer := requireViewer(t, f.db, grant.GrantID)
	require.Equal(t, models.GrantStateRevoked, secondGrant.State)
	require.Equal(t, firstUpdatedAt, secondGrant.UpdatedAt)
	require.Equal(t, "client.disabled", secondGrant.Reason)
	require.Equal(t, models.ViewerStateRevokePending, secondViewer.State)
	require.Equal(t, firstRetryAt.UTC(), secondViewer.RetryAt.UTC())
	require.Equal(t, firstViewer.Attempts, secondViewer.Attempts)
	require.Equal(t, firstViewer.LastErrorClass, secondViewer.LastErrorClass)
}

type failingAfterRevocationStore struct {
	inner *OpenAPIRevocationStore
}

func (s failingAfterRevocationStore) RecordRevocationIntent(ctx context.Context, tx *gorm.DB, intent openapiclient.RevocationIntent) error {
	if err := s.inner.RecordRevocationIntent(ctx, tx, intent); err != nil {
		return err
	}
	return errors.New("intent acknowledgement failed")
}

func TestOpenAPIRevocationStoreClientServiceRollsBackStatusAndMediaOnStoreFailure(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	base := f.newService(t, f.store)
	view := f.createClient(t, base, "rollback-client")
	view = f.enableScope(t, base, view, openAPIPlayScope)
	grant := revocationGrant("00000000-0000-4000-8000-000000000041", view.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&grant).Error)
	viewer := revocationViewer(grant.GrantID, 141, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&viewer).Error)

	failing := f.newService(t, failingAfterRevocationStore{inner: f.store})
	_, err := failing.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.Error(t, err)
	current, err := failing.Get(context.Background(), view.ID)
	require.NoError(t, err)
	require.Equal(t, models.StatusActive, current.Status)
	require.Equal(t, view.AuthEpoch, current.AuthEpoch)
	require.Equal(t, view.RowVersion, current.RowVersion)
	require.Equal(t, models.GrantStateBound, requireGrant(t, f.db, grant.GrantID).State)
	require.Equal(t, models.ViewerStateActive, requireViewer(t, f.db, grant.GrantID).State)
	var auditCount int64
	require.NoError(t, f.db.Model(&models.Audit{}).Where("client_id = ? AND reason_class = ?", view.ID, "client.disabled").Count(&auditCount).Error)
	require.Zero(t, auditCount)
}

func TestOpenAPIRevocationProgressIsConservativeWithoutWorkerEvidence(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	service := f.newService(t, f.store)
	view := f.createClient(t, service, "progress-client")
	view = f.enableScope(t, service, view, openAPIPlayScope)
	grant := revocationGrant("00000000-0000-4000-8000-000000000051", view.ID, openAPIPlayScope, 1, 1, models.GrantStateBound, f.clock)
	require.NoError(t, f.db.Create(&grant).Error)

	disabled, err := service.SetStatus(context.Background(), view.ID, models.StatusDisabled, view.RowVersion, 7)
	require.NoError(t, err)
	progress, err := f.store.Progress(context.Background(), disabled.ID)
	require.NoError(t, err)
	require.EqualValues(t, 0, progress.Pending)
	require.EqualValues(t, 0, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusUnknown, progress.Status)

	viewer := revocationViewer(grant.GrantID, 151, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&viewer).Error)
	progress, err = f.store.Progress(context.Background(), disabled.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, progress.Pending)
	require.Equal(t, OpenAPIRevocationStatusPending, progress.Status)
	require.NoError(t, f.db.Model(&models.Viewer{}).Where("grant_id = ?", grant.GrantID).Updates(map[string]any{
		"state": models.ViewerStateClosed, "updated_at": f.clock,
	}).Error)
	progress, err = f.store.Progress(context.Background(), disabled.ID)
	require.NoError(t, err)
	require.EqualValues(t, 0, progress.Pending)
	require.EqualValues(t, 1, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusClosed, progress.Status)
}

func TestOpenAPIRevocationProgressUsesAggregateForLargeGrantSetAndUnknownViewer(t *testing.T) {
	f := newOpenAPIRevocationFixture(t)
	const grantCount = 2305
	grants := make([]models.PlayGrant, grantCount)
	for i := range grants {
		grants[i] = revocationGrant(fmt.Sprintf("10000000-0000-4000-8000-%012d", i+1), testClientID, openAPIPlayScope, 1, 1, models.GrantStateRevoked, f.clock)
	}
	// Keep each INSERT below SQLite's variable limit. The resulting dataset is
	// intentionally larger than SQL Server's 2100-parameter limit for the old
	// grant-ID IN-list implementation.
	require.NoError(t, f.db.CreateInBatches(&grants, 20).Error)

	progress, err := f.store.Progress(context.Background(), testClientID)
	require.NoError(t, err)
	require.Zero(t, progress.Pending)
	require.Zero(t, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusUnknown, progress.Status, "missing viewer rows are not proof of clean closure")

	unknown := revocationViewer(grants[0].GrantID, 9001, models.ViewerStateClosed, f.clock)
	require.NoError(t, f.db.Create(&unknown).Error)
	require.NoError(t, f.db.Exec("PRAGMA ignore_check_constraints = ON").Error)
	require.NoError(t, f.db.Model(&models.Viewer{}).Where("id = ?", unknown.ID).Update("state", "worker_unknown").Error)
	require.NoError(t, f.db.Exec("PRAGMA ignore_check_constraints = OFF").Error)
	progress, err = f.store.Progress(context.Background(), testClientID)
	require.NoError(t, err)
	require.Zero(t, progress.Pending)
	require.Zero(t, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusUnknown, progress.Status, "an unrecognized viewer state must remain conservative")

	closed := revocationViewer(grants[1].GrantID, 9002, models.ViewerStateClosed, f.clock)
	require.NoError(t, f.db.Create(&closed).Error)
	progress, err = f.store.Progress(context.Background(), testClientID)
	require.NoError(t, err)
	require.Zero(t, progress.Pending)
	require.EqualValues(t, 1, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusUnknown, progress.Status, "unknown and missing rows prevent a clear result")

	active := revocationViewer(grants[2].GrantID, 9003, models.ViewerStateActive, f.clock)
	require.NoError(t, f.db.Create(&active).Error)
	progress, err = f.store.Progress(context.Background(), testClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, progress.Pending)
	require.EqualValues(t, 1, progress.Closed)
	require.Equal(t, OpenAPIRevocationStatusPending, progress.Status)
}

func disableOpenAPIRevocationFixtureClient(t *testing.T, fixture *openAPIGrantFixture) openapiclient.RevocationIntent {
	t.Helper()
	nextEpoch := int64(3)
	require.NoError(t, fixture.db.Model(&models.Client{}).Where("id = ?", testClientID).Updates(map[string]any{
		"status": models.StatusDisabled, "auth_epoch": nextEpoch, "updated_at": fixture.now,
	}).Error)
	return openapiclient.RevocationIntent{ClientID: testClientID, ClientEpoch: nextEpoch, Reason: "client.disabled", CreatedAt: fixture.now}
}

func TestOpenAPIRevocationStoreBindThenRevokeQueuesBoundViewer(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	bound, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("bind-before-revoke"))
	require.NoError(t, err)
	require.Equal(t, models.ViewerStateActive, bound.State)

	store := NewOpenAPIRevocationStore(fixture.db, func() time.Time { return fixture.now })
	intent := disableOpenAPIRevocationFixtureClient(t, fixture)
	require.NoError(t, fixture.db.Transaction(func(tx *gorm.DB) error {
		return store.RecordRevocationIntent(context.Background(), tx, intent)
	}))
	require.Equal(t, models.GrantStateRevoked, requireGrantState(t, fixture.db, reservation.GrantID, models.GrantStateRevoked).State)
	viewer := requireViewer(t, fixture.db, reservation.GrantID)
	require.Equal(t, models.ViewerStateRevokePending, viewer.State)
	require.Equal(t, 0, viewer.Attempts)
}

func TestOpenAPIRevocationStoreRevokeThenBindRejectsWithoutViewer(t *testing.T) {
	fixture := newOpenAPIGrantFixture(t)
	defer fixture.close(t)
	service, token, reservation := issueOpenAPITestGrant(t, fixture)
	store := NewOpenAPIRevocationStore(fixture.db, func() time.Time { return fixture.now })
	intent := disableOpenAPIRevocationFixtureClient(t, fixture)
	require.NoError(t, fixture.db.Transaction(func(tx *gorm.DB) error {
		return store.RecordRevocationIntent(context.Background(), tx, intent)
	}))
	require.Equal(t, models.GrantStateRevoked, requireGrantState(t, fixture.db, reservation.GrantID, models.GrantStateRevoked).State)

	_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("bind-after-revoke"))
	require.ErrorIs(t, err, ErrOpenAPIViewerDenied)
	var viewerCount int64
	require.NoError(t, fixture.db.Model(&models.Viewer{}).Where("grant_id = ?", reservation.GrantID).Count(&viewerCount).Error)
	require.Zero(t, viewerCount)
}
