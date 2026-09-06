package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// checkNativeRevocation runs after the core gate and the native grant/viewer
// fixture have proved that this is an isolated, migrated database. It keeps
// the client lifecycle mutation in the same SQL transaction as the real
// revocation store, but does not require the unrelated application operation
// log table which is intentionally absent from the core migration database.
func checkNativeRevocation(t *testing.T, root *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(root.Statement.Context, 35*time.Second)
	defer cancel()
	db := root.WithContext(ctx)

	fixture := nativeGrantViewerFixture{}
	var clientIDs []int64
	defer func() {
		cleanupNativeRevocationClients(t, root, clientIDs)
		fixture.cleanup(t, root)
	}()

	var err error
	fixture, err = prepareNativeGrantViewerFixture(t, db)
	require.NoError(t, err)
	clientIDs = append(clientIDs, fixture.client.ID)

	clock := time.Date(2026, 9, 6, 21, 0, 0, 123456789, time.UTC)
	now := func() time.Time { return clock }
	store := playauth.NewOpenAPIRevocationStore(db, now)
	signer, err := playauth.NewSigner([]byte(strings.Repeat("r", 32)), playauth.WithNow(now))
	require.NoError(t, err)
	grantService, err := playauth.NewOpenAPIGrantService(db, signer, nativeGrantViewerNodeAuthority{}, now)
	require.NoError(t, err)
	quota := limit.NewQuota(db, now)

	b, err := insertNativeRevocationClient(db, "uvp_native_revocation_b", "native revocation B", now())
	clientIDs = append(clientIDs, b.ID)
	require.NoError(t, err)
	u, err := insertNativeRevocationClient(db, "uvp_native_revocation_u", "native revocation U", now())
	clientIDs = append(clientIDs, u.ID)
	require.NoError(t, err)

	aGrantID := issueNativeRevocationBound(t, ctx, quota, grantService, fixture.client.ID, "native-revoke-a", 1)
	bGrantID := issueNativeRevocationBound(t, ctx, quota, grantService, b.ID, "native-revoke-b", 2)
	uGrantID := issueNativeRevocationBound(t, ctx, quota, grantService, u.ID, "native-revoke-u", 3)

	// This bound row deliberately carries the next scope epoch before that
	// epoch is revoked. It proves the exact '< scope epoch' boundary without
	// needing a second media/network flow in this database gate.
	futureGrant := nativeRevocationBoundGrant(uuid.NewString(), b.ID, 1, 2, now())
	require.NoError(t, db.Create(&futureGrant).Error)
	futureViewer := nativeRevocationViewer(futureGrant.GrantID, "native-revoke-b-future", now())
	require.NoError(t, db.Create(&futureViewer).Error)

	var aBefore models.Client
	require.NoError(t, db.First(&aBefore, "id = ?", fixture.client.ID).Error)
	bBefore := b
	uBefore := u

	// Client-wide disable: the bound viewer is durably queued, while B and U
	// remain untouched. The parent row, epoch, media tombstone and audit are
	// committed by one native transaction.
	disableIntent := openapiclient.RevocationIntent{
		ClientID: fixture.client.ID, ClientEpoch: aBefore.AuthEpoch + 1,
		Reason: "client.disabled", CreatedAt: clock,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.Client{}).Where("id = ? AND row_version = ?", aBefore.ID, aBefore.RowVersion).
			Updates(map[string]any{
				"status": models.StatusDisabled, "auth_epoch": disableIntent.ClientEpoch,
				"row_version": aBefore.RowVersion + 1, "updated_by": 7, "updated_at": clock,
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("native client disable update failed")
		}
		if err := store.RecordRevocationIntent(ctx, tx, disableIntent); err != nil {
			return err
		}
		audit := nativeRevocationAudit(aBefore.ID, "client.disabled", clock)
		return tx.Create(&audit).Error
	}))

	var disabled models.Client
	require.NoError(t, db.First(&disabled, "id = ?", aBefore.ID).Error)
	require.Equal(t, models.StatusDisabled, disabled.Status)
	require.Equal(t, disableIntent.ClientEpoch, disabled.AuthEpoch)
	require.Equal(t, aBefore.RowVersion+1, disabled.RowVersion)
	require.Equal(t, models.GrantStateRevoked, nativeRevocationGrant(t, db, aGrantID).State)
	aViewer := nativeRevocationViewerRow(t, db, aGrantID)
	require.Equal(t, models.ViewerStateRevokePending, aViewer.State)
	require.Equal(t, 0, aViewer.Attempts)
	require.Equal(t, "revocation_pending", aViewer.LastErrorClass)
	require.NotNil(t, aViewer.RetryAt)
	require.Equal(t, clock.UTC().Truncate(time.Microsecond), aViewer.RetryAt.UTC())
	require.EqualValues(t, 1, nativeRevocationAuditCount(t, db, aBefore.ID, "client.disabled"))

	bAfterClientDisable := nativeRevocationClient(t, db, bBefore.ID)
	require.Equal(t, models.StatusActive, bAfterClientDisable.Status)
	require.Equal(t, bBefore.AuthEpoch, bAfterClientDisable.AuthEpoch)
	require.Equal(t, bBefore.RowVersion, bAfterClientDisable.RowVersion)
	require.Equal(t, models.GrantStateBound, nativeRevocationGrant(t, db, bGrantID).State)
	require.Equal(t, models.ViewerStateActive, nativeRevocationViewerRow(t, db, bGrantID).State)
	uAfterClientDisable := nativeRevocationClient(t, db, uBefore.ID)
	require.Equal(t, models.StatusActive, uAfterClientDisable.Status)
	require.Equal(t, uBefore.AuthEpoch, uAfterClientDisable.AuthEpoch)
	require.Equal(t, uBefore.RowVersion, uAfterClientDisable.RowVersion)
	require.Equal(t, models.GrantStateBound, nativeRevocationGrant(t, db, uGrantID).State)
	require.Equal(t, models.ViewerStateActive, nativeRevocationViewerRow(t, db, uGrantID).State)

	progress, err := store.Progress(ctx, aBefore.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, progress.Pending)
	require.Zero(t, progress.Closed)
	require.Equal(t, playauth.OpenAPIRevocationStatusPending, progress.Status)

	firstGrant := nativeRevocationGrant(t, db, aGrantID)
	firstViewer := nativeRevocationViewerRow(t, db, aGrantID)
	firstUpdatedAt := firstGrant.UpdatedAt
	require.NotNil(t, firstViewer.RetryAt)
	firstRetryAt := firstViewer.RetryAt.UTC()
	firstAttempts := firstViewer.Attempts
	firstErrorClass := firstViewer.LastErrorClass
	clock = clock.Add(time.Minute)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return store.RecordRevocationIntent(ctx, tx, disableIntent)
	}))
	replayedGrant := nativeRevocationGrant(t, db, aGrantID)
	replayedViewer := nativeRevocationViewerRow(t, db, aGrantID)
	require.Equal(t, firstUpdatedAt, replayedGrant.UpdatedAt)
	require.Equal(t, firstRetryAt, replayedViewer.RetryAt.UTC())
	require.Equal(t, firstAttempts, replayedViewer.Attempts)
	require.Equal(t, firstErrorClass, replayedViewer.LastErrorClass)

	require.NoError(t, db.Model(&models.Viewer{}).Where("grant_id = ?", aGrantID).
		Updates(map[string]any{"state": models.ViewerStateClosed, "updated_at": clock}).Error)
	progress, err = store.Progress(ctx, aBefore.ID)
	require.NoError(t, err)
	require.Zero(t, progress.Pending)
	require.EqualValues(t, 1, progress.Closed)
	require.Equal(t, playauth.OpenAPIRevocationStatusClosed, progress.Status)

	// One hundred missing viewer rows keep the aggregate query on the native
	// path while proving that absence of a worker row is conservative unknown.
	missing := make([]models.PlayGrant, 100)
	for i := range missing {
		missing[i] = nativeRevocationRevokedGrant(fmt.Sprintf("30000000-0000-4000-8000-%012d", i+1), aBefore.ID, disableIntent.ClientEpoch-1, 6, clock)
	}
	require.NoError(t, db.CreateInBatches(&missing, 25).Error)
	progress, err = store.Progress(ctx, aBefore.ID)
	require.NoError(t, err)
	require.Zero(t, progress.Pending)
	require.EqualValues(t, 1, progress.Closed)
	require.Equal(t, playauth.OpenAPIRevocationStatusUnknown, progress.Status)

	// Scope-only revoke captures the old scope epoch, but not the future row
	// created above. Client auth epoch and unrelated U media remain unchanged.
	var bScope models.ClientScope
	require.NoError(t, db.Where("client_id = ? AND scope = ?", b.ID, limit.PlayLiveApplyScope).First(&bScope).Error)
	scopeIntent := openapiclient.RevocationIntent{
		ClientID: b.ID, ClientEpoch: bBefore.AuthEpoch, Scope: limit.PlayLiveApplyScope,
		ScopeEpoch: bScope.ScopeEpoch + 1, Reason: "scope.revoke", CreatedAt: clock,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		clientUpdated := tx.Model(&models.Client{}).
			Where("id = ? AND row_version = ?", b.ID, bBefore.RowVersion).
			Updates(map[string]any{"row_version": bBefore.RowVersion + 1, "updated_by": 7, "updated_at": clock})
		if clientUpdated.Error != nil || clientUpdated.RowsAffected != 1 {
			return errors.New("native scope client update failed")
		}
		updated := tx.Model(&models.ClientScope{}).
			Where("client_id = ? AND scope = ?", b.ID, bScope.Scope).
			Updates(map[string]any{"enabled": false, "scope_epoch": scopeIntent.ScopeEpoch, "updated_by": 7, "updated_at": clock})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("native scope revoke update failed")
		}
		if err := store.RecordRevocationIntent(ctx, tx, scopeIntent); err != nil {
			return err
		}
		audit := nativeRevocationAudit(b.ID, "scope.revoke", clock)
		return tx.Create(&audit).Error
	}))
	revokedB := nativeRevocationGrant(t, db, bGrantID)
	require.Equal(t, models.GrantStateRevoked, revokedB.State)
	require.Equal(t, models.ViewerStateRevokePending, nativeRevocationViewerRow(t, db, bGrantID).State)
	require.Equal(t, models.GrantStateBound, nativeRevocationGrant(t, db, futureGrant.GrantID).State)
	require.Equal(t, models.ViewerStateActive, nativeRevocationViewerRow(t, db, futureGrant.GrantID).State)
	bAfter := nativeRevocationClient(t, db, b.ID)
	require.Equal(t, models.StatusActive, bAfter.Status)
	require.Equal(t, bBefore.AuthEpoch, bAfter.AuthEpoch)
	require.Equal(t, bBefore.RowVersion+1, bAfter.RowVersion)
	var bScopeAfter models.ClientScope
	require.NoError(t, db.Where("client_id = ? AND scope = ?", b.ID, limit.PlayLiveApplyScope).First(&bScopeAfter).Error)
	require.False(t, bScopeAfter.Enabled)
	require.Equal(t, scopeIntent.ScopeEpoch, bScopeAfter.ScopeEpoch)
	require.EqualValues(t, 1, nativeRevocationAuditCount(t, db, b.ID, "scope.revoke"))

	// Force an error after the real store has changed media and an audit row
	// has been written. The native transaction must roll all three domains back.
	uFailure := nativeFailingRevocationStore{inner: store}
	uIntent := openapiclient.RevocationIntent{ClientID: u.ID, ClientEpoch: uBefore.AuthEpoch + 1, Reason: "client.disabled", CreatedAt: clock}
	err = db.Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.Client{}).Where("id = ? AND row_version = ?", uBefore.ID, uBefore.RowVersion).
			Updates(map[string]any{
				"status": models.StatusDisabled, "auth_epoch": uIntent.ClientEpoch,
				"row_version": uBefore.RowVersion + 1, "updated_by": 7, "updated_at": clock,
			})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return errors.New("native rollback client update failed")
		}
		audit := nativeRevocationAudit(uBefore.ID, "client.disabled", clock)
		if err := tx.Create(&audit).Error; err != nil {
			return err
		}
		return uFailure.RecordRevocationIntent(ctx, tx, uIntent)
	})
	require.Error(t, err)
	uAfter := nativeRevocationClient(t, db, uBefore.ID)
	require.Equal(t, uBefore.Status, uAfter.Status)
	require.Equal(t, uBefore.AuthEpoch, uAfter.AuthEpoch)
	require.Equal(t, uBefore.RowVersion, uAfter.RowVersion)
	require.Equal(t, models.GrantStateBound, nativeRevocationGrant(t, db, uGrantID).State)
	require.Equal(t, models.ViewerStateActive, nativeRevocationViewerRow(t, db, uGrantID).State)
	require.Zero(t, nativeRevocationAuditCount(t, db, uBefore.ID, "client.disabled"))

	t.Logf("%s native revocation: bound client disable, exact scope revoke, B/U isolation, tombstone replay, aggregate missing/closed/100 rows, and full lifecycle rollback passed", db.Dialector.Name())
}

type nativeFailingRevocationStore struct {
	inner *playauth.OpenAPIRevocationStore
}

func (s nativeFailingRevocationStore) RecordRevocationIntent(ctx context.Context, tx *gorm.DB, intent openapiclient.RevocationIntent) error {
	if err := s.inner.RecordRevocationIntent(ctx, tx, intent); err != nil {
		return err
	}
	return errors.New("native revocation acknowledgement failed")
}

func insertNativeRevocationClient(db *gorm.DB, ak, name string, now time.Time) (models.Client, error) {
	client := models.Client{
		AK: ak, Name: name, OwnerDeptID: nativeGrantViewerDepartmentID, Status: models.StatusActive,
		SecretCiphertext: []byte("native-revocation-ciphertext"), SecretIV: []byte("native-rev"), SecretKeyID: "native",
		SecretVersion: 1, AuthEpoch: 1, RateLimit: 10, Burst: 20, ViewerQuota: 10, RowVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&client).Error; err != nil {
		return client, err
	}
	if err := db.Create(&models.ClientScope{
		ClientID: client.ID, Scope: limit.PlayLiveApplyScope, Enabled: true, ScopeEpoch: 1, UpdatedAt: now,
	}).Error; err != nil {
		return client, err
	}
	return client, nil
}

func issueNativeRevocationBound(t *testing.T, ctx context.Context, quota *limit.Quota, service *playauth.OpenAPIGrantService, clientID int64, identifier string, generation uint64) string {
	t.Helper()
	reservation, err := quota.ReservePending(ctx, limit.ReservationRequest{
		ClientID: clientID, Scope: limit.PlayLiveApplyScope,
		DeviceID: nativeGrantViewerDeviceID, ChannelID: nativeGrantViewerChannelID,
	})
	require.NoError(t, err)
	issued, err := service.Issue(ctx, nativeGrantViewerIssueRequest(reservation.GrantID, "https-flv", generation))
	require.NoError(t, err)
	viewer, err := service.BindViewer(ctx, issued.Token, nativeGrantViewerBindRequest(identifier, "https-flv", generation))
	require.NoError(t, err)
	require.Equal(t, models.ViewerStateActive, viewer.State)
	return reservation.GrantID
}

func nativeRevocationBoundGrant(id string, clientID, clientEpoch, scopeEpoch int64, now time.Time) models.PlayGrant {
	deviceID, channelID := nativeGrantViewerDeviceID, nativeGrantViewerChannelID
	nodeUUID, bootNonce := nativeGrantViewerNodeUUID, nativeGrantViewerBootNonce
	schema, vhost, app, stream, protocol := "rtmp", "__defaultVhost__", "rtp", nativeGrantViewerDeviceID+"_"+nativeGrantViewerChannelID, "https-flv"
	generation := uint64(9)
	return models.PlayGrant{
		GrantID: id, ClientID: clientID, Scope: limit.PlayLiveApplyScope,
		DeviceID: &deviceID, ChannelID: &channelID, ClientEpoch: clientEpoch, ScopeEpoch: scopeEpoch, DeviceEpoch: 1,
		NodeUUID: &nodeUUID, BootNonce: &bootNonce, Schema: &schema, VHost: &vhost, App: &app, Stream: &stream,
		MediaGeneration: &generation, Protocol: &protocol, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
		State: models.GrantStateBound, CreatedAt: now, UpdatedAt: now,
	}
}

func nativeRevocationRevokedGrant(id string, clientID, clientEpoch, scopeEpoch int64, now time.Time) models.PlayGrant {
	return models.PlayGrant{
		GrantID: id, ClientID: clientID, Scope: limit.PlayLiveApplyScope,
		ClientEpoch: clientEpoch, ScopeEpoch: scopeEpoch, DeviceEpoch: 1,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour), State: models.GrantStateRevoked,
		Reason: "client.disabled", CreatedAt: now, UpdatedAt: now,
	}
}

func nativeRevocationViewer(grantID, identifier string, now time.Time) models.Viewer {
	seen := now
	return models.Viewer{
		GrantID: grantID, NodeUUID: nativeGrantViewerNodeUUID, BootNonce: nativeGrantViewerBootNonce, Identifier: identifier,
		Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: nativeGrantViewerDeviceID + "_" + nativeGrantViewerChannelID,
		MediaGeneration: 9, State: models.ViewerStateActive, LastSeenAt: &seen, CreatedAt: now, UpdatedAt: now,
	}
}

func nativeRevocationClient(t *testing.T, db *gorm.DB, id int64) models.Client {
	t.Helper()
	var row models.Client
	require.NoError(t, db.First(&row, "id = ?", id).Error)
	return row
}

func nativeRevocationGrant(t *testing.T, db *gorm.DB, id string) models.PlayGrant {
	t.Helper()
	var row models.PlayGrant
	require.NoError(t, db.First(&row, "grant_id = ?", id).Error)
	return row
}

func nativeRevocationViewerRow(t *testing.T, db *gorm.DB, grantID string) models.Viewer {
	t.Helper()
	var row models.Viewer
	require.NoError(t, db.First(&row, "grant_id = ?", grantID).Error)
	return row
}

func nativeRevocationAudit(clientID int64, reason string, now time.Time) models.Audit {
	return models.Audit{
		RequestID: uuid.NewString(), ClientID: &clientID, AKFingerprint: "native-revocation",
		ResourceType: "openapi_client", ResourceID: fmt.Sprint(clientID), Result: "success",
		ReasonClass: reason, Source: "native-revocation-test", CreatedAt: now, CompletedAt: &now,
	}
}

func nativeRevocationAuditCount(t *testing.T, db *gorm.DB, clientID int64, reason string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&models.Audit{}).Where("client_id = ? AND reason_class = ?", clientID, reason).Count(&count).Error)
	return count
}

func cleanupNativeRevocationClients(t *testing.T, root *gorm.DB, clientIDs []int64) {
	t.Helper()
	if len(clientIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := root.WithContext(ctx)
	for _, clientID := range clientIDs {
		if err := db.Where("grant_id IN (?)", db.Model(&models.PlayGrant{}).Select("grant_id").Where("client_id = ?", clientID)).Delete(&models.Viewer{}).Error; err != nil {
			t.Errorf("native revocation viewer cleanup failed: %v", err)
		}
		if err := db.Where("client_id = ?", clientID).Delete(&models.PlayGrant{}).Error; err != nil {
			t.Errorf("native revocation grant cleanup failed: %v", err)
		}
		if err := db.Where("client_id = ?", clientID).Delete(&models.ClientScope{}).Error; err != nil {
			t.Errorf("native revocation scope cleanup failed: %v", err)
		}
		if err := db.Where("client_id = ?", clientID).Delete(&models.Audit{}).Error; err != nil {
			t.Errorf("native revocation audit cleanup failed: %v", err)
		}
		if err := db.Where("id = ?", clientID).Delete(&models.Client{}).Error; err != nil {
			t.Errorf("native revocation client cleanup failed: %v", err)
		}
	}
}
