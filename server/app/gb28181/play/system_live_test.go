package play

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

func newDeviceOperationSQLFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "device-operations.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (
		id INTEGER PRIMARY KEY, device_id TEXT, access_epoch INTEGER, cleanup_completed_epoch INTEGER,
		legacy_revoked_before DATETIME NULL, deleted_at DATETIME NULL)`).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(id,device_id,access_epoch,cleanup_completed_epoch) VALUES(1,?,1,1)", onlineDevice().DeviceID).Error)
	return db
}

func TestOpenAPISystemLiveUsesStableSnapshotWithoutUpgradingCaller(t *testing.T) {
	db := newDeviceOperationSQLFixture(t)
	var captured Request
	s := &Service{liveCoordinator: NewCoordinator(func(_ context.Context, req Request) (*Result, error) {
		captured = req
		return &Result{StreamID: "system-test", Generation: 1}, nil
	})}
	trusted := NewSystemLiveEnsurer(s, playauth.NewDeviceSecurityStore(db))
	req := Request{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, Trigger: "recording-plan"}
	_, err := trusted.EnsureLive(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 1, captured.DeviceEpoch)
	require.Zero(t, req.DeviceEpoch, "the caller request is not mutated")
	for _, invalid := range []Request{
		{DeviceID: req.DeviceID, DeviceEpoch: 1},
		{DeviceID: req.DeviceID, AuthorizationID: "old-authorization"},
		{DeviceID: req.DeviceID, QualificationID: "external-ticket"},
	} {
		_, err := trusted.EnsureLive(context.Background(), invalid)
		require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable, "system adapter cannot refresh an existing authority")
	}
}

func TestAutoOnDemandEnsurerUsesCurrentEpochWhenPlaybackAuthIsDisabled(t *testing.T) {
	db := newDeviceOperationSQLFixture(t)
	var captured Request
	s := &Service{operationBarrier: playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))}
	s.liveCoordinator = NewCoordinator(func(_ context.Context, req Request) (*Result, error) {
		captured = req
		return &Result{StreamID: "auto-on-demand-test", Generation: 1}, nil
	})
	ensurer := NewAutoOnDemandEnsurer(s, playauth.NewDeviceSecurityStore(db))

	result, err := ensurer.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		RequiredNode: 7, Trigger: "on_stream_not_found",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.EqualValues(t, 1, captured.DeviceEpoch)
	require.Equal(t, int64(7), captured.RequiredNode)
}

func TestAutoOnDemandEnsurerRejectsPendingDeviceCleanup(t *testing.T) {
	db := newDeviceOperationSQLFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch=0 WHERE id=1").Error)
	called := false
	s := &Service{}
	s.liveCoordinator = NewCoordinator(func(context.Context, Request) (*Result, error) {
		called = true
		return &Result{StreamID: "must-not-start"}, nil
	})
	ensurer := NewAutoOnDemandEnsurer(s, playauth.NewDeviceSecurityStore(db))

	_, err := ensurer.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		RequiredNode: 7,
	})

	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.False(t, called)
}

func TestAutoOnDemandEnsurerDoesNotRefreshExplicitDeviceEpoch(t *testing.T) {
	db := newDeviceOperationSQLFixture(t)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
	called := false
	s := &Service{operationBarrier: playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db))}
	s.liveCoordinator = NewCoordinator(func(context.Context, Request) (*Result, error) {
		called = true
		return &Result{StreamID: "must-not-start"}, nil
	})
	ensurer := NewAutoOnDemandEnsurer(s, playauth.NewDeviceSecurityStore(db))

	_, err := ensurer.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
	})

	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.False(t, called)
}

func TestOpenAPISystemLivePendingOrCorruptStateNeverCallsPlayer(t *testing.T) {
	for _, sql := range []string{
		"UPDATE gb_device SET access_epoch=2",
		"UPDATE gb_device SET cleanup_completed_epoch=NULL",
		"UPDATE gb_device SET cleanup_completed_epoch=0",
		"UPDATE gb_device SET cleanup_completed_epoch=2",
		"ALTER TABLE gb_device DROP COLUMN cleanup_completed_epoch",
	} {
		t.Run(sql, func(t *testing.T) {
			db := newDeviceOperationSQLFixture(t)
			require.NoError(t, db.Exec(sql).Error)
			called := false
			s := &Service{liveCoordinator: NewCoordinator(func(context.Context, Request) (*Result, error) {
				called = true
				return &Result{StreamID: "must-not-start"}, nil
			})}
			_, err := NewSystemLiveEnsurer(s, playauth.NewDeviceSecurityStore(db)).EnsureLive(context.Background(), Request{DeviceID: onlineDevice().DeviceID})
			require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
			require.False(t, called)
		})
	}
}
