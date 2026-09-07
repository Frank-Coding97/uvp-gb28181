package uac

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestPlaybackIntentAdapterTransfersOnlyDurableQuiescedOwner(t *testing.T) {
	for _, failSQL := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable-unknown", true: "fault-SQL"}[failSQL], func(t *testing.T) {
			f := newPlaybackOperationUDPFixture(t)
			child := &playbackIntentChild{u: f.u, op: f.op, gate: make(chan struct{}, 1)}
			if failSQL {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER deny_child_fault BEFORE UPDATE ON gb_device_operation_intent BEGIN SELECT RAISE(ABORT,'fixture blocked final facts'); END`).Error)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			local, err := child.Close(ctx)
			require.Error(t, err, "zero 2xx cannot prove remote completion")
			f.u.playbackIntentMu.Lock()
			retained := f.u.playbackIntents[f.id.OperationID]
			f.u.playbackIntentMu.Unlock()
			if failSQL {
				require.False(t, local)
				require.Same(t, f.op, retained)
				require.NoError(t, f.db.Exec("DROP TRIGGER deny_child_fault").Error)
				local, _ = child.Close(ctx)
			}
			require.True(t, local)
			if !failSQL {
				require.Nil(t, retained)
			}
			loaded, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
			require.NoError(t, err)
			require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
			require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
		})
	}
}

func TestPlaybackIntentAdapterNeverDeletesReplacementOwner(t *testing.T) {
	f := newPlaybackOperationPreparedUDPFixture(t)
	child := &playbackIntentChild{u: f.u, op: f.op, gate: make(chan struct{}, 1)}
	replacement := &playbackIntentOperation{id: f.id}
	f.u.playbackIntentMu.Lock()
	f.u.playbackIntents[f.id.OperationID] = replacement
	f.u.playbackIntentMu.Unlock()
	local, err := child.Close(context.Background())
	require.False(t, local)
	require.ErrorIs(t, err, ErrPlaybackUnavailable)
	f.u.playbackIntentMu.Lock()
	require.Same(t, replacement, f.u.playbackIntents[f.id.OperationID])
	// Only restore the actual fixture owner; no production registry is touched.
	f.u.playbackIntents[f.id.OperationID] = f.op
	f.u.playbackIntentMu.Unlock()
	local, err = child.Close(context.Background())
	require.True(t, local)
	require.NoError(t, err)
	_, err = f.store.DispatchSIPInviteStep(context.Background(), f.id, 3, strings.Repeat("b", 32))
	// A store write alone cannot revive the removed child or permit its Invite.
	require.NoError(t, err)
	_, err = child.Invite(context.Background())
	require.Error(t, err)
}
