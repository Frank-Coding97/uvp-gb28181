package playauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/limit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIGrantIssueRequiresDeviceCleanupAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "pending", value: int64(3)},
		{name: "null", value: nil},
		{name: "nonpositive", value: int64(0)},
		{name: "ahead", value: int64(5)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOpenAPIGrantFixture(t)
			defer fixture.close(t)
			quota := limit.NewQuota(fixture.db, func() time.Time { return fixture.now })
			reservation, err := quota.ReservePending(context.Background(), limit.ReservationRequest{
				ClientID: testClientID, Scope: limit.PlayLiveApplyScope, DeviceID: testDeviceID, ChannelID: testChannelID,
			})
			require.NoError(t, err)
			service, err := NewOpenAPIGrantService(fixture.db, fixture.signer, fixture.authority, func() time.Time { return fixture.now })
			require.NoError(t, err)

			setOpenAPICleanupCompletedEpoch(t, fixture.db, test.value)
			_, err = service.Issue(context.Background(), openAPITestGrantIssueRequest(reservation.GrantID))
			require.ErrorIs(t, err, ErrOpenAPIGrantUnavailable)
			var pending models.PlayGrant
			require.NoError(t, fixture.db.First(&pending, "grant_id = ?", reservation.GrantID).Error)
			require.Equal(t, models.GrantStatePending, pending.State)

			setOpenAPICleanupCompletedEpoch(t, fixture.db, int64(4))
			issued, err := service.Issue(context.Background(), openAPITestGrantIssueRequest(reservation.GrantID))
			require.NoError(t, err)
			require.NotEmpty(t, issued.Token)
		})
	}
}

func TestOpenAPIViewerRequiresDeviceCleanupAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "pending", value: int64(3)},
		{name: "null", value: nil},
		{name: "nonpositive", value: int64(0)},
		{name: "ahead", value: int64(5)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newOpenAPIGrantFixture(t)
			defer fixture.close(t)
			service, token, _ := issueOpenAPITestGrant(t, fixture)

			setOpenAPICleanupCompletedEpoch(t, fixture.db, test.value)
			_, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("cleanup-gate"))
			require.ErrorIs(t, err, ErrOpenAPIViewerUnavailable)

			setOpenAPICleanupCompletedEpoch(t, fixture.db, int64(4))
			viewer, err := service.BindViewer(context.Background(), token, openAPIViewerRequest("cleanup-gate"))
			require.NoError(t, err)
			require.Equal(t, models.ViewerStateActive, viewer.State)
		})
	}
}

func openAPITestGrantIssueRequest(grantID string) OpenAPIGrantIssueRequest {
	return OpenAPIGrantIssueRequest{
		GrantID: grantID, DeviceID: testDeviceID, ChannelID: testChannelID, NodeUUID: testNodeUUID, BootNonce: testBootNonce,
		Schema: "rtmp", VHost: "__defaultVhost__", App: "rtp", Stream: testDeviceID + "_" + testChannelID,
		MediaGeneration: 9, Protocol: "https-flv",
	}
}

func setOpenAPICleanupCompletedEpoch(t *testing.T, db *gorm.DB, value any) {
	t.Helper()
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", testDeviceID).Update("cleanup_completed_epoch", value).Error)
}
