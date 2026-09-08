package playauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceRTPWorkRecordsActualOutcomeAndQuiescence(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NotNil(t, work)
	var opens int
	open := func(_ context.Context, identity DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error) {
		opens++
		require.Equal(t, rtpStepIdentity(1), identity)
		return DeviceRTPOpenResult{Result: "created", Port: 30000}, nil
	}
	result, err := work.Open(ctx, open)
	require.NoError(t, err)
	require.Equal(t, 30000, result.Port)
	_, err = work.Open(ctx, open)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	require.Equal(t, 1, opens)
	require.NoError(t, work.Flush(ctx))
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "created", loaded.Steps[0].OpenResult.Result)
	require.Nil(t, loaded.Steps[0].LocalQuiescedAt)
	// A transfer must not prevent the old owner from recording actual cleanup.
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		return "close_pending", nil
	})
	require.NoError(t, err)
	require.NoError(t, work.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "close_pending", loaded.Steps[0].IngressCloseResult)
	require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
	require.Equal(t, IntentDispatched, loaded.Intent.State)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch)
	_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		t.Fatal("quiesced owner must never start another HTTP call")
		return "", nil
	})
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
}

func TestDeviceRTPWorkPersistsActualProcessIdentity(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	out, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	processID, err := sipCleanupProcessID()
	require.NoError(t, err)
	require.Equal(t, processID, out.Steps[0].OwnerProcessID)
	require.NotEqual(t, out.Steps[0].OwnerRunID, out.Steps[0].OwnerProcessID)
	loaded, err := NewDeviceOperationIntentStore(f.db).LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, out.Steps[0].OwnerProcessID, loaded.Steps[0].OwnerProcessID)
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Scan(&raw).Error)
	var wire rtpStepsWire
	require.NoError(t, json.Unmarshal([]byte(raw), &wire))
	wire.Steps[0].OwnerProcessID = strings.Repeat("f", 32)
	encoded, err := json.Marshal(wire)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?", string(encoded)).Error)
	require.ErrorIs(t, work.Quiesce(ctx), ErrDeviceIntentConflict, "an owner cannot rewrite another process's record")
	var after string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Scan(&after).Error)
	require.Equal(t, string(encoded), after)
	// Historical canonical rows remain readable, with no inferred process ID.
	wire.Steps[0].OwnerProcessID = ""
	encoded, err = json.Marshal(wire)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?", string(encoded)).Error)
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Empty(t, loaded.Steps[0].OwnerProcessID)
	require.ErrorIs(t, work.Flush(ctx), ErrDeviceIntentConflict)
}

func TestDeviceRTPExecutionCanonicalLegacyAndMalformedFacts(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	var legacy string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id = ?", id.OperationID).Scan(&legacy).Error)
	require.NotContains(t, legacy, "ownerRunID")
	var old rtpStepsWire
	require.NoError(t, json.Unmarshal([]byte(legacy), &old))
	roundtrip, err := json.Marshal(old)
	require.NoError(t, err)
	require.Equal(t, legacy, string(roundtrip), "old v1 canonical bytes remain unchanged")
	_, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	_, err = work.Open(ctx, func(context.Context, DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error) {
		return DeviceRTPOpenResult{Result: "created", Port: 30000}, nil
	})
	require.NoError(t, err)
	require.NoError(t, work.Quiesce(ctx))
	var valid string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Where("operation_id = ?", id.OperationID).Scan(&valid).Error)
	for _, kind := range []string{"null", "unknown-field", "unknown-result", "no-owner", "invalid-process", "unpaired-time", "early-quiesced", "version-ahead"} {
		t.Run(kind, func(t *testing.T) {
			var wire rtpStepsWire
			require.NoError(t, json.Unmarshal([]byte(valid), &wire))
			switch kind {
			case "unknown-result":
				wire.Steps[0].OpenResult.Result = "complete"
			case "no-owner":
				wire.Steps[0].OwnerRunID = ""
			case "invalid-process":
				wire.Steps[0].OwnerProcessID = "not-a-process-identity"
			case "unpaired-time":
				wire.Steps[0].OpenObservedAt = nil
			case "early-quiesced":
				early := wire.Steps[0].PreparedAt.Add(-time.Second)
				wire.Steps[0].LocalQuiescedAt = &early
			case "version-ahead":
				wire.Steps[0].RowVersion = 10000
			}
			body, err := json.Marshal(wire)
			require.NoError(t, err)
			raw := string(body)
			if kind == "null" {
				raw = strings.Replace(raw, `"ownerRunID":"`+wire.Steps[0].OwnerRunID+`"`, `"ownerRunID":null`, 1)
			}
			if kind == "unknown-field" {
				raw = strings.Replace(raw, `"version":1`, `"version":1,"complete":true`, 1)
			}
			require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("rtp_steps_json", raw).Error)
			_, err = store.LoadRTPResourceSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
	require.NoError(t, f.db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("rtp_steps_json", valid).Error)
	_, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
}

func TestDeviceRTPWorkQuiesceJoinsIgnoredCancellation(t *testing.T) {
	_, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = work.Open(ctx, func(context.Context, DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error) {
			close(entered)
			<-unblock
			return DeviceRTPOpenResult{}, errors.New("fixture lost HTTP response")
		})
	}()
	<-entered
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	err = work.Quiesce(short)
	close(unblock)
	<-done
	require.ErrorIs(t, err, context.DeadlineExceeded)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].LocalQuiescedAt)
	require.NoError(t, work.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].OpenResult, "no response is unknown, not a made-up close proof")
	require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
}

func TestDeviceRTPWorkCommitUnknownCannotMintHandle(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f, store, id := newRTPStepFixture(t)
			ctx := context.Background()
			_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
			require.NoError(t, err)
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
			_, work, err := NewDeviceOperationIntentStore(faultDB).DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Nil(t, work)
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			if committed {
				require.NotEmpty(t, loaded.Steps[0].OwnerRunID)
				_, work, err = store.DispatchRTPResourceWork(ctx, id, loaded.Intent.RowVersion, rtpStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
				require.Nil(t, work)
			}
		})
	}
}

func TestDeviceRTPPreparedOwnerCanJoinUnknownDispatchButNeverOpen(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f, store, id := newRTPStepFixture(t)
			ctx := context.Background()
			_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
			require.NoError(t, err)
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
			work, err := NewDeviceOperationIntentStore(faultDB).PrepareRTPResourceWork(ctx, id, rtpStepIdentity(1).StepID)
			require.NoError(t, err)
			_, err = work.Dispatch(ctx, 3)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			_, err = work.Open(ctx, func(context.Context, DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error) {
				t.Fatal("an unknown commit cannot admit the first Open")
				return DeviceRTPOpenResult{}, nil
			})
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
			// Restore only this fixture's transient commit failure, same database.
			faultDB.Statement.ConnPool = f.db.Statement.ConnPool
			require.NoError(t, work.Quiesce(ctx))
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			if committed {
				require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
				require.Nil(t, loaded.Steps[0].OpenResult)
			} else {
				require.Equal(t, RTPStepPrepared, loaded.Steps[0].State)
				require.Empty(t, loaded.Steps[0].OwnerRunID)
			}
			_, err = work.Dispatch(ctx, loaded.Intent.RowVersion)
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		})
	}
}

func TestDeviceRTPWorkLostObservationCommitKeepsLaterActualCleanupFacts(t *testing.T) {
	f, store, id := newRTPStepFixture(t)
	ctx := context.Background()
	identity := rtpStepIdentity(1)
	identity.TCPMode = 0 // The ingress-drained result is qualified only for UDP.
	_, err := store.AddRTPResourceStep(ctx, id, 2, identity)
	require.NoError(t, err)
	_, work, err := store.DispatchRTPResourceWork(ctx, id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	_, err = work.Open(ctx, func(context.Context, DeviceRTPResourceIdentity) (DeviceRTPOpenResult, error) {
		return DeviceRTPOpenResult{Result: "created", Port: 30000}, nil
	})
	require.NoError(t, err)
	faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
	faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: true}
	work.work.store = NewDeviceOperationIntentStore(faultDB) // same dedicated DB, transient commit acknowledgement loss
	require.ErrorIs(t, work.Flush(ctx), ErrDeviceIntentUnavailable)
	work.work.store = store
	_, err = work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "rtp_ingress_drained", nil })
	require.NoError(t, err)
	require.NoError(t, work.Quiesce(ctx), "a prior committed response must not strand later actually observed facts")
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "created", loaded.Steps[0].OpenResult.Result)
	require.Equal(t, "rtp_ingress_drained", loaded.Steps[0].IngressCloseResult)
	require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
}
