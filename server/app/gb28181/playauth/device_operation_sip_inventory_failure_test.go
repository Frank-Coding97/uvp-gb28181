package playauth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceSIPInventoryConcurrentObservationsRetainEachBranch(t *testing.T) {
	_, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for tries := 0; tries < 40; tries++ {
				out, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				_, err = store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, sipExtraBranch(1+n%2))
				if err == nil {
					return
				}
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
			t.Error("CAS retries exhausted")
		}(n)
	}
	wg.Wait()
	out, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 7, out.Intent.RowVersion)
	require.Len(t, out.Steps[0].AdditionalBranches, 2)
	require.NotNil(t, sipFindBranch(&out.Steps[0], sipExtraBranch(1).RemoteTag))
	require.NotNil(t, sipFindBranch(&out.Steps[0], sipExtraBranch(2).RemoteTag))
}

func TestDeviceSIPInventoryCommitUnknownReturnsNoMaterial(t *testing.T) {
	for _, operation := range []string{"additional", "fault", "conflict"} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", operation, committed), func(t *testing.T) {
				f, store, id := sipCleanupFixture(t)
				ctx := context.Background()
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				fault := newIntentFixtureStore(faultDB)
				var out DeviceSIPInviteSteps
				var err error
				if operation == "fault" {
					out, err = fault.ObserveSIPBranchInventoryFault(ctx, id, 5, sipStepIdentity(1).StepID, SIPBranchObserverIncomplete)
				} else {
					b := sipExtraBranch(1)
					if operation == "conflict" {
						b.RemoteTag = "remote-one"
						b.StatusCode = 202
					}
					out, err = fault.ObserveSIPAdditionalBranch(ctx, id, 5, b)
				}
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out)
				loaded, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				want := int64(5)
				if committed {
					want++
				}
				require.Equal(t, want, loaded.Intent.RowVersion)
				if committed && operation == "conflict" {
					require.Equal(t, SIPBranchIdentityConflict, loaded.Steps[0].BranchInventoryFault)
				}
			})
		}
	}
}

func TestDeviceSIPInventoryByteLimitRetainsFaultAndOldFacts(t *testing.T) {
	_, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	out, err := store.PrepareSIPBranchCleanup(ctx, id, 5, sipCleanupIdentity(1))
	require.NoError(t, err)
	out, err = store.ObserveSIPCleanupQuiesced(ctx, id, 6, sipCleanupIdentity(1).AttemptID)
	require.NoError(t, err)
	selected := out.Steps[0].KnownBranch
	for n := 1; n < maxSIPObservedBranches; n++ {
		b := sipExtraBranch(n)
		route := "sip:" + strings.Repeat("u", 256) + "@" + strings.Repeat(strings.Repeat("a", 50)+".", 4) + strings.Repeat("z", 40) + ";lr"
		require.True(t, validSIPDialogURI(route, true))
		for j := 0; j < 8; j++ {
			b.RouteSet = append(b.RouteSet, route)
		}
		previous := out.Steps[0].AdditionalBranches
		out, err = store.ObserveSIPAdditionalBranch(ctx, id, out.Intent.RowVersion, b)
		require.NoError(t, err)
		if out.Steps[0].BranchInventoryFault != "" {
			require.Equal(t, SIPBranchInventoryOverflow, out.Steps[0].BranchInventoryFault)
			require.Equal(t, previous, out.Steps[0].AdditionalBranches)
			require.Equal(t, selected, out.Steps[0].KnownBranch)
			raw, err := encodeSIPInviteSteps(out)
			require.NoError(t, err)
			require.LessOrEqual(t, len(raw), maxIntentSIPBytes)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, out, loaded)
			return
		}
	}
	t.Fatal("large valid route material did not exercise byte overflow")
}

func TestDeviceSIPInventoryFaultBeforeSelectedIsNotACKAuthority(t *testing.T) {
	_, store, id := dispatchedSIPBranchFixture(t)
	ctx := context.Background()
	out, err := store.ObserveSIPBranchInventoryFault(ctx, id, 4, sipStepIdentity(1).StepID, SIPBranchObserverIncomplete)
	require.NoError(t, err)
	require.Nil(t, out.Steps[0].KnownBranch)
	out, err = store.ObserveSIPKnownBranch(ctx, id, 5, sipKnownBranch())
	require.NoError(t, err)
	_, err = store.DispatchSIPKnownBranchACK(ctx, id, 6, sipKnownBranch())
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	loaded, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
	raw, err := json.Marshal(out.Steps[0])
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(raw))
}

func TestDeviceSIPInventorySelectedINFOSequenceDoesNotLeakToExtra(t *testing.T) {
	_, store, id := sipINFOFixture(t)
	ctx := context.Background()
	i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
	out, err := store.PrepareSIPINFO(ctx, id, 6, i)
	require.NoError(t, err)
	out, err = store.ObserveSIPINFOQuiesced(ctx, id, 7, i.InfoID)
	require.NoError(t, err)
	out, err = store.ObserveSIPAdditionalBranch(ctx, id, 8, sipExtraBranch(1))
	require.NoError(t, err)
	out, err = store.PrepareSIPBranchCleanup(ctx, id, 9, sipExtraCleanup(1))
	require.NoError(t, err)
	require.Equal(t, i.Request.Request.CSeq, out.Steps[0].AdditionalBranches[0].CleanupAttempts[0].Identity.BYE.Request.CSeq)
	out, err = store.ObserveSIPCleanupQuiesced(ctx, id, 10, sipExtraCleanup(1).AttemptID)
	require.NoError(t, err)
	out, err = store.PrepareSIPBranchCleanup(ctx, id, 11, sipCleanupIdentity(2))
	require.NoError(t, err)
	require.Equal(t, i.Request.Request.CSeq+1, out.Steps[0].KnownBranch.CleanupAttempts[0].Identity.BYE.Request.CSeq)
	_, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
}

func TestDeviceSIPInventoryCleanupTicketBindsExactRemoteTagAndLatestAttempt(t *testing.T) {
	for _, fault := range []string{"remote-tag", "non-latest"} {
		t.Run(fault, func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			out, err := store.ObserveSIPAdditionalBranch(ctx, id, 5, sipExtraBranch(1))
			require.NoError(t, err)
			i := sipExtraCleanup(1)
			out, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, out.Intent.RowVersion, i)
			require.NoError(t, err)
			if fault == "remote-tag" {
				ticket.work.identity.ACK.RemoteTag = "remote-one"
				ticket.work.identity.BYE.RemoteTag = "remote-one"
			} else {
				out, err = store.ObserveSIPCleanupQuiesced(ctx, id, out.Intent.RowVersion, i.AttemptID)
				require.NoError(t, err)
				i.AttemptID = strings.Repeat("f", 32)
				i.ACK.Request.Branch += "-next"
				i.BYE.Request.Branch += "-next"
				i.BYE.Request.CSeq++
				_, err = store.PrepareSIPBranchCleanup(ctx, id, out.Intent.RowVersion, i)
				require.NoError(t, err)
			}
			lease, err := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db)).BeginSIPCleanup(ctx, ticket)
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
			require.Nil(t, lease)
		})
	}
}
