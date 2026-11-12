package play

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbplay "uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/models"
)

const (
	stopDeviceCode  = "37010301021320000018"
	stopChannelCode = "37010301021320000006"
)

type stopLiveService struct {
	stopped []string
	err     error
}

func (s *stopLiveService) StartAuthorized(context.Context, gbplay.AuthorizedRequest) (*gbplay.Result, error) {
	return nil, nil
}

func (s *stopLiveService) Stop(_ context.Context, streamID, _, _ string) error {
	s.stopped = append(s.stopped, streamID)
	return s.err
}

// The platform stop primitive takes any stream id, so the grant ledger is the
// only thing that can tell one client's stream from another's.
func TestActiveLiveGrantHidesAnotherClientsChannelGrant(t *testing.T) {
	db := quotaDB(t, 2)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	ctx := context.Background()
	device, channel := stopDeviceCode, stopChannelCode
	require.NoError(t, db.Create(&models.PlayGrant{
		GrantID: "11111111-1111-4111-8111-111111111111", ClientID: 2, Scope: auth.PlayLiveScope,
		DeviceID: &device, ChannelID: &channel, IssuedAt: quotaNow, ExpiresAt: quotaNow.Add(time.Minute),
		State: models.GrantStatePending, CreatedAt: quotaNow, UpdatedAt: quotaNow,
	}).Error)

	_, found, err := quota.ActiveLiveGrant(ctx, 1, device, channel)
	require.NoError(t, err)
	require.False(t, found, "another client's grant must never authorize a stop")

	// Client 1 builds its own grant without the quota being affected.
	require.NoError(t, db.Create(&models.PlayGrant{
		GrantID: "22222222-2222-4222-8222-222222222222", ClientID: 1, Scope: auth.PlayLiveScope,
		DeviceID: &device, ChannelID: &channel, IssuedAt: quotaNow, ExpiresAt: quotaNow.Add(time.Minute),
		State: models.GrantStatePending, CreatedAt: quotaNow, UpdatedAt: quotaNow,
	}).Error)
	grant, found, err := quota.ActiveLiveGrant(ctx, 1, device, channel)
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, grant.Stream, "a reservation that never reached media has no stream yet")

	_, found, err = quota.ActiveLiveGrant(ctx, 1, device, "37010301021320000007")
	require.NoError(t, err)
	require.False(t, found, "a different channel is a different grant")
}

func TestReleaseRetiresTheLedgerOnceAndOnlyForItsOwner(t *testing.T) {
	db := quotaDB(t, 2)
	quota := NewViewerQuota(db, func() time.Time { return quotaNow })
	ctx := context.Background()
	const grantID = "33333333-3333-4333-8333-333333333333"
	device, channel := stopDeviceCode, stopChannelCode
	require.NoError(t, db.Create(&models.PlayGrant{
		GrantID: grantID, ClientID: 1, Scope: auth.PlayLiveScope, DeviceID: &device, ChannelID: &channel,
		IssuedAt: quotaNow, ExpiresAt: quotaNow.Add(time.Minute),
		State: models.GrantStatePending, CreatedAt: quotaNow, UpdatedAt: quotaNow,
	}).Error)

	// A foreign client never gets past the ledger lock, so the exact
	// classification depends on which check rejects it first (unknown client
	// versus grant owner). Both are refusals, which is what matters here.
	require.Error(t, quota.Release(ctx, 2, grantID), "a foreign client must not release the grant")

	require.NoError(t, quota.Release(ctx, 1, grantID))
	var revoked models.PlayGrant
	require.NoError(t, db.Where("grant_id = ?", grantID).Take(&revoked).Error)
	require.Equal(t, models.GrantStateRevoked, revoked.State)
	require.Equal(t, "client_stop", revoked.Reason)

	// Repeated stops are harmless: the client may retry a request whose reply
	// was lost, and the ledger has nothing left to retire.
	require.NoError(t, quota.Release(ctx, 1, grantID))
	_, found, err := quota.ActiveLiveGrant(ctx, 1, device, channel)
	require.NoError(t, err)
	require.False(t, found, "a released grant no longer authorizes anything")
}

func TestStopRefusesAChannelThisClientNeverOpened(t *testing.T) {
	db := quotaDB(t, 2)
	service := &stopLiveService{}
	dispatcher := NewDispatcher(db, service)
	_, err := dispatcher.Stop(context.Background(), auth.PlayRequest{
		ClientID: 1, OwnerDeptID: 1, DataScope: models.DataScopeDepartment,
		DeviceID: stopDeviceCode, ChannelID: stopChannelCode,
	})
	require.ErrorIs(t, err, auth.ErrPlayNotFound)
	require.Empty(t, service.stopped, "no platform stop may run without ownership evidence")
}
