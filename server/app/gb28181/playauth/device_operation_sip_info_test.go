package playauth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/mansrtsp"
)

func sipINFOIdentity(t *testing.T, n int, command DeviceSIPINFOCommand) DeviceSIPINFOIdentity {
	t.Helper()
	r := sipCleanupIdentity(n).BYE
	r.Request.Branch = fmt.Sprintf("z9hG4bK-info-%d", n)
	body, err := command.Body(r.Request.CSeq)
	require.NoError(t, err)
	r.Request.ContentType, r.Request.BodyLength, r.Request.BodySHA256 = mansrtsp.ContentType, len(body), fmt.Sprintf("%x", sha256.Sum256(body))
	return DeviceSIPINFOIdentity{InfoID: fmt.Sprintf("%032x", n+200), Request: r, Command: command}
}

func sipINFOFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity) {
	t.Helper()
	f, store, id := sipCleanupFixture(t)
	// The shared SIP fixture starts as a live intent. INFO is playback-only.
	id.Kind = "playback"
	require.NoError(t, f.db.Model(&DeviceOperationIntent{}).Where("operation_id = ?", id.OperationID).Update("kind", id.Kind).Error)
	branch := sipKnownBranch()
	branch.RouteSet = []string{}
	_, err := store.DispatchSIPKnownBranchACK(context.Background(), id, 5, branch)
	require.NoError(t, err)
	return f, store, id
}

func sipINFOResponse(i DeviceSIPINFOIdentity) DeviceSIPINFOResponse {
	return DeviceSIPINFOResponse{InfoID: i.InfoID, CallID: i.Request.Request.CallID, CSeq: i.Request.Request.CSeq,
		LocalTag: i.Request.Request.LocalTag, RemoteTag: i.Request.RemoteTag, SIPStatus: 200, BodyClass: "empty"}
}

func TestDeviceSIPINFOStagesAndSharedCleanupCSeq(t *testing.T) {
	f, store, id := sipINFOFixture(t)
	ctx := context.Background()
	i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
	out, err := store.PrepareSIPINFO(ctx, id, 6, i)
	require.NoError(t, err)
	require.Equal(t, SIPStepPrepared, out.Steps[0].KnownBranch.InfoSteps[0].State)
	duplicate, err := store.PrepareSIPINFO(ctx, id, 7, i)
	require.NoError(t, err)
	require.EqualValues(t, 7, duplicate.Intent.RowVersion, "duplicate is observation, not dispatch permission")
	_, err = store.DispatchSIPINFO(ctx, id, 7, i.InfoID)
	require.NoError(t, err)
	_, err = store.DispatchSIPINFO(ctx, id, 8, i.InfoID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	_, err = store.ObserveSIPINFOResponse(ctx, id, 8, sipINFOResponse(i))
	require.NoError(t, err)
	_, err = store.PrepareSIPINFO(ctx, id, 9, sipINFOIdentity(t, 2, DeviceSIPINFOCommand{Action: "resume"}))
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "response is not local transaction quiescence")
	_, err = store.PrepareSIPBranchCleanup(ctx, id, 9, sipCleanupIdentity(2))
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "BYE cannot overtake local INFO")
	out, err = store.ObserveSIPINFOQuiesced(ctx, id, 9, i.InfoID)
	require.NoError(t, err)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out, loaded)
	_, err = store.PrepareSIPBranchCleanup(ctx, id, 10, sipCleanupIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "BYE cannot reuse the INFO sequence")
	_, err = store.PrepareSIPBranchCleanup(ctx, id, 10, sipCleanupIdentity(2))
	require.NoError(t, err)
	_, err = store.PrepareSIPINFO(ctx, id, 11, sipINFOIdentity(t, 3, DeviceSIPINFOCommand{Action: "teardown"}))
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "cleanup permanently closes business INFO admission")
}

func TestDeviceSIPINFOPreparedBurnsCSeqAndUnknownBlocksBusiness(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(fmt.Sprint(dispatched), func(t *testing.T) {
			_, store, id := sipINFOFixture(t)
			ctx, version := context.Background(), int64(6)
			i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "play"})
			_, err := store.PrepareSIPINFO(ctx, id, version, i)
			require.NoError(t, err)
			version++
			if dispatched {
				_, err = store.DispatchSIPINFO(ctx, id, version, i.InfoID)
				require.NoError(t, err)
				version++
			}
			_, err = store.ObserveSIPINFOQuiesced(ctx, id, version, i.InfoID)
			require.NoError(t, err)
			version++
			_, err = store.PrepareSIPINFO(ctx, id, version, sipINFOIdentity(t, 2, DeviceSIPINFOCommand{Action: "pause"}))
			if dispatched {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
				_, err = store.PrepareSIPBranchCleanup(ctx, id, version, sipCleanupIdentity(2))
				require.NoError(t, err, "unknown remote control outcome does not prevent owned BYE cleanup")
			} else {
				require.NoError(t, err, "confirmed never-dispatched work burns its sequence, but permits the next one")
			}
		})
	}
}

func TestDeviceSIPINFOFreshEpochAndCanonicalCommands(t *testing.T) {
	commands := []DeviceSIPINFOCommand{{Action: "play"}, {Action: "pause"}, {Action: "resume"},
		{Action: "seek", PositionNanos: int64(time.Second), SegmentDurationNanos: int64(time.Minute)}, {Action: "scale", Scale: 2}, {Action: "teardown"}}
	for _, command := range commands {
		t.Run(command.Action, func(t *testing.T) {
			f, store, id := sipINFOFixture(t)
			ctx, i := context.Background(), sipINFOIdentity(t, 1, command)
			_, err := store.PrepareSIPINFO(ctx, id, 6, i)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			_, err = store.DispatchSIPINFO(ctx, id, 7, i.InfoID)
			require.Error(t, err, "even INFO TEARDOWN requires current business authority")
			_, err = store.ObserveSIPINFOQuiesced(ctx, id, 7, i.InfoID)
			require.NoError(t, err, "old epoch may still record local exit")
			_, err = store.PrepareSIPINFO(ctx, id, 8, sipINFOIdentity(t, 2, command))
			require.Error(t, err)
			_, err = store.PrepareSIPBranchCleanup(ctx, id, 8, sipCleanupIdentity(2))
			require.NoError(t, err)
		})
	}
	for _, command := range []DeviceSIPINFOCommand{{Action: "unknown"}, {Action: "pause", Scale: 1},
		{Action: "play", PositionNanos: 1}, {Action: "seek"}, {Action: "scale", Scale: 3}} {
		_, err := command.Body(2)
		require.Error(t, err)
	}
}

func TestDeviceSIPINFOAndCleanupPrepareHaveOneCASWinner(t *testing.T) {
	_, store, id := sipINFOFixture(t)
	ctx, i := context.Background(), sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
	var wg sync.WaitGroup
	var wins atomic.Int32
	for _, prepare := range []func() error{
		func() error { _, err := store.PrepareSIPINFO(ctx, id, 6, i); return err },
		func() error { _, err := store.PrepareSIPBranchCleanup(ctx, id, 6, sipCleanupIdentity(1)); return err },
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if prepare() == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
	out, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	b := out.Steps[0].KnownBranch
	require.Equal(t, 1, len(b.InfoSteps)+len(b.CleanupAttempts))
}

func TestDeviceSIPINFOCannotAttachPlaybackControlToOtherIntentKinds(t *testing.T) {
	for _, kind := range []string{"live", "download", "talk", "ptz"} {
		t.Run(kind, func(t *testing.T) {
			f, store, id := sipINFOFixture(t)
			id.Kind = kind
			require.NoError(t, f.db.Model(&DeviceOperationIntent{}).Where("operation_id = ?", id.OperationID).Update("kind", kind).Error)
			_, err := store.PrepareSIPINFO(context.Background(), id, 6, sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"}))
			require.Error(t, err)
		})
	}
}
