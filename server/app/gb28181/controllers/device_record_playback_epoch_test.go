package controllers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
	"uvplatform.cn/uvp-gb28181/app/gb28181/recordquery"
)

type playbackSnapshotFunc func(recordquery.ResolveRequest) (recordquery.Snapshot, error)

func (f playbackSnapshotFunc) Resolve(r recordquery.ResolveRequest) (recordquery.Snapshot, error) {
	return f(r)
}

type epochPlaybackIO struct{ calls int }

func (p *epochPlaybackIO) Pick(context.Context, gbplayback.PickRequest) (gbplayback.NodeInfo, error) {
	p.calls++
	return gbplayback.NodeInfo{}, errors.New("fixture ends at first external boundary")
}
func (*epochPlaybackIO) Open(context.Context, gbplayback.RTPRequest) (gbplayback.RTPAllocation, error) {
	panic("unexpected RTP dispatch")
}
func (*epochPlaybackIO) Invite(context.Context, gbplayback.UACInvite) (gbplayback.DialogInfo, error) {
	panic("unexpected SIP dispatch")
}
func (*epochPlaybackIO) Teardown(context.Context, string) error { return nil }
func (*epochPlaybackIO) Wait(context.Context, string) (gbplayback.MediaReady, error) {
	panic("unexpected media wait")
}

func TestPlaybackHTTPOriginalEpochRejectedByActualService(t *testing.T) {
	for _, mode := range []string{"playback", "download"} {
		t.Run(mode, func(t *testing.T) {
			f, _, now := newPlaybackHTTPFixture(t)
			io := &epochPlaybackIO{}
			registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{})
			barrier := playauth.NewDeviceOperationBarrier(playauth.NewDeviceSecurityStore(f.db))
			service := gbplayback.NewService(registry, io, io, io, io, gbplayback.ServiceConfig{DeviceOperations: barrier})
			defer service.Close(context.Background())
			f.controller.SetPlaybackRuntime(service, playbackSnapshotFunc(func(recordquery.ResolveRequest) (recordquery.Snapshot, error) {
				// Even if cleanup has finished, an old permission snapshot cannot
				// use the newly completed epoch to authorize a new session.
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2, owner_dept_id=20").Error)
				require.NoError(t, f.db.Exec("UPDATE gb_channel SET owner_dept_id=20").Error)
				return recordquery.Snapshot{RecordKey: "opaque-key", DeviceCode: f.device.DeviceID, ChannelCode: f.channel.ChannelID,
					ChannelID: f.channel.ID, SegmentStart: now, SegmentEnd: now.AddDate(0, 0, 1)}, nil
			}))
			w := postPlaybackEpochRequest(f, mode)
			require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
			require.Zero(t, io.calls)
			require.Zero(t, registry.Size())
		})
	}
}

func postPlaybackEpochRequest(f recordQueryFixture, mode string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(f.channel.ID)+"/"+mode+"-sessions", strings.NewReader(`{"recordKey":"opaque-key","playFrom":"2026-08-04T08:00:00"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-epoch")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func TestPlaybackPermissionCapturesOriginalEpochBeforeSnapshotResolve(t *testing.T) {
	for _, mode := range []string{"playback", "download"} {
		t.Run(mode, func(t *testing.T) {
			f, service, now := newPlaybackHTTPFixture(t)
			f.controller.SetPlaybackRuntime(service, playbackSnapshotFunc(func(recordquery.ResolveRequest) (recordquery.Snapshot, error) {
				// The permission query has already returned. Never reread epoch 2
				// as authority for this request after resolving the record key.
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, owner_dept_id=20").Error)
				require.NoError(t, f.db.Exec("UPDATE gb_channel SET owner_dept_id=20").Error)
				return recordquery.Snapshot{RecordKey: "opaque-key", DeviceCode: f.device.DeviceID, ChannelCode: f.channel.ChannelID,
					ChannelID: f.channel.ID, SegmentStart: now, SegmentEnd: now.AddDate(0, 0, 1)}, nil
			}))
			w := postPlaybackEpochRequest(f, mode)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String()) // Service fake only captures the original snapshot.
			a := service.created.Authorization
			require.EqualValues(t, 1, a.DeviceEpoch)
			require.EqualValues(t, f.device.ID, a.DevicePK)
			require.EqualValues(t, f.channel.ID, a.ChannelPK)
			require.Equal(t, f.device.DeviceID, a.DeviceCode)
			require.Equal(t, f.channel.ChannelID, a.ChannelCode)
			require.EqualValues(t, 1, a.CleanupCompletedEpoch)
			require.NotContains(t, w.Body.String(), "DeviceEpoch")
		})
	}
}

func TestPlaybackPermissionSnapshotRejectsUnsafeRoot(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE gb_device SET access_epoch=NULL",
		"UPDATE gb_device SET access_epoch=0",
		"UPDATE gb_device SET access_epoch=2",
		"UPDATE gb_device SET cleanup_completed_epoch=2",
		"UPDATE gb_device SET owner_dept_id=20",
		"ALTER TABLE gb_device DROP COLUMN access_epoch",
	} {
		t.Run(mutation, func(t *testing.T) {
			f, service, _ := newPlaybackHTTPFixture(t)
			require.NoError(t, f.db.Exec(mutation).Error)
			w := postPlaybackEpochRequest(f, "playback")
			require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
			require.Empty(t, service.created.OwnerID)
		})
	}
}

func TestPlaybackPermissionSnapshotPreservesPersonnelSharing(t *testing.T) {
	f, service, _ := newPlaybackHTTPFixture(t)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET owner_dept_id=20").Error)
	require.NoError(t, f.db.Exec("UPDATE gb_channel SET owner_dept_id=20").Error)
	require.NoError(t, f.db.Create(&gbmodels.GbDeviceGrant{DeviceID: f.device.ID, TargetType: gbmodels.GrantTargetTypeUser, TargetID: 100}).Error)
	w := postPlaybackEpochRequest(f, "playback")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.EqualValues(t, 1, service.created.Authorization.DeviceEpoch)
}
