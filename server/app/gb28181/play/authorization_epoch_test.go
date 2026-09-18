package play

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

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

func TestOpenAPIAutoStartRechecksQueuedAuthorizationBeforeMedia(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	for _, scenario := range []string{"transferred", "wrong-epoch", "missing-epoch", "wrong-device", "wrong-channel", "wrong-node", "changed-node-identity", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			z := &mockZLM{openErr: errors.New("media must not be reached")}
			inviter := &mockInviter{}
			service, _, _, picker, _ := newFixedAuthorizationService(t, true, z, inviter)
			authority := newTestPlayEpochAuthority()
			signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
			require.NoError(t, err)
			authorization := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(), playauth.WithDeviceSecurityAuthority(authority))
			service.tokenIssuer = authorization
			preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), AuthorizedRequest{
				DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
			})
			require.NoError(t, err)
			claims, err := authorization.VerifyForAutoStart(tokenFromFixedAuthorization(t, preauthorized), playauth.Binding{
				DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
				App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
			})
			require.NoError(t, err)
			req := Request{DeviceID: claims.DeviceID, ChannelID: claims.ChannelID, DeviceEpoch: claims.DeviceEpoch,
				AuthorizationID: claims.AuthorizationGeneration, RequiredNode: 1, Trigger: "on_stream_not_found"}
			service.channels.(*fakeChannels).c.StreamID = preauthorized.StreamID
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "transferred":
				authority.epoch.Store(2)
			case "wrong-epoch":
				req.DeviceEpoch = 2
			case "missing-epoch":
				req.DeviceEpoch = 0
			case "wrong-device":
				req.DeviceID = "34020000001110000999"
			case "wrong-channel":
				req.ChannelID = "34020000001320000999"
			case "wrong-node":
				req.RequiredNode = 99
			case "changed-node-identity":
				mediaNode, ok := service.registry.Get(req.RequiredNode)
				require.True(t, ok)
				mediaNode.MediaServerUUID = "node-b"
			case "canceled":
				cancel()
			}
			result, err := service.EnsureLive(ctx, req)
			require.Error(t, err)
			require.Nil(t, result)
			require.Zero(t, z.openCalls.Load(), "an invalid queued authorization must not open RTP")
			require.Zero(t, z.onlineCalls.Load(), "preflight must precede reuse probes")
			require.Zero(t, z.closeCalls.Load(), "preflight must precede stale cleanup")
			require.Zero(t, inviter.inviteCalls.Load())
			require.EqualValues(t, 1, picker.calls.Load(), "only the original preauthorization may select a node")
		})
	}
}

func TestOpenAPIAutoStartRejectsRevokedCallerWithoutStoppingSharedMedia(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, _, _, _, notifier := newFixedAuthorizationService(t, true, z, inviter)
	authority := newTestPlayEpochAuthority()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	authorization := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(), playauth.WithDeviceSecurityAuthority(authority))
	service.tokenIssuer = authorization
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), AuthorizedRequest{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
	})
	require.NoError(t, err)
	claims, err := authorization.VerifyForAutoStart(tokenFromFixedAuthorization(t, preauthorized), playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
	})
	require.NoError(t, err)
	req := Request{DeviceID: claims.DeviceID, ChannelID: claims.ChannelID, DeviceEpoch: claims.DeviceEpoch,
		AuthorizationID: claims.AuthorizationGeneration, RequiredNode: 1, Trigger: "on_stream_not_found"}
	result, err := service.EnsureLive(context.Background(), req)
	require.NoError(t, err)
	authority.epoch.Store(2)
	denied, err := service.EnsureLive(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, denied)
	current, ok := service.CurrentLiveRef(result.StreamID)
	require.True(t, ok, "a caller denial is not a device cleanup operation")
	require.Equal(t, resultLiveRef(result), current)
	require.Zero(t, z.closeCalls.Load())
	require.EqualValues(t, 1, inviter.inviteCalls.Load())
	require.NoError(t, service.Stop(context.Background(), result.StreamID, "", ""))
}

func TestOpenAPIAutoStartPostBindFailureDoesNotStopSharedGeneration(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, _, _, _, notifier := newFixedAuthorizationService(t, true, z, inviter)
	authority := newTestPlayEpochAuthority()
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	authorization := playauth.NewAuthorizationService(signer, playauth.NewAuthorizationRegistry(), playauth.WithDeviceSecurityAuthority(authority))
	service.tokenIssuer = authorization
	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), AuthorizedRequest{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
	})
	require.NoError(t, err)
	claims, err := authorization.VerifyForAutoStart(tokenFromFixedAuthorization(t, preauthorized), playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
	})
	require.NoError(t, err)
	inviter.onInvite = func(session *uac.Session) {
		authority.epoch.Store(2)
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	result, err := service.EnsureLive(context.Background(), Request{
		DeviceID: claims.DeviceID, ChannelID: claims.ChannelID, DeviceEpoch: claims.DeviceEpoch,
		AuthorizationID: claims.AuthorizationGeneration, RequiredNode: 1, Trigger: "on_stream_not_found",
	})
	require.Error(t, err)
	require.Nil(t, result)
	current, ok := service.CurrentLiveRef(preauthorized.StreamID)
	require.True(t, ok, "late caller rejection must not impersonate device-wide cleanup")
	require.NotZero(t, current.Generation)
	require.Zero(t, z.closeCalls.Load())
	require.EqualValues(t, 1, inviter.inviteCalls.Load())
	require.NoError(t, service.Stop(context.Background(), preauthorized.StreamID, "", ""))
}

type blockedCallerIssuer struct {
	*playauth.AuthorizationService
	entered chan struct{}
	release chan struct{}
}

func (i *blockedCallerIssuer) BindContext(ctx context.Context, prepared playauth.Prepared, binding playauth.Binding) (playauth.Grant, error) {
	if binding.ClientIP == "caller-denied" {
		close(i.entered)
		select {
		case <-i.release:
			return playauth.Grant{}, playauth.ErrTokenRevoked
		case <-ctx.Done():
			return playauth.Grant{}, ctx.Err()
		}
	}
	return i.AuthorizationService.BindContext(ctx, prepared, binding)
}

func TestOpenAPIExplicitCallerDenialDoesNotStopSuccessfulSharedCaller(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, notifier, _ := newFixedSvc(t, z, inviter, onlineDevice(), aChannel())
	signer, err := playauth.NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	issuer := &blockedCallerIssuer{AuthorizationService: testPlayAuthorization(signer),
		entered: make(chan struct{}), release: make(chan struct{}, 1)}
	defer close(issuer.release)
	service.tokenIssuer = issuer
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ownerErrors := make(chan error, 1)
	go func() {
		_, err := service.StartAuthorized(ctx, AuthorizedRequest{
			DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1, ClientIP: "caller-denied",
		})
		ownerErrors <- err
	}()
	select {
	case <-issuer.entered:
	case <-ctx.Done():
		t.Fatal("owner did not reach its caller-specific authorization")
	}
	shared, err := service.StartAuthorized(ctx, AuthorizedRequest{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
	})
	require.NoError(t, err)
	require.True(t, shared.Reused)
	issuer.release <- struct{}{}
	select {
	case err := <-ownerErrors:
		require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	case <-ctx.Done():
		t.Fatal("owner did not return after authorization denial")
	}
	current, ok := service.CurrentLiveRef(shared.StreamID)
	require.True(t, ok, "coordinator ownership is not exclusive viewer ownership")
	require.Equal(t, resultLiveRef(shared), current)
	require.Zero(t, z.closeCalls.Load())
	require.EqualValues(t, 1, inviter.inviteCalls.Load())
	require.NoError(t, service.Stop(context.Background(), shared.StreamID, "", ""))
}

func TestOpenAPIAutoStartBindingRequiresActualResultIdentity(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	for _, scenario := range []string{"zero-generation", "wrong-app", "wrong-stream", "missing-node", "wrong-node"} {
		t.Run(scenario, func(t *testing.T) {
			service, authorization, _, _, _ := newFixedAuthorizationService(t, true, &mockZLM{}, &mockInviter{})
			preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), AuthorizedRequest{
				DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, DeviceEpoch: 1,
			})
			require.NoError(t, err)
			token := tokenFromFixedAuthorization(t, preauthorized)
			binding := playauth.Binding{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
				App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a"}
			claims, err := authorization.VerifyForAutoStart(token, binding)
			require.NoError(t, err)
			req := Request{DeviceID: claims.DeviceID, ChannelID: claims.ChannelID, DeviceEpoch: claims.DeviceEpoch,
				AuthorizationID: claims.AuthorizationGeneration, RequiredNode: 1}
			result := &Result{App: binding.App, StreamID: binding.Stream, Generation: 17, Node: &ResultNode{ID: 1}}
			switch scenario {
			case "zero-generation":
				result.Generation = 0
			case "wrong-app":
				result.App = "other"
			case "wrong-stream":
				result.StreamID = "other"
			case "missing-node":
				result.Node = nil
			case "wrong-node":
				result.Node.ID = 2
			}
			require.ErrorIs(t, service.bindResultAuthorization(context.Background(), req, result), ErrPlayAuthorizationUnavailable)
			_, err = authorization.VerifyForAutoStart(token, binding)
			require.NoError(t, err, "a mismatched media result must not bind or terminate the caller's authorization")
		})
	}
}

func TestOpenAPIAutoStartDoesNotDriveQuarantinedGenerationCleanup(t *testing.T) {
	var starts, stops atomic.Int32
	coordinator := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) {
			starts.Add(1)
			return &Result{Generation: 2}, nil
		},
		func(context.Context, *Result) error {
			stops.Add(1)
			return nil
		},
	)
	req := Request{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		RequiredNode: 1, AuthorizationID: "queued", DeviceEpoch: 1}
	coordinator.entries[coordinatorKey{deviceID: req.DeviceID, channelID: req.ChannelID}] = &coordinatorEntry{
		state: LiveStateCleanupPending, ownerNode: 1,
		result: &Result{Generation: 1, Node: &ResultNode{ID: 1}},
	}
	_, err := coordinator.EnsureLive(context.Background(), req)
	require.ErrorIs(t, err, ErrLiveCleanupPending)
	require.Zero(t, starts.Load())
	require.Zero(t, stops.Load(), "queued authorization cannot authorize a destructive cleanup retry")
}
