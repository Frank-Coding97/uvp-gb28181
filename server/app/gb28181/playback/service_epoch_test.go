package playback

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func playbackEpochFixture(t *testing.T) (*gorm.DB, *playauth.DeviceOperationBarrier, CreateRequest) {
	t.Helper()
	db := authoritytest.OpenSQLite(t, filepath.Join(t.TempDir(), "playback.sqlite"))
	require.NoError(t, db.Exec(`CREATE TABLE gb_device (id INTEGER PRIMARY KEY, device_id TEXT,
		access_epoch INTEGER, cleanup_completed_epoch INTEGER, legacy_revoked_before DATETIME, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec("INSERT INTO gb_device(id, device_id, access_epoch, cleanup_completed_epoch) VALUES (1, '34020000002000000001', 1, 1)").Error)
	require.NoError(t, db.AutoMigrate(&playauth.DeviceOperationIntent{}))
	req := validCreate(time.Now())
	req.DeviceID, req.ChannelID, req.SIPChannelID = "34020000002000000001", "2", "34020000001320000001"
	req.Authorization = AuthorizationSnapshot{DevicePK: 1, DeviceEpoch: 1, CleanupCompletedEpoch: 1,
		DeviceCode: req.DeviceID, ChannelPK: 2, ChannelCode: req.SIPChannelID}
	return db, newAuthorizedBarrierTest(t, db), req
}

func TestPlaybackOriginalEpochPreflightBeforeAnySessionOrIO(t *testing.T) {
	for _, state := range []string{"transferred", "pending", "schema-missing", "snapshot-missing", "target-mismatch", "barrier-missing"} {
		t.Run(state, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, barrier, req := playbackEpochFixture(t)
			switch state {
			case "transferred":
				require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2").Error)
			case "pending":
				require.NoError(t, db.Exec("UPDATE gb_device SET cleanup_completed_epoch=0").Error)
			case "schema-missing":
				require.NoError(t, db.Exec("ALTER TABLE gb_device DROP COLUMN access_epoch").Error)
			case "snapshot-missing":
				req.Authorization = AuthorizationSnapshot{}
			case "target-mismatch":
				req.SIPChannelID = "34020000001320000002"
			case "barrier-missing":
				barrier = nil
			}
			h := &heldPlaybackStage{}
			r := NewRegistry(RegistryConfig{})
			s := NewService(r, h, h, h, h, ServiceConfig{DeviceOperations: barrier})
			_, err := s.Create(context.Background(), req)
			require.Error(t, err)
			require.Zero(t, r.Size())
			require.Zero(t, h.node.Load())
			require.Zero(t, h.open.Load())
			require.Zero(t, h.invite.Load())
			require.NoError(t, s.Close(context.Background()))
		})
	}
}

func TestPlaybackPreflightIsNotLeaseOrDurableOperation(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	db, barrier, req := playbackEpochFixture(t)
	h := &heldPlaybackStage{}
	s := NewService(NewRegistry(RegistryConfig{}), h, h, h, h, ServiceConfig{DeviceOperations: barrier})
	defer s.Close(context.Background())
	created, err := s.Create(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, req.Authorization, created.Session.Authorization)
	var count int64
	require.NoError(t, db.Model(&playauth.DeviceOperationIntent{}).Count(&count).Error)
	require.Zero(t, count, "this preparation packet must not create ownerless durable operations")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2), "preflight is not lane ownership")
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2").Error)
	_, err = s.Create(context.Background(), req)
	require.Error(t, err, "preflight must run even before reusing an existing session")
	require.EqualValues(t, 1, h.node.Load())
}

func TestPlaybackRegistryIdempotencyBindsAuthorizationIdentity(t *testing.T) {
	mutations := map[string]func(*CreateRequest){
		"epoch":        func(r *CreateRequest) { r.Authorization.DeviceEpoch++ },
		"device-pk":    func(r *CreateRequest) { r.Authorization.DevicePK++ },
		"device-code":  func(r *CreateRequest) { r.Authorization.DeviceCode = "34020000002000000002" },
		"channel-pk":   func(r *CreateRequest) { r.Authorization.ChannelPK++ },
		"channel-code": func(r *CreateRequest) { r.Authorization.ChannelCode = "34020000001320000002" },
		"mode":         func(r *CreateRequest) { r.Mode = ModeDownload },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			_, _, req := playbackEpochFixture(t)
			r := NewRegistry(RegistryConfig{})
			created, err := r.Create(context.Background(), req)
			require.NoError(t, err)
			original := req
			mutate(&req)
			_, err = r.Create(context.Background(), req)
			require.ErrorIs(t, err, ErrPlaybackBusy)
			req.IdempotencyKey = "different-key"
			_, err = r.Create(context.Background(), req)
			require.ErrorIs(t, err, ErrPlaybackBusy, "active-scope reuse must also compare the identity")
			reused, err := r.Create(context.Background(), original)
			require.NoError(t, err)
			require.True(t, reused.Existing)
			require.Equal(t, created.Session.ID, reused.Session.ID)
		})
	}
}
