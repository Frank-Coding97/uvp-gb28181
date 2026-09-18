package uac

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackIntentInventoryPersistsForkAndFaultBeforeACK(t *testing.T) {
	for _, kind := range []string{"fork", "conflict", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f := newPlaybackOperationUDPFixture(t)
			f.respond(t, 200)
			require.Eventually(t, func() bool {
				f.op.factMu.Lock()
				defer f.op.factMu.Unlock()
				return f.op.first != nil
			}, time.Second, time.Millisecond)
			f.op.factMu.Lock()
			response := f.op.first.Clone()
			f.op.factMu.Unlock()
			switch kind {
			case "fork":
				response.To().Params.Add("tag", "extra-before-ACK")
			case "conflict":
				response.Contact().Address.User = "changed"
			case "invalid":
				response.RemoveHeader("Contact")
			}
			_, err := f.peer.WriteTo([]byte(response.String()), f.address)
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				snapshot := f.op.owned.ObservedBranches()
				return snapshot.Incomplete || len(snapshot.Responses) == 2
			}, time.Second, time.Millisecond)
			_, err = f.op.ReadAndAccept(context.Background())
			require.Error(t, err, "cannot publish a business dialog with a fork or incomplete observer")
			err = f.op.CloseLocal(context.Background())
			require.ErrorIs(t, err, ErrPlaybackCleanupUnknown)
			loaded, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
			require.NoError(t, err)
			b := loaded.Steps[0].KnownBranch
			require.NotNil(t, b, "retain selected material even when multi-branch cleanup is incomplete")
			require.Equal(t, "owned-device", b.Identity.RemoteTag)
			require.Equal(t, playauth.SIPStepPrepared, b.ACKState)
			if kind == "fork" {
				require.Len(t, loaded.Steps[0].AdditionalBranches, 1)
				require.Equal(t, "extra-before-ACK", loaded.Steps[0].AdditionalBranches[0].Identity.RemoteTag)
			} else {
				require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
			}
			require.False(t, f.op.originalReleased)
			f.noACK(t)
			// Repeat only reconciles exact facts, never network or new authority.
			require.ErrorIs(t, f.op.CloseLocal(context.Background()), ErrPlaybackCleanupUnknown)
			again, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
			require.NoError(t, err)
			require.Equal(t, loaded, again)
		})
	}
}

func TestPlaybackIntentInventoryBurstRetainedWithoutBusinessConsumer(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	f.respond(t, 200)
	require.Eventually(t, func() bool {
		f.op.factMu.Lock()
		defer f.op.factMu.Unlock()
		return f.op.first != nil
	}, time.Second, time.Millisecond)
	for _, tag := range []string{"extra-A", "extra-B", "extra-C"} {
		r := sip.NewResponseFromRequest(f.invite, 200, "fixture", nil)
		r.To().Params.Add("tag", tag)
		r.AppendHeader(f.invite.Contact().Clone())
		_, err := f.peer.WriteTo([]byte(r.String()), f.address)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return len(f.op.owned.ObservedBranches().Responses) == 4 }, time.Second, time.Millisecond)
	require.ErrorIs(t, f.op.CloseLocal(context.Background()), ErrPlaybackCleanupUnknown)
	out, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
	require.NoError(t, err)
	require.Len(t, out.Steps[0].AdditionalBranches, 3)
	f.noACK(t)
}

func TestPlaybackIntentInventoryInvalidFirstResponsePersistsFault(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := newPlaybackOperationUDPFixture(t)
	r := sip.NewResponseFromRequest(f.invite, 200, "fixture", nil)
	r.To().Params.Add("tag", "invalid-no-contact")
	_, err := f.peer.WriteTo([]byte(r.String()), f.address)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.op.owned.ObservedBranches().Incomplete }, time.Second, time.Millisecond)
	require.ErrorIs(t, f.op.CloseLocal(context.Background()), ErrPlaybackCleanupUnknown)
	out, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
	require.NoError(t, err)
	require.Nil(t, out.Steps[0].KnownBranch)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, out.Steps[0].BranchInventoryFault)
	require.False(t, f.op.originalReleased)
	f.noACK(t)
}
