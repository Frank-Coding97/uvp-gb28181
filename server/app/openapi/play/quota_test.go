package play

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

var quotaNow = time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)

type quotaDevice struct {
	ID                    uint       `gorm:"primaryKey"`
	DeviceID              string     `gorm:"column:device_id;uniqueIndex"`
	AccessEpoch           int64      `gorm:"column:access_epoch"`
	CleanupCompletedEpoch int64      `gorm:"column:cleanup_completed_epoch"`
	DeletedAt             *time.Time `gorm:"column:deleted_at"`
}

func (quotaDevice) TableName() string { return "gb_device" }

func TestViewerQuotaReservesCompensatesAndExpiresPending(t *testing.T) {
	now := quotaNow
	db := quotaDB(t, 1)
	quota := NewViewerQuota(db, func() time.Time { return now })

	first, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)
	_, err = quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.ErrorIs(t, err, ErrQuotaExceeded)

	require.NoError(t, quota.Fail(context.Background(), 1, first.GrantID, "play_start_failed"))
	second, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)
	require.NotEqual(t, first.GrantID, second.GrantID)

	now = now.Add(pendingReservationTTL + time.Second)
	third, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)
	require.NotEqual(t, second.GrantID, third.GrantID)
}

func TestViewerQuotaBindsIdempotentlyAndFlowReportReleasesSlot(t *testing.T) {
	db := quotaDB(t, 1)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	reservation, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)

	claims := playauth.Claims{
		Version: 4, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
		DeviceEpoch: 3, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9,
		OpenAPIClientID: 1, OpenAPIGrantID: reservation.GrantID,
	}
	binding := playauth.OpenAPIViewerBinding{
		NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "session-a",
		Protocol: "http-flv", Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", MediaGeneration: 9,
	}
	require.NoError(t, quota.BindViewer(context.Background(), claims, binding))
	require.NoError(t, quota.BindViewer(context.Background(), claims, binding), "same ZLM connection must be idempotent")

	_, err = quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: claims.DeviceID, ChannelID: claims.ChannelID,
	})
	require.ErrorIs(t, err, ErrQuotaExceeded)

	require.NoError(t, quota.CloseViewer(context.Background(), playauth.OpenAPIFlowReport{
		NodeUUID: binding.NodeUUID, BootNonce: binding.BootNonce, Identifier: binding.Identifier,
		Protocol: binding.Protocol, Schema: binding.Schema, VHost: binding.VHost, App: binding.App, Stream: binding.Stream, Player: true,
	}))
	_, err = quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: claims.DeviceID, ChannelID: claims.ChannelID,
	})
	require.NoError(t, err)
}

type quotaSnapshotter struct {
	snapshot ViewerRuntimeSnapshot
	err      error
}

func (s quotaSnapshotter) SnapshotViewerRuntime(context.Context, ViewerRuntimeTarget) (ViewerRuntimeSnapshot, error) {
	return s.snapshot, s.err
}

func TestViewerQuotaReconcilesLostFlowReportFromSameMediaBoot(t *testing.T) {
	db := quotaDB(t, 1)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	reservation, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)
	claims := playauth.Claims{
		Version: 4, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
		DeviceEpoch: 3, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9,
		OpenAPIClientID: 1, OpenAPIGrantID: reservation.GrantID,
	}
	binding := playauth.OpenAPIViewerBinding{
		NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "session-a",
		Protocol: "http-flv", Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", MediaGeneration: 9,
	}
	require.NoError(t, quota.BindViewer(context.Background(), claims, binding))

	closed, err := quota.ReconcileViewers(context.Background(), quotaSnapshotter{snapshot: ViewerRuntimeSnapshot{
		BootNonce: binding.BootNonce, Identifiers: map[string]struct{}{},
	}}, 100)
	require.NoError(t, err)
	require.Equal(t, 1, closed)
	_, err = quota.Reserve(context.Background(), ReservationRequest{ClientID: 1, DeviceID: claims.DeviceID, ChannelID: claims.ChannelID})
	require.NoError(t, err, "same-boot runtime absence must release the leaked viewer quota")
}

func TestViewerQuotaReconcileFailsClosedForDifferentBootOrSnapshotFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot quotaSnapshotter
	}{
		{name: "different boot", snapshot: quotaSnapshotter{snapshot: ViewerRuntimeSnapshot{BootNonce: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Identifiers: map[string]struct{}{}}}},
		{name: "runtime unavailable", snapshot: quotaSnapshotter{err: errors.New("runtime unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := quotaDB(t, 1)
			quota := NewViewerQuota(db, func() time.Time { return quotaNow })
			reservation, err := quota.Reserve(context.Background(), ReservationRequest{ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006"})
			require.NoError(t, err)
			binding := playauth.OpenAPIViewerBinding{NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "session-a", Protocol: "http-flv", Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", MediaGeneration: 9}
			require.NoError(t, quota.BindViewer(context.Background(), playauth.Claims{Version: 4, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006", DeviceEpoch: 3, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9, OpenAPIClientID: 1, OpenAPIGrantID: reservation.GrantID}, binding))
			closed, reconcileErr := quota.ReconcileViewers(context.Background(), tc.snapshot, 100)
			if tc.snapshot.err != nil {
				require.Error(t, reconcileErr)
			} else {
				require.NoError(t, reconcileErr)
			}
			require.Zero(t, closed)
			_, err = quota.Reserve(context.Background(), ReservationRequest{ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006"})
			require.ErrorIs(t, err, ErrQuotaExceeded)
		})
	}
}

func TestViewerQuotaReconcilesFromAuthoritativeTargetWithoutBootIdentity(t *testing.T) {
	db := quotaDB(t, 1)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	reservation, err := quota.Reserve(context.Background(), ReservationRequest{ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006"})
	require.NoError(t, err)
	binding := playauth.OpenAPIViewerBinding{NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "session-a", Protocol: "http-flv", Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", MediaGeneration: 9}
	require.NoError(t, quota.BindViewer(context.Background(), playauth.Claims{Version: 4, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006", DeviceEpoch: 3, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9, OpenAPIClientID: 1, OpenAPIGrantID: reservation.GrantID}, binding))

	closed, err := quota.ReconcileViewers(context.Background(), quotaSnapshotter{snapshot: ViewerRuntimeSnapshot{
		TargetAuthoritative: true,
		Identifiers:         map[string]struct{}{},
	}}, 100)
	require.NoError(t, err)
	require.Equal(t, 1, closed)
}

func TestViewerQuotaRejectsGrantReplayAsAnotherConnection(t *testing.T) {
	db := quotaDB(t, 1)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	reservation, err := quota.Reserve(context.Background(), ReservationRequest{
		ClientID: 1, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
	})
	require.NoError(t, err)
	claims := playauth.Claims{
		Version: 4, DeviceID: "37010301021320000018", ChannelID: "37010301021320000006",
		DeviceEpoch: 3, App: "rtp", Stream: "stream-a", MediaServerID: "node-a", MediaGeneration: 9,
		OpenAPIClientID: 1, OpenAPIGrantID: reservation.GrantID,
	}
	first := playauth.OpenAPIViewerBinding{
		NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "session-a",
		Protocol: "http-flv", Schema: "http", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-a", MediaGeneration: 9,
	}
	require.NoError(t, quota.BindViewer(context.Background(), claims, first))
	second := first
	second.Identifier = "session-b"
	require.ErrorIs(t, quota.BindViewer(context.Background(), claims, second), ErrQuotaDenied)
}

func quotaDB(t *testing.T, viewerQuota int) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Client{}, &models.ClientScope{}, &models.PlayGrant{}, &models.Viewer{}, &quotaDevice{}))
	require.NoError(t, db.Create(&models.Client{
		ID: 1, AK: "uvp_test", Name: "quota client", OwnerDeptID: 1, DataScope: models.DataScopeDepartment,
		Status: models.StatusActive, SecretCiphertext: []byte("cipher"), SecretIV: []byte("iv"), SecretKeyID: "key",
		SecretVersion: 1, AuthEpoch: 4, RateLimit: 10, Burst: 20, ViewerQuota: viewerQuota, RowVersion: 1,
		CreatedAt: quotaNow, UpdatedAt: quotaNow,
	}).Error)
	require.NoError(t, db.Create(&models.ClientScope{
		ClientID: 1, Scope: "play:live", Enabled: true, ScopeEpoch: 5, UpdatedAt: quotaNow,
	}).Error)
	require.NoError(t, db.Create(&quotaDevice{
		ID: 1, DeviceID: "37010301021320000018", AccessEpoch: 3, CleanupCompletedEpoch: 3,
	}).Error)
	return db
}

var _ = errors.Is
