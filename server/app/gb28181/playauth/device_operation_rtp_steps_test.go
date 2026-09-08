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

func newRTPStepFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity) {
	t.Helper()
	f, store := newIntentFixture(t)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	id := intentIdentity(1)
	_, err := store.Reserve(context.Background(), id)
	require.NoError(t, err)
	_, err = store.Dispatch(context.Background(), id, 1)
	require.NoError(t, err)
	return f, store, id
}

func rtpStepIdentity(n int) DeviceRTPResourceIdentity {
	resourceID, _ := NewDeviceRTPResourceID(intentIdentity(1).OperationID, fmt.Sprintf("%032x", n), time.UnixMilli(1788750000000))
	return DeviceRTPResourceIdentity{StepID: fmt.Sprintf("%032x", n), NodePK: 3, NodeUUID: "fixture-node", NodeRevision: 9,
		BootNonce: strings.Repeat("a", 32), ResourceID: resourceID,
		VHost: "__defaultVhost__", App: "rtp", Stream: fmt.Sprintf("fixture-step-%d", n),
		Port: 0, LocalIP: "127.0.0.1", TCPMode: 1, SSRC: 12345, OnlyTrack: 0}
}

func TestDeviceRTPStepsPreserveEveryIdentityAndNeverComplete(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	first, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	require.Equal(t, int64(3), first.Intent.RowVersion)
	require.Equal(t, RTPStepPrepared, first.Steps[0].State)
	second, err := store.AddRTPResourceStep(ctx, id, 3, rtpStepIdentity(2))
	require.NoError(t, err)
	require.Len(t, second.Steps, 2)
	dispatched, err := store.DispatchRTPResourceStep(ctx, id, 4, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.Equal(t, RTPStepMayHaveDispatched, dispatched.Steps[0].State)
	require.Equal(t, RTPStepPrepared, dispatched.Steps[1].State)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, dispatched, loaded)
	require.Equal(t, rtpStepIdentity(1), loaded.Steps[0].Identity)
	require.Equal(t, rtpStepIdentity(2), loaded.Steps[1].Identity)
	for _, value := range []any{loaded, loaded.Steps[0], loaded.Steps[0].Identity} {
		body, err := json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body), "internal materials must not become API JSON")
	}
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = store.DispatchRTPResourceStep(ctx, id, 5, rtpStepIdentity(2).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	_, err = store.AddRTPResourceStep(ctx, id, 5, rtpStepIdentity(3))
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err, "old epoch remains readable for cleanup, never reauthorization")
	require.Equal(t, dispatched, loaded)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch)
}

func TestDeviceRTPStepsCASAndDuplicateMaterials(t *testing.T) {
	_, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	observed, err := store.AddRTPResourceStep(ctx, id, 3, rtpStepIdentity(1))
	require.NoError(t, err)
	require.EqualValues(t, 3, observed.Intent.RowVersion)
	require.Len(t, observed.Steps, 1)
	for _, snapshot := range []DeviceRTPResourceIdentity{func() DeviceRTPResourceIdentity {
		value := rtpStepIdentity(1)
		value.BootNonce = strings.Repeat("b", 32)
		return value
	}(), func() DeviceRTPResourceIdentity {
		value := rtpStepIdentity(1)
		value.ResourceID = "d1788750099999-" + value.ResourceID[15:]
		return value
	}()} {
		_, err = store.AddRTPResourceStep(ctx, id, 3, snapshot)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	var wg sync.WaitGroup
	var winners atomic.Int32
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.DispatchRTPResourceStep(ctx, id, 3, rtpStepIdentity(1).StepID)
			if err == nil {
				winners.Add(1)
			} else {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())
	_, err = store.DispatchRTPResourceStep(ctx, id, 4, rtpStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentConflict, "read-back never grants a second dispatch")
}

func TestDeviceRTPStepsConcurrentAppendNeverLosesEarlierResource(t *testing.T) {
	_, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	type attempt struct {
		n   int
		err error
	}
	results := make(chan attempt, 2)
	for n := 1; n <= 2; n++ {
		go func(n int) {
			_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(n))
			results <- attempt{n, err}
		}(n)
	}
	winners, loser := 0, 0
	for n := 0; n < 2; n++ {
		result := <-results
		if result.err == nil {
			winners++
		} else {
			require.ErrorIs(t, result.err, ErrDeviceIntentConflict)
			loser = result.n
		}
	}
	require.Equal(t, 1, winners)
	// Only preparation is explicitly retried with a fresh parent CAS version.
	// This is not a network-dispatch retry or an automatic recovery action.
	out, err := store.AddRTPResourceStep(ctx, id, 3, rtpStepIdentity(loser))
	require.NoError(t, err)
	require.Len(t, out.Steps, 2)
	require.Equal(t, rtpStepIdentity(loser), out.Steps[1].Identity)
	require.NotEqual(t, out.Steps[0].Identity.StepID, out.Steps[1].Identity.StepID)
	require.Equal(t, RTPStepPrepared, out.Steps[0].State)
	require.Equal(t, RTPStepPrepared, out.Steps[1].State)
}

func TestDeviceRTPStepsRejectBadMaterialAndBoundGrowth(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	for _, change := range []func(*DeviceRTPResourceIdentity){
		func(s *DeviceRTPResourceIdentity) { s.StepID = "bad" },
		func(s *DeviceRTPResourceIdentity) { s.NodePK = 0 },
		func(s *DeviceRTPResourceIdentity) { s.NodeUUID = "" },
		func(s *DeviceRTPResourceIdentity) { s.NodeRevision = 0 },
		func(s *DeviceRTPResourceIdentity) { s.ResourceID = "bad" },
		func(s *DeviceRTPResourceIdentity) { s.BootNonce = "bad" },
		func(s *DeviceRTPResourceIdentity) { s.VHost = "alias" },
		func(s *DeviceRTPResourceIdentity) { s.App = "a/b" },
		func(s *DeviceRTPResourceIdentity) { s.Stream = "" },
		func(s *DeviceRTPResourceIdentity) { s.Port = 65535 },
		func(s *DeviceRTPResourceIdentity) { s.LocalIP = "example.invalid" },
		func(s *DeviceRTPResourceIdentity) { s.TCPMode = 2 },
		func(s *DeviceRTPResourceIdentity) { s.OnlyTrack = 3 },
	} {
		snapshot := rtpStepIdentity(1)
		change(&snapshot)
		_, err := store.AddRTPResourceStep(ctx, id, 2, snapshot)
		require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	}
	for n := 1; n <= 16; n++ {
		_, err := store.AddRTPResourceStep(ctx, id, int64(n+1), rtpStepIdentity(n))
		require.NoError(t, err)
	}
	_, err := store.AddRTPResourceStep(ctx, id, 18, rtpStepIdentity(17))
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Len(t, loaded.Steps, 16)
	for _, raw := range []string{"", "null", "[]", `{"version":2,"steps":[]}`, `{"version":1,"version":1,"steps":[]}`, strings.Repeat("x", 32769)} {
		require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=? WHERE operation_id=?", raw, id.OperationID).Error)
		_, err = store.LoadRTPResourceSteps(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		_, err = store.AddRTPResourceStep(ctx, id, 18, rtpStepIdentity(17))
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	}
}

func TestDeviceRTPStepsCommitUnknownNeverGrantsDispatch(t *testing.T) {
	for _, commitFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%v", commitFirst), func(t *testing.T) {
			f, normal, id := newRTPStepFixture(t)
			ctx := context.Background()
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: commitFirst}
			fault := newIntentFixtureStore(faultDB)
			out, err := fault.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out.Intent.OperationID)
			loaded, err := normal.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			if !commitFirst {
				require.Empty(t, loaded.Steps)
				_, err = normal.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
				require.NoError(t, err)
			}
			out, err = fault.DispatchRTPResourceStep(ctx, id, 3, rtpStepIdentity(1).StepID)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out.Intent.OperationID)
			loaded, err = normal.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			if commitFirst {
				require.Equal(t, RTPStepMayHaveDispatched, loaded.Steps[0].State)
				_, err = normal.DispatchRTPResourceStep(ctx, id, 4, rtpStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			} else {
				require.Equal(t, RTPStepPrepared, loaded.Steps[0].State)
			}
		})
	}
}

func TestDeviceRTPStepsMissingSchemaAndParentStatesFailClosed(t *testing.T) {
	f, store := newIntentFixture(t)
	ctx := context.Background()
	id := intentIdentity(1)
	_, err := store.Reserve(ctx, id)
	require.NoError(t, err)
	_, err = store.Dispatch(ctx, id, 1)
	require.NoError(t, err)
	_, err = store.LoadRTPResourceSteps(ctx, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable, "missing column is not empty history")
	_, err = store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Empty(t, loaded.Steps, "NULL carries no completion/coverage claim")
	reserved := intentIdentity(2)
	_, err = store.Reserve(ctx, reserved)
	require.NoError(t, err)
	reservedStep := rtpStepIdentity(1)
	reservedStep.ResourceID, err = NewDeviceRTPResourceID(reserved.OperationID, reservedStep.StepID, time.UnixMilli(1788750000000))
	require.NoError(t, err)
	_, err = store.AddRTPResourceStep(ctx, reserved, 2, reservedStep)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	require.NoError(t, store.CancelReserved(ctx, reserved.OperationID, 1))
	_, err = store.AddRTPResourceStep(ctx, reserved, 2, reservedStep)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	wrong := id
	wrong.Kind = "talk"
	_, err = store.LoadRTPResourceSteps(ctx, wrong)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	_, err = store.LoadRTPResourceSteps(nil, id)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, err = NewDeviceOperationIntentStore(nil).AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

func TestDeviceRTPStepsBindResourcesToOriginalOperation(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	step := rtpStepIdentity(1)
	_, err := store.AddRTPResourceStep(ctx, id, 2, step)
	require.NoError(t, err)
	other := intentIdentity(2)
	_, err = store.Reserve(ctx, other)
	require.NoError(t, err)
	_, err = store.Dispatch(ctx, other, 1)
	require.NoError(t, err)
	_, err = store.AddRTPResourceStep(ctx, other, 2, step)
	require.ErrorIs(t, err, ErrDeviceIntentInvalid, "cross-parent copying must fail before granting network rights")
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id=?", id.OperationID).Scan(&raw).Error)
	var wire rtpStepsWire
	require.NoError(t, json.Unmarshal([]byte(raw), &wire))
	now := time.Now().UTC()
	wire.Steps[0].PreparedAt = now
	body, err := json.Marshal(wire)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?, updated_at=? WHERE operation_id=?", string(body), now, other.OperationID).Error)
	_, err = store.LoadRTPResourceSteps(ctx, other)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable, "read path also rejects transported evidence")
}

func TestDeviceRTPStepsResourceBindingGolden(t *testing.T) {
	op, step := intentIdentity(1).OperationID, fmt.Sprintf("%032x", 1)
	now := time.UnixMilli(1788750000000)
	id, err := NewDeviceRTPResourceID(op, step, now)
	require.NoError(t, err)
	// Independently calculated with openssl dgst -sha256 and NUL separators.
	require.Equal(t, "d1788750025000-6e7437fbe1c330e044fd9f45ffd876ff", id)
	second, err := NewDeviceRTPResourceID(op, step, now)
	require.NoError(t, err)
	require.Equal(t, id, second)
	other, err := NewDeviceRTPResourceID(intentIdentity(2).OperationID, step, now)
	require.NoError(t, err)
	require.NotEqual(t, id[15:], other[15:])
	_, err = NewDeviceRTPResourceID("bad", step, now)
	require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	_, err = NewDeviceRTPResourceID(op, step, time.Time{})
	require.ErrorIs(t, err, ErrDeviceIntentInvalid)
}

func TestDeviceRTPStepsRejectCanonicalButInvalidEvidence(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	var original string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id=?", id.OperationID).Scan(&original).Error)
	for _, change := range []func(*rtpStepsWire){
		func(w *rtpStepsWire) { w.Steps = append(w.Steps, w.Steps[0]) },
		func(w *rtpStepsWire) { w.Steps[0].Version = 2 },
		func(w *rtpStepsWire) { w.Steps[0].Action = "close_rtp" },
		func(w *rtpStepsWire) { w.Steps[0].State = "closed" },
		func(w *rtpStepsWire) { w.Steps[0].RowVersion = 2 },
		func(w *rtpStepsWire) { w.Steps[0].State = RTPStepMayHaveDispatched; w.Steps[0].RowVersion = 2 },
		func(w *rtpStepsWire) { w.Steps[0].PreparedAt = w.Steps[0].PreparedAt.Add(-time.Hour) },
	} {
		var wire rtpStepsWire
		require.NoError(t, json.Unmarshal([]byte(original), &wire))
		change(&wire)
		body, err := json.Marshal(wire)
		require.NoError(t, err)
		require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=? WHERE operation_id=?", string(body), id.OperationID).Error)
		_, err = store.LoadRTPResourceSteps(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		_, err = store.DispatchRTPResourceStep(ctx, id, 3, rtpStepIdentity(1).StepID)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	}
}
