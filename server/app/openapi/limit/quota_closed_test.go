package limit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIQuotaClosedViewerReleasesOnlyDurableBoundGrant(t *testing.T) {
	cases := []struct {
		name        string
		grantState  models.GrantState
		viewerState *models.ViewerState
		want        int64
	}{
		{name: "bound missing viewer", grantState: models.GrantStateBound, want: 1},
		{name: "bound active viewer", grantState: models.GrantStateBound, viewerState: viewerStatePtr(models.ViewerStateActive), want: 1},
		{name: "bound revoke pending viewer", grantState: models.GrantStateBound, viewerState: viewerStatePtr(models.ViewerStateRevokePending), want: 1},
		{name: "bound closed viewer", grantState: models.GrantStateBound, viewerState: viewerStatePtr(models.ViewerStateClosed), want: 0},
		{name: "expired live viewer", grantState: models.GrantStateExpired, viewerState: viewerStatePtr(models.ViewerStateActive), want: 1},
		{name: "expired closed viewer", grantState: models.GrantStateExpired, viewerState: viewerStatePtr(models.ViewerStateClosed), want: 0},
		{name: "revoked live viewer", grantState: models.GrantStateRevoked, viewerState: viewerStatePtr(models.ViewerStateRevokePending), want: 1},
		{name: "revoked closed viewer", grantState: models.GrantStateRevoked, viewerState: viewerStatePtr(models.ViewerStateClosed), want: 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db := quotaFixture(t, 1)
			grant := quotaMediaGrant("00000000-0000-4000-8000-000000000901", test.grantState, quotaTestNow.Add(-time.Minute))
			require.NoError(t, db.Create(&grant).Error)
			if test.viewerState != nil {
				viewer := quotaViewerForGrant(grant.GrantID, *test.viewerState)
				require.NoError(t, db.Create(&viewer).Error)
			}

			quota := NewQuota(db, func() time.Time { return quotaTestNow })
			occupied, err := quota.Occupied(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, test.want, occupied)
			if test.name == "bound closed viewer" {
				_, err := quota.ReservePending(context.Background(), ReservationRequest{ClientID: 1, Scope: PlayLiveApplyScope, DeviceID: "device-a", ChannelID: "channel-a"})
				require.NoError(t, err, "a closed bound viewer must release the quota")
			}
		})
	}
}

func quotaViewerForGrant(grantID string, state models.ViewerState) models.Viewer {
	return models.Viewer{
		GrantID: grantID, NodeUUID: "node-a", BootNonce: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Identifier: "quota-viewer",
		Schema: "ws", VHost: "__defaultVhost__", App: "live", Stream: "stream-a", MediaGeneration: 1,
		State: state, CreatedAt: quotaTestNow, UpdatedAt: quotaTestNow,
	}
}

func viewerStatePtr(state models.ViewerState) *models.ViewerState { return &state }
