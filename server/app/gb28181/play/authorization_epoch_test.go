package play

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

// Explicit fixture authority; production uses the durable SQL authority.
type testPlayEpochAuthority struct{ epoch atomic.Int64 }

func newTestPlayEpochAuthority() *testPlayEpochAuthority {
	a := &testPlayEpochAuthority{}
	a.epoch.Store(1)
	return a
}

func (a *testPlayEpochAuthority) Load(ctx context.Context, _ string) (playauth.DeviceSecurityState, error) {
	if err := ctx.Err(); err != nil {
		return playauth.DeviceSecurityState{}, err
	}
	return playauth.DeviceSecurityState{AccessEpoch: a.epoch.Load()}, nil
}

func (a *testPlayEpochAuthority) AuthorizeEpoch(ctx context.Context, _ string, epoch int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if epoch <= 0 || epoch != a.epoch.Load() {
		return playauth.ErrTokenRevoked
	}
	return nil
}

func (*testPlayEpochAuthority) AuthorizeLegacy(ctx context.Context, _ string, _ int64) error {
	return ctx.Err()
}

func testPlayAuthorization(signer *playauth.Signer) *playauth.AuthorizationService {
	return playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(),
		playauth.WithDeviceSecurityAuthority(newTestPlayEpochAuthority()))
}

func TestOpenAPIPlayIssuerRejectsRawSigner(t *testing.T) {
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	_, contextIssuer := any(signer).(playauth.ContextDirectIssuer)
	require.False(t, contextIssuer)
	_, preparedIssuer := any(signer).(preparedTokenIssuer)
	require.False(t, preparedIssuer)
}

func TestOpenAPIPlayKeepsEpochSnapshotAcrossMediaWork(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	service, notifier, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
	authority := newTestPlayEpochAuthority()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	service.tokenIssuer = playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(), playauth.WithDeviceSecurityAuthority(authority))
	snapshot := &fakeSnapshotSvc{}
	service.snapshotSvc = snapshot
	inv.onInvite = func(session *uac.Session) {
		authority.epoch.Store(2)
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	result, err := service.StartAuthorized(context.Background(), AuthorizedRequest{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
	})
	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.Nil(t, result)
	require.EqualValues(t, 1, inv.inviteCalls.Load())
	require.Zero(t, snapshot.called.Load(), "internal snapshot must not upgrade the old device epoch")
}

func TestOpenAPIPlayRejectsStaleOrMissingSnapshotBeforeMediaWork(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, true, false)
	for _, epoch := range []int64{0, -1, 2} {
		z := &mockZLM{port: 40000}
		inv := &mockInviter{}
		service, _, _ := newFixedSvc(t, z, inv, onlineDevice(), aChannel())
		result, err := service.StartAuthorized(context.Background(), AuthorizedRequest{
			DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: epoch,
		})
		require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
		require.Nil(t, result)
		require.Zero(t, z.openCalls.Load())
		require.Zero(t, inv.inviteCalls.Load())
	}
}
