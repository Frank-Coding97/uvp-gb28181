package playauth

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Each invocation owns a real OS process. Exit deliberately bypasses database
// close and owner cleanup; the next process never borrows its dispatch permit.
func TestDeviceSIPCleanupProcessHelper(t *testing.T) {
	mode := os.Getenv("UVP_SIP_CLEANUP_PROCESS_MODE")
	if mode == "" {
		return
	}
	path := os.Getenv("UVP_SIP_CLEANUP_PROCESS_DB")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	s := NewDeviceOperationIntentStore(db)
	ctx, id, identity := context.Background(), intentIdentity(1), sipCleanupIdentity(1)
	if mode == "recover" {
		old, err := s.LoadSIPInviteSteps(ctx, id)
		require.NoError(t, err)
		version := old.Intent.RowVersion
		for _, fn := range []func(context.Context, DeviceOperationIntentIdentity, int64, string) (DeviceSIPInviteSteps, error){
			s.DispatchSIPCleanupACK, s.DispatchSIPCleanupBYE, s.ObserveSIPCleanupQuiesced,
		} {
			_, err = fn(ctx, id, version, identity.AttemptID)
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		}
		next, ticket, err := s.PrepareSIPBranchCleanupWork(ctx, id, version, sipCleanupIdentity(2))
		require.NoError(t, err)
		a := next.Steps[0].KnownBranch.CleanupAttempts
		require.Len(t, a, 2)
		require.Equal(t, old.Steps[0].KnownBranch.CleanupAttempts[0], a[0])
		require.NotEqual(t, a[0].OwnerRunID, a[1].OwnerRunID)
		require.Nil(t, a[0].LocalQuiescedAt)
		require.Equal(t, a[0].Identity.BYE.Request.CSeq+1, a[1].Identity.BYE.Request.CSeq)
		require.Equal(t, SIPCleanupPrepared, a[1].State)
		barrier := NewDeviceOperationBarrier(NewDeviceSecurityStore(db))
		lease, err := barrier.BeginSIPCleanup(ctx, ticket)
		require.NoError(t, err, "real new process may own a new compensation attempt")
		require.Equal(t, id.DeviceEpoch, lease.OperationEpoch())
		_, err = barrier.BeginEpoch(ctx, id.DeviceCode, 2)
		require.Error(t, err, "cleanup registration must not clear fresh business gate")
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		require.ErrorIs(t, barrier.WaitBefore(waitCtx, uint(id.DevicePK), 2), context.DeadlineExceeded)
		cancel()
		lease.Release()
		require.NoError(t, barrier.WaitBefore(ctx, uint(id.DevicePK), 2))
		os.Exit(0)
	}
	_, err = s.PrepareSIPBranchCleanup(ctx, id, 5, identity)
	require.NoError(t, err)
	if mode == "ack" || mode == "bye" {
		_, err = s.DispatchSIPCleanupACK(ctx, id, 6, identity.AttemptID)
		require.NoError(t, err)
	}
	if mode == "bye" {
		_, err = s.DispatchSIPCleanupBYE(ctx, id, 7, identity.AttemptID)
		require.NoError(t, err)
	}
	require.Contains(t, []string{"prepared", "ack", "bye"}, mode)
	os.Exit(0)
}

func TestDeviceSIPCleanupActualProcessRestart(t *testing.T) {
	for _, stage := range []string{"prepared", "ack", "bye"} {
		t.Run(stage, func(t *testing.T) {
			f, _, _ := sipCleanupFixture(t)
			require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
			require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
			path := filepath.Join(t.TempDir(), "cleanup.db")
			// Copy only this isolated fixture, before any cleanup owner exists.
			require.NoError(t, f.db.Exec("VACUUM INTO ?", path).Error)
			binary, err := os.Executable()
			require.NoError(t, err)
			for _, mode := range []string{stage, "recover"} {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDeviceSIPCleanupProcessHelper$", "-test.count=1")
				cmd.Env = append(os.Environ(), "UVP_SIP_CLEANUP_PROCESS_MODE="+mode, "UVP_SIP_CLEANUP_PROCESS_DB="+path)
				out, err := cmd.CombinedOutput()
				cancel()
				require.NoError(t, err, "isolated %s: %s", mode, out)
			}
		})
	}
}

func TestDeviceSIPCleanupLateSuccessStopsSuccessorOnly(t *testing.T) {
	_, s, id := sipCleanupFixture(t)
	ctx, i := context.Background(), sipCleanupIdentity(1)
	_, err := s.PrepareSIPBranchCleanup(ctx, id, 5, i)
	require.NoError(t, err)
	_, err = s.DispatchSIPCleanupACK(ctx, id, 6, i.AttemptID)
	require.NoError(t, err)
	_, err = s.DispatchSIPCleanupBYE(ctx, id, 7, i.AttemptID)
	require.NoError(t, err)
	_, err = s.ObserveSIPCleanupQuiesced(ctx, id, 8, i.AttemptID)
	require.NoError(t, err)
	_, err = s.PrepareSIPBranchCleanup(ctx, id, 9, sipCleanupIdentity(2))
	require.NoError(t, err)
	response := DeviceSIPCleanupBYEResponse{i.AttemptID, i.BYE.Request.CallID, i.BYE.Request.CSeq, i.BYE.Request.LocalTag, i.BYE.RemoteTag, 200}
	for _, mutate := range []func(*DeviceSIPCleanupBYEResponse){
		func(r *DeviceSIPCleanupBYEResponse) { r.CSeq++ },
		func(r *DeviceSIPCleanupBYEResponse) { r.RemoteTag += "other" },
		func(r *DeviceSIPCleanupBYEResponse) { r.StatusCode = 481 },
	} {
		bad := response
		mutate(&bad)
		_, err = s.ObserveSIPCleanupBYE(ctx, id, 10, bad)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	final, err := s.ObserveSIPCleanupBYE(ctx, id, 10, response)
	require.NoError(t, err)
	duplicate, err := s.ObserveSIPCleanupBYE(ctx, id, 11, response)
	require.NoError(t, err)
	require.Equal(t, final, duplicate)
	_, err = s.DispatchSIPCleanupACK(ctx, id, 11, sipCleanupIdentity(2).AttemptID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	loaded, err := s.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, final, loaded)
	require.Equal(t, IntentDispatched, loaded.Intent.State)
}

func TestDeviceSIPCleanupCannotReopenOriginalACK(t *testing.T) {
	f, s, id := sipCleanupFixture(t)
	ctx := context.Background()
	prepared, err := s.PrepareSIPBranchCleanup(ctx, id, 5, sipCleanupIdentity(1))
	require.NoError(t, err)
	_, err = s.DispatchSIPKnownBranchACK(ctx, id, 6, prepared.Steps[0].KnownBranch.Identity)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	// A damaged row that claims a later original ACK must also fail on load.
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
	var w sipInviteStepsWire
	require.NoError(t, json.Unmarshal([]byte(raw), &w))
	b := w.Steps[0].KnownBranch
	later := b.CleanupAttempts[0].PreparedAt.Add(time.Microsecond)
	b.ACKState, b.ACKRowVersion, b.ACKDispatchStartedAt = SIPStepMayHaveDispatched, 2, &later
	encoded, err := json.Marshal(w)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?,updated_at=?", string(encoded), later).Error)
	_, err = s.LoadSIPInviteSteps(ctx, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

func TestDeviceSIPCleanupExactRouteProfile(t *testing.T) {
	for _, tc := range []struct {
		name, target, requestURI, destination string
		routes                                []string
	}{
		{"direct-default-port", "sip:device@127.0.0.1", "sip:device@127.0.0.1", "127.0.0.1:5060", []string{}},
		{"direct-ipv6", "sip:device@[::1]:5062", "sip:device@[::1]:5062", "[::1]:5062", []string{}},
		{"loose-reversed", "sip:device@127.0.0.1:5062", "sip:device@127.0.0.1:5062", "proxy-two.example:5070", []string{"sip:proxy-one.example;lr", "sip:proxy-two.example:5070;lr"}},
		{"current-strict-builder", "sip:device@127.0.0.1:5062", "sip:proxy-two.example:5070", "proxy-two.example:5070", []string{"sip:proxy-one.example;lr", "sip:proxy-two.example:5070"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s, id := dispatchedSIPBranchFixture(t)
			b := sipKnownBranch()
			b.RemoteTarget, b.RouteSet = tc.target, tc.routes
			ctx := context.Background()
			_, err := s.ObserveSIPKnownBranch(ctx, id, 4, b)
			require.NoError(t, err)
			i := sipCleanupIdentity(1)
			for _, request := range []*DeviceSIPCleanupRequestIdentity{&i.ACK, &i.BYE} {
				request.Request.RequestURI, request.Request.Destination = tc.requestURI, tc.destination
				request.Routes = slices.Clone(tc.routes)
				slices.Reverse(request.Routes)
			}
			if len(i.ACK.Routes) > 1 {
				bad := cloneSIPCleanupIdentity(i)
				slices.Reverse(bad.ACK.Routes)
				_, err = s.PrepareSIPBranchCleanup(ctx, id, 5, bad)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
			out, err := s.PrepareSIPBranchCleanup(ctx, id, 5, i)
			require.NoError(t, err)
			loaded, err := s.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, out, loaded)
		})
	}
}

func TestDeviceSIPCleanupByteLimitPreservesPreviousRecords(t *testing.T) {
	f, s, id := dispatchedSIPBranchFixture(t)
	b := sipKnownBranch()
	b.RouteSet = make([]string, 8)
	for n := range b.RouteSet {
		b.RouteSet[n] = "sip:" + strings.Repeat("x", 200) + "@proxy.example:5060;lr"
	}
	ctx := context.Background()
	_, err := s.ObserveSIPKnownBranch(ctx, id, 4, b)
	require.NoError(t, err)
	version := int64(5)
	for n := 1; n <= maxSIPCleanupAttempts; n++ {
		i := sipCleanupIdentity(n)
		i.ACK.Routes, i.BYE.Routes = slices.Clone(b.RouteSet), slices.Clone(b.RouteSet)
		i.ACK.Request.Destination, i.BYE.Request.Destination = "proxy.example:5060", "proxy.example:5060"
		var before string
		require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&before).Error)
		_, err = s.PrepareSIPBranchCleanup(ctx, id, version, i)
		if err != nil {
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Greater(t, n, 1)
			var after string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&after).Error)
			require.Equal(t, before, after)
			loaded, err := s.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, version, loaded.Intent.RowVersion)
			require.Len(t, loaded.Steps[0].KnownBranch.CleanupAttempts, n-1)
			return
		}
		version++
		_, err = s.ObserveSIPCleanupQuiesced(ctx, id, version, i.AttemptID)
		require.NoError(t, err)
		version++
	}
	t.Fatal("fixture must reach the byte limit before the attempt count limit")
}
