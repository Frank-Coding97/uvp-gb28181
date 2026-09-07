package playauth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newSIPStepFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity) {
	t.Helper()
	f, store, id := newRTPStepFixture(t)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN sip_steps_json TEXT NULL").Error)
	return f, store, id
}

func sipStepIdentity(n int) DeviceSIPInviteIdentity {
	return DeviceSIPInviteIdentity{StepID: fmt.Sprintf("%032x", n), CallID: fmt.Sprintf("playback-fixture-%d", n), CSeq: 123,
		RequestURI: "sip:34020000001320000001@3402000000", FromURI: "sip:34020000002000000001@3402000000", LocalTag: "from-tag",
		ToURI: "sip:34020000001320000001@3402000000", ContactURI: "sip:34020000002000000001@127.0.0.1:5060",
		Transport: "UDP", Destination: "127.0.0.1:5060", ViaHost: "127.0.0.1", ViaPort: 5060, ViaTransport: "UDP",
		Branch: fmt.Sprintf("z9hG4bK-fixture-%d", n), MaxForwards: 70, ContentType: "application/sdp", BodyLength: 100,
		BodySHA256: strings.Repeat("a", 64)}
}

func TestDeviceSIPStepsPersistentIdentityAndTransfer(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	first, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	require.EqualValues(t, 3, first.Intent.RowVersion)
	require.Equal(t, SIPStepPrepared, first.Steps[0].State)
	second, err := store.AddSIPInviteStep(ctx, id, 3, sipStepIdentity(2))
	require.NoError(t, err)
	require.Len(t, second.Steps, 2)
	dispatched, err := store.DispatchSIPInviteStep(ctx, id, 4, sipStepIdentity(1).StepID)
	require.NoError(t, err)
	require.Equal(t, SIPStepMayHaveDispatched, dispatched.Steps[0].State)
	require.Equal(t, SIPStepPrepared, dispatched.Steps[1].State)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, dispatched, loaded)
	for _, value := range []any{loaded, loaded.Steps[0], loaded.Steps[0].Identity} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body))
	}
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = store.AddSIPInviteStep(ctx, id, 5, sipStepIdentity(3))
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	_, err = store.DispatchSIPInviteStep(ctx, id, 5, sipStepIdentity(2).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	loaded, err = store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, dispatched, loaded)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
}

func TestDeviceSIPStepsCASAndSharedRTPVersion(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
	require.NoError(t, err)
	duplicate, err := store.AddSIPInviteStep(ctx, id, 3, sipStepIdentity(1))
	require.NoError(t, err)
	require.EqualValues(t, 3, duplicate.Intent.RowVersion)
	changed := sipStepIdentity(1)
	changed.CSeq++
	_, err = store.AddSIPInviteStep(ctx, id, 3, changed)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	changed = sipStepIdentity(1)
	changed.StepID = sipStepIdentity(2).StepID
	_, err = store.AddSIPInviteStep(ctx, id, 3, changed)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "one original INVITE must not gain a second step")
	_, err = store.AddRTPResourceStep(ctx, id, 3, rtpStepIdentity(1))
	require.NoError(t, err)
	_, err = store.DispatchSIPInviteStep(ctx, id, 3, sipStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "RTP and SIP share the same parent version")
	var wg sync.WaitGroup
	var winners atomic.Int32
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchSIPInviteStep(ctx, id, 4, sipStepIdentity(1).StepID)
			if err == nil {
				winners.Add(1)
			} else {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	require.EqualValues(t, 1, winners.Load())
	_, err = store.DispatchSIPInviteStep(ctx, id, 5, sipStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	rtp, err := NewDeviceOperationIntentStore(f.db).LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, rtp.Steps, 1)
	require.Equal(t, rtpStepIdentity(1), rtp.Steps[0].Identity)
}

func TestDeviceSIPStepsCommitUnknownNeverGrantsDispatch(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f, store, id := newSIPStepFixture(t)
			ctx := context.Background()
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
			fault := NewDeviceOperationIntentStore(faultDB)
			out, err := fault.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out.Intent.OperationID)
			if !committed {
				_, err = store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
				require.NoError(t, err)
			}
			out, err = fault.DispatchSIPInviteStep(ctx, id, 3, sipStepIdentity(1).StepID)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out.Intent.OperationID)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			if committed {
				require.Equal(t, SIPStepMayHaveDispatched, loaded.Steps[0].State)
				_, err = store.DispatchSIPInviteStep(ctx, id, 4, sipStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			} else {
				require.Equal(t, SIPStepPrepared, loaded.Steps[0].State)
			}
		})
	}
}

func TestDeviceSIPStepsRejectUnsafeAndDamagedMaterials(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	for _, mutate := range []func(*DeviceSIPInviteIdentity){
		func(i *DeviceSIPInviteIdentity) { i.StepID = "bad" },
		func(i *DeviceSIPInviteIdentity) { i.CallID = "raw\r\nAuthorization: secret" },
		func(i *DeviceSIPInviteIdentity) { i.LocalTag = "" },
		func(i *DeviceSIPInviteIdentity) { i.CSeq = 0 },
		func(i *DeviceSIPInviteIdentity) { i.RequestURI = "sip:user:secret@example.com" },
		func(i *DeviceSIPInviteIdentity) { i.ToURI += "?Authorization=secret" },
		func(i *DeviceSIPInviteIdentity) { i.FromURI += ";token=secret" },
		func(i *DeviceSIPInviteIdentity) { i.ContactURI = "sip:user@127.0.0.1" },
		func(i *DeviceSIPInviteIdentity) { i.Transport = "TLS" },
		func(i *DeviceSIPInviteIdentity) { i.ViaTransport = "TCP" },
		func(i *DeviceSIPInviteIdentity) { i.ViaPort = 0 },
		func(i *DeviceSIPInviteIdentity) { i.Destination = "127.0.0.1:05060" },
		func(i *DeviceSIPInviteIdentity) { i.Branch = "" },
		func(i *DeviceSIPInviteIdentity) { i.RPortValue = "5060" },
		func(i *DeviceSIPInviteIdentity) { i.MaxForwards = 256 },
		func(i *DeviceSIPInviteIdentity) { i.ContentType = "message/sip" },
		func(i *DeviceSIPInviteIdentity) { i.BodyLength = 0 },
		func(i *DeviceSIPInviteIdentity) { i.BodySHA256 = strings.Repeat("A", 64) },
	} {
		i := sipStepIdentity(1)
		mutate(&i)
		_, err := store.AddSIPInviteStep(ctx, id, 2, i)
		require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	}
	for n := 1; n <= 16; n++ {
		_, err := store.AddSIPInviteStep(ctx, id, int64(n+1), sipStepIdentity(n))
		require.NoError(t, err)
	}
	_, err := store.AddSIPInviteStep(ctx, id, 18, sipStepIdentity(17))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	var canonical string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&canonical).Error)
	for _, raw := range []string{"", "null", "[]", `{"version":2,"steps":[]}`, `{"version":1,"version":1,"steps":[]}`,
		strings.Replace(canonical, `"state":"prepared"`, `"state":"closed"`, 1),
		strings.Replace(canonical, `"action":"invite"`, `"action":"bye"`, 1),
		strings.Replace(canonical, `"version":1`, `"version":1,"remoteTag":"unobserved"`, 1), strings.Repeat("x", 32769)} {
		require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", raw).Error)
		_, err := store.LoadSIPInviteSteps(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		_, err = store.DispatchSIPInviteStep(ctx, id, 18, sipStepIdentity(1).StepID)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	}
}

func TestDeviceSIPStepsByteBudgetPreservesPriorMaterial(t *testing.T) {
	f, store, id := newSIPStepFixture(t)
	ctx := context.Background()
	uri := "sip:" + strings.Repeat("u", 256) + "@" + strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60) + ".example.com:5060"
	var last DeviceSIPInviteSteps
	var before string
	for n := 1; n <= 16; n++ {
		i := sipStepIdentity(n)
		i.RequestURI, i.FromURI, i.ToURI, i.ContactURI = uri, uri, uri, uri
		require.True(t, validSIPInviteIdentity(i), "failure must be aggregate bytes, not identity validation")
		out, err := store.AddSIPInviteStep(ctx, id, int64(n+1), i)
		if err != nil {
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out.Intent.OperationID)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			require.Equal(t, last, loaded)
			var after string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&after).Error)
			require.Equal(t, before, after, "over-budget write must preserve exact prior JSON")
			return
		}
		last = out
		require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&before).Error)
	}
	t.Fatal("fixture failed to exercise the byte limit before the step limit")
}

func TestDeviceSIPStepsRequireSchemaAndOriginalParent(t *testing.T) {
	_, unmigrated, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := unmigrated.LoadSIPInviteSteps(ctx, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, err = unmigrated.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, store, id := newSIPStepFixture(t)
	for _, change := range []func(*DeviceOperationIntentIdentity){
		func(i *DeviceOperationIntentIdentity) { i.DeviceEpoch++ },
		func(i *DeviceOperationIntentIdentity) { i.Kind = "playback" },
		func(i *DeviceOperationIntentIdentity) { i.TargetCode = cleanupDeviceB },
	} {
		other := id
		change(&other)
		_, err = store.LoadSIPInviteSteps(ctx, other)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.AddSIPInviteStep(cancelled, id, 2, sipStepIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

func TestDeviceSIPStepsStoredTimeMustBeUTCMicroseconds(t *testing.T) {
	for _, change := range []string{"offset", "nanoseconds"} {
		t.Run(change, func(t *testing.T) {
			f, store, id := newSIPStepFixture(t)
			_, err := store.AddSIPInviteStep(context.Background(), id, 2, sipStepIdentity(1))
			require.NoError(t, err)
			// Give the tampered prepared instant room below parent.updated_at,
			// so this tests the time representation, not chronological ordering.
			_, err = store.AddSIPInviteStep(context.Background(), id, 3, sipStepIdentity(2))
			require.NoError(t, err)
			var raw string
			require.NoError(t, f.db.Table("gb_device_operation_intent").Select("sip_steps_json").Scan(&raw).Error)
			var wire sipInviteStepsWire
			require.NoError(t, json.Unmarshal([]byte(raw), &wire))
			if change == "offset" {
				wire.Steps[0].PreparedAt = wire.Steps[0].PreparedAt.In(time.FixedZone("fixture", 8*3600))
			} else {
				wire.Steps[0].PreparedAt = wire.Steps[0].PreparedAt.Add(time.Nanosecond)
			}
			body, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET sip_steps_json=?", string(body)).Error)
			_, err = store.LoadSIPInviteSteps(context.Background(), id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
}
