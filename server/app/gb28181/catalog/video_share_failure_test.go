package catalog_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/videoshare"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func newPendingCatalogFixture(t *testing.T) (*gorm.DB, catalog.Sender, catalog.CatalogItem) {
	t.Helper()
	db := newPipelineTestDB(t)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceVideoSharePolicy{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	device := gbmodels.GbDevice{DeviceID: "37011200001170000031", OwnerDeptID: 1}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceVideoSharePolicy{
		DevicePK: device.ID, DefaultEnabled: true, Source: "config", PolicyRevision: 3, InitializedAt: device.CreatedAt,
	}).Error)
	return db, catalog.Sender{OwnerDeptID: 1, SourceDeviceID: device.DeviceID}, catalog.CatalogItem{
		DeviceID: "37011200001320000031", ParentID: device.DeviceID, CivilCode: "370112", Name: "auto sharing",
	}
}

func assertSinglePendingCatalogShare(t *testing.T, db *gorm.DB) gbmodels.GbChannelVideoShare {
	t.Helper()
	var channels []gbmodels.GbChannel
	require.NoError(t, db.Find(&channels).Error)
	require.Len(t, channels, 1)
	var shares []gbmodels.GbChannelVideoShare
	require.NoError(t, db.Find(&shares).Error)
	require.Len(t, shares, 1)
	share := shares[0]
	require.Equal(t, channels[0].ID, share.ChannelPK)
	require.True(t, share.Enabled)
	require.Equal(t, "auto", share.Source)
	require.Nil(t, share.ActivatedAt)
	require.Nil(t, share.ExpiresAt)
	require.Nil(t, share.CreatedBy)
	var logs int64
	require.NoError(t, db.Model(&basemodels.SysOperationLog{}).Count(&logs).Error)
	require.EqualValues(t, 1, logs, "duplicate discovery must not create duplicate auto-pending audits")
	return share
}

func TestCatalogPendingShareWriteFailureRollsBackChannelAndRetries(t *testing.T) {
	db, sender, item := newPendingCatalogFixture(t)
	// Fail an actual SQL write after channel insertion, instead of returning a
	// canned service error before the transaction has anything to roll back.
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_pending_share BEFORE INSERT ON gb_channel_video_share
		BEGIN SELECT RAISE(ABORT, 'injected pending-share write failure'); END`).Error)
	pipeline := catalog.New(db)
	err := pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{item})
	require.ErrorContains(t, err, "injected pending-share write failure")
	for _, model := range []any{&gbmodels.GbChannel{}, &gbmodels.GbChannelMount{}, &gbmodels.GbCatalogNode{},
		&gbmodels.GbChannelVideoShare{}, &basemodels.SysOperationLog{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count, "failed item must leave no partially initialized rows: %T", model)
	}
	require.NoError(t, db.Exec("DROP TRIGGER fail_pending_share").Error)
	// Retry is explicitly a new catalog delivery after the dependency recovers;
	// the failed delivery above is required to return its error to the caller.
	for i := 0; i < 2; i++ {
		require.NoError(t, pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))
	}
	assertSinglePendingCatalogShare(t, db)
}

func TestCatalogConcurrentDiscoveryCreatesOnePendingShare(t *testing.T) {
	seed, sender, item := newPendingCatalogFixture(t)
	path := filepath.Join(t.TempDir(), "catalog-share.db")
	require.NoError(t, seed.Exec("VACUUM INTO ?", path).Error)
	// Independent physical pools exercise database serialization. SQLite's own
	// busy timeout waits for the concurrent writer; no failed call is ignored or
	// retried by this test, and every original Ingest must return nil.
	pools := make([]*gorm.DB, 2)
	for i := range pools {
		db, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
		require.NoError(t, err)
		raw, err := db.DB()
		require.NoError(t, err)
		raw.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = raw.Close() })
		pools[i] = db
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var ready sync.WaitGroup
	ready.Add(cap(results))
	for i := 0; i < cap(results); i++ {
		pipeline := catalog.New(pools[i%len(pools)])
		go func() {
			ready.Done()
			<-start
			results <- pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{item, item})
		}()
	}
	ready.Wait()
	close(start)
	for i := 0; i < cap(results); i++ {
		select {
		case err := <-results:
			require.NoError(t, err, "concurrent discovery call %d must succeed; locks are not accepted as passing", i)
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent catalog discovery did not complete")
		}
	}
	assertSinglePendingCatalogShare(t, pools[0])
}

func TestCatalogAndNotifyDoNotReopenNaturallyExpiredShare(t *testing.T) {
	db, sender, item := newPendingCatalogFixture(t)
	pipeline := catalog.New(db)
	require.NoError(t, pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{item}))
	share := assertSinglePendingCatalogShare(t, db)
	// Persist an already elapsed activated row with no revocation marker. This
	// tests natural-expiry state without claiming a real three-minute wait.
	activated, expiry := time.Now().UTC().Add(-6*time.Minute), time.Now().UTC().Add(-time.Minute)
	creator := uint(7)
	share.ActivatedAt, share.ExpiresAt, share.CreatedBy = &activated, &expiry, &creator
	require.NoError(t, db.Save(&share).Error)
	require.NoError(t, db.Take(&share, share.ID).Error)
	signer, err := videoshare.NewSigner(make([]byte, 32))
	require.NoError(t, err)
	service, err := videoshare.NewService(db, signer, func() time.Duration { return 5 * time.Minute },
		func(context.Context, *gorm.DB, uint, uint) (videoshare.AuthorizationSnapshot, error) {
			return videoshare.AuthorizationSnapshot{DevicePK: share.DevicePK, DeviceID: share.DeviceID, DeviceEpoch: share.DeviceEpoch}, nil
		})
	require.NoError(t, err)
	for _, action := range []string{"catalog", "ADD", "UPDATE", "catalog", "ADD"} {
		if action == "catalog" {
			err = pipeline.Ingest(context.Background(), sender, []catalog.CatalogItem{item})
		} else {
			// The production Catalog NOTIFY processor dispatches these events to
			// this shared pipeline; the SIP dialog itself is tested separately.
			err = pipeline.IngestDelta(context.Background(), sender, action, item)
		}
		require.NoError(t, err, action)
		var after gbmodels.GbChannelVideoShare
		require.NoError(t, db.Take(&after, share.ID).Error)
		require.Equal(t, share, after, "%s must preserve expiry, generation, revision and all lifecycle fields", action)
		status, err := service.Status(context.Background(), share.ChannelPK, creator)
		require.NoError(t, err)
		require.False(t, status.Enabled)
		require.True(t, status.Expired)
		require.False(t, status.Pending)
	}
}
