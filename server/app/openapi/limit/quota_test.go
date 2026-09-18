package limit

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIQuotaReservesAtMostConfiguredLimit(t *testing.T) {
	db := quotaFixture(t, 10)
	quota := NewQuota(db, func() time.Time { return quotaTestNow })

	results := make(chan error, 11)
	var wg sync.WaitGroup
	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var accepted, rejected int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrQuotaExceeded):
			rejected++
		default:
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	require.Equal(t, 10, accepted)
	require.Equal(t, 1, rejected)
	occupied, err := quota.Occupied(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 10, occupied)
}

func TestOpenAPIQuotaClientIsolationAndServiceRebuild(t *testing.T) {
	db := quotaFixture(t, 10)
	second := models.Client{ID: 2, AK: "ak-2", Name: "client-2", OwnerDeptID: 1, Status: models.StatusActive,
		SecretCiphertext: []byte("cipher"), SecretIV: []byte("iv"), SecretKeyID: "key", SecretVersion: 1,
		AuthEpoch: 1, RateLimit: 10, Burst: 20, ViewerQuota: 2, RowVersion: 1, CreatedAt: quotaTestNow, UpdatedAt: quotaTestNow}
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: 2, Scope: "play:live:apply", Enabled: true, ScopeEpoch: 1, UpdatedAt: quotaTestNow}).Error)

	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	for i := 0; i < 10; i++ {
		_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
		require.NoError(t, err)
	}
	_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaExceeded)

	for i := 0; i < 2; i++ {
		_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 2, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
		require.NoError(t, err)
	}
	_, err = quota.ReservePending(context.Background(), ReservationRequest{ClientID: 2, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaExceeded)

	rebuilt := NewQuota(db, func() time.Time { return quotaTestNow })
	_, err = rebuilt.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestOpenAPIQuotaExpiryReleasesPendingButNotLiveViewer(t *testing.T) {
	now := quotaTestNow
	db := quotaFixture(t, 1)
	quota := NewQuota(db, func() time.Time { return now })

	first, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.NoError(t, err)
	var firstGrant models.PlayGrant
	require.NoError(t, db.First(&firstGrant, "grant_id = ?", first.GrantID).Error)
	require.EqualValues(t, 3, firstGrant.DeviceEpoch)
	now = now.Add(31 * time.Second)
	second, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.NoError(t, err)
	require.NotEqual(t, first.GrantID, second.GrantID)

	var grants []models.PlayGrant
	require.NoError(t, db.Find(&grants).Error)
	require.Len(t, grants, 2)
	var expiredPending models.PlayGrant
	require.NoError(t, db.First(&expiredPending, "grant_id = ?", first.GrantID).Error)
	require.Equal(t, models.GrantStateFailed, expiredPending.State)
	require.Equal(t, "pending_timeout", expiredPending.Reason)

	// A bound grant remains occupied even after its grant TTL. A revoked grant
	// with a real pending viewer is also retained until that viewer is closed.
	require.NoError(t, db.Delete(&models.PlayGrant{}, "grant_id = ?", second.GrantID).Error)
	bound := quotaMediaGrant("00000000-0000-4000-8000-000000000101", models.GrantStateBound, now.Add(-time.Minute))
	require.NoError(t, db.Create(&bound).Error)
	require.NoError(t, db.Create(&models.Viewer{GrantID: bound.GrantID, NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "1-1", Schema: "ws", VHost: "__defaultVhost__", App: "live", Stream: "stream-a", MediaGeneration: 1, State: models.ViewerStateActive, CreatedAt: now, UpdatedAt: now}).Error)

	revoked := quotaMediaGrant("00000000-0000-4000-8000-000000000102", models.GrantStateRevoked, now.Add(-time.Minute))
	// A revoked grant need not retain a binding, but its real viewer does.
	revoked.DeviceID = nil
	revoked.ChannelID = nil
	revoked.NodeUUID = nil
	revoked.BootNonce = nil
	revoked.Schema = nil
	revoked.VHost = nil
	revoked.App = nil
	revoked.Stream = nil
	revoked.MediaGeneration = nil
	revoked.Protocol = nil
	require.NoError(t, db.Create(&revoked).Error)
	require.NoError(t, db.Create(&models.Viewer{GrantID: revoked.GrantID, NodeUUID: "node-a", BootNonce: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Identifier: "2-1", Schema: "ws", VHost: "__defaultVhost__", App: "live", Stream: "stream-b", MediaGeneration: 1, State: models.ViewerStatePending, CreatedAt: now, UpdatedAt: now}).Error)

	occupied, err := quota.Occupied(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, occupied)
	_, err = quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestOpenAPIQuotaCountsGrantOnceAcrossPendingToBound(t *testing.T) {
	db := quotaFixture(t, 1)
	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	reservation, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.NoError(t, err)

	grant := quotaMediaGrant(reservation.GrantID, models.GrantStateBound, quotaTestNow)
	require.NoError(t, db.Model(&models.PlayGrant{}).Where("grant_id = ?", reservation.GrantID).Updates(map[string]any{
		"state": grant.State, "device_id": grant.DeviceID, "channel_id": grant.ChannelID, "node_uuid": grant.NodeUUID,
		"boot_nonce": grant.BootNonce, "schema": grant.Schema, "vhost": grant.VHost, "app": grant.App,
		"stream": grant.Stream, "media_generation": grant.MediaGeneration, "protocol": grant.Protocol,
	}).Error)

	occupied, err := quota.Occupied(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, occupied)
	_, err = quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestOpenAPIQuotaReleaseExpiredIssuedGrantKeepsLiveViewer(t *testing.T) {
	db := quotaFixture(t, 2)
	pending := quotaMediaGrant("00000000-0000-4000-8000-000000000200", models.GrantStatePending, quotaTestNow.Add(-time.Minute))
	require.NoError(t, db.Create(&pending).Error)
	expired := quotaMediaGrant("00000000-0000-4000-8000-000000000201", models.GrantStateIssued, quotaTestNow.Add(-time.Minute))
	require.NoError(t, db.Create(&expired).Error)

	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	released, err := quota.ReleaseExpired(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, released)
	var stored models.PlayGrant
	require.NoError(t, db.First(&stored, "grant_id = ?", pending.GrantID).Error)
	require.Equal(t, models.GrantStateFailed, stored.State)
	require.Equal(t, "pending_timeout", stored.Reason)
	stored = models.PlayGrant{}
	require.NoError(t, db.First(&stored, "grant_id = ?", expired.GrantID).Error)
	require.Equal(t, models.GrantStateExpired, stored.State)
	require.Equal(t, "issued_timeout", stored.Reason)

	protected := quotaMediaGrant("00000000-0000-4000-8000-000000000202", models.GrantStateIssued, quotaTestNow.Add(-time.Minute))
	require.NoError(t, db.Create(&protected).Error)
	require.NoError(t, db.Create(&models.Viewer{GrantID: protected.GrantID, NodeUUID: "node-a", BootNonce: "cccccccccccccccccccccccccccccccc", Identifier: "3-1", Schema: "ws", VHost: "__defaultVhost__", App: "live", Stream: "stream-c", MediaGeneration: 1, State: models.ViewerStateRevokePending, CreatedAt: quotaTestNow, UpdatedAt: quotaTestNow}).Error)
	released, err = quota.ReleaseExpired(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, released)
	stored = models.PlayGrant{}
	require.NoError(t, db.First(&stored, "grant_id = ?", protected.GrantID).Error)
	require.Equal(t, models.GrantStateIssued, stored.State)
}

func TestOpenAPIQuotaReleaseExpiredIsBoundedToOneBatch(t *testing.T) {
	db := quotaFixture(t, 1)
	grants := make([]models.PlayGrant, 0, 501)
	for i := 0; i < 501; i++ {
		grants = append(grants, quotaMediaGrant(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+300), models.GrantStatePending, quotaTestNow.Add(-time.Minute)))
	}
	require.NoError(t, db.CreateInBatches(&grants, 100).Error)

	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	released, err := quota.ReleaseExpired(context.Background(), 1)
	require.NoError(t, err)
	require.EqualValues(t, 500, released)
	var pending int64
	require.NoError(t, db.Model(&models.PlayGrant{}).Where("client_id = ? AND state = ?", 1, models.GrantStatePending).Count(&pending).Error)
	require.EqualValues(t, 1, pending)
	occupied, err := quota.Occupied(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, occupied)
}

func TestOpenAPIQuotaRejectsInvalidInputAndDatabaseFailure(t *testing.T) {
	db := quotaFixture(t, 1)
	quota := NewQuota(db, func() time.Time { return quotaTestNow })
	for _, request := range []ReservationRequest{
		{},
		{ClientID: 1},
		{ClientID: 0, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"},
		{ClientID: 1, Scope: PlayLiveApplyScope},
		{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a"},
		{ClientID: 1, Scope: "device:list", DeviceID: "device-a", ChannelID: "channel-a"},
	} {
		_, err := quota.ReservePending(context.Background(), request)
		require.ErrorIs(t, err, ErrInvalidReservation)
	}
	_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-deleted", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaUnavailable)

	missing := NewQuota(mustQuotaDB(t), func() time.Time { return quotaTestNow })
	_, err = missing.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaUnavailable)

	var nilQuota *Quota
	_, err = nilQuota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
	require.ErrorIs(t, err, ErrQuotaUnavailable)
}

const quotaNowUnix = int64(1790000000)

var quotaTestNow = time.Unix(quotaNowUnix, 0).UTC()

func quotaFixture(t *testing.T, viewerQuota int) *gorm.DB {
	t.Helper()
	db := mustQuotaDB(t)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.PlayGrant{}, &models.Viewer{}))
	require.NoError(t, db.Exec("CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id TEXT NOT NULL UNIQUE, access_epoch INTEGER NOT NULL DEFAULT 1, cleanup_completed_epoch INTEGER DEFAULT 1, deleted_at DATETIME NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch) VALUES (1, 'device-a', 3, 3)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch, deleted_at) VALUES (2, 'device-deleted', 3, 3, ?)", quotaTestNow).Error)
	client := models.Client{ID: 1, AK: "ak-1", Name: "client-1", OwnerDeptID: 1, Status: models.StatusActive,
		SecretCiphertext: []byte("cipher"), SecretIV: []byte("iv"), SecretKeyID: "key", SecretVersion: 1,
		AuthEpoch: 1, RateLimit: 10, Burst: 20, ViewerQuota: viewerQuota, RowVersion: 1, CreatedAt: quotaTestNow, UpdatedAt: quotaTestNow}
	require.NoError(t, db.Create(&client).Error)
	require.NoError(t, db.Create(&models.ClientScope{ClientID: 1, Scope: "play:live:apply", Enabled: true, ScopeEpoch: 1, UpdatedAt: quotaTestNow}).Error)
	t.Cleanup(func() {
		raw, err := db.DB()
		if err == nil {
			_ = raw.Close()
		}
	})
	return db
}

func mustQuotaDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quota.sqlite")
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	return db
}

func quotaMediaGrant(id string, state models.GrantState, at time.Time) models.PlayGrant {
	generation := uint64(1)
	return models.PlayGrant{GrantID: id, ClientID: 1, Scope: "play:live:apply", DeviceID: quotaStringPtr("device-a"), ChannelID: quotaStringPtr("channel-a"),
		ClientEpoch: 1, ScopeEpoch: 1, DeviceEpoch: 1, NodeUUID: quotaStringPtr("node-a"), BootNonce: quotaStringPtr("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Schema: quotaStringPtr("ws"), VHost: quotaStringPtr("__defaultVhost__"), App: quotaStringPtr("live"), Stream: quotaStringPtr("stream-a"),
		MediaGeneration: &generation, Protocol: quotaStringPtr("ws-flv"), IssuedAt: at, ExpiresAt: at.Add(-time.Second), State: state,
		CreatedAt: at, UpdatedAt: at}
}

func quotaStringPtr(value string) *string { return &value }
