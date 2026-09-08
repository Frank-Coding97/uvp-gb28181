package playauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func originalCloseFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore, DeviceOperationIntentIdentity, *RTPResourceWork) {
	t.Helper()
	f, store, id := newRTPStepFixture(t)
	_, err := store.AddRTPResourceStep(context.Background(), id, 2, rtpStepIdentity(1))
	require.NoError(t, err)
	_, work, err := store.DispatchRTPResourceWork(context.Background(), id, 3, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	return f, store, id, work
}

type originalCloseAfterCommitPool struct {
	*sql.DB
	after func()
}

type originalCloseAfterCommitTx struct {
	*sql.Tx
	after func()
}

func (p originalCloseAfterCommitPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &originalCloseAfterCommitTx{Tx: tx, after: p.after}, nil
}

func (tx originalCloseAfterCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.after()
	return nil
}

func TestDeviceRTPOriginalCloseCancellationAfterCommitNeverCalls(t *testing.T) {
	for _, seal := range []bool{false, true} {
		t.Run(fmt.Sprint(seal), func(t *testing.T) {
			f, store, id, work := originalCloseFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pool, err := f.db.DB()
			require.NoError(t, err)
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
			faultDB.Statement.ConnPool = originalCloseAfterCommitPool{DB: pool, after: func() {
				if seal {
					work.work.sealed.Store(true)
				} else {
					cancel()
				}
			}}
			work.work.store = newIntentFixtureStore(faultDB)
			_, err = work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
				t.Fatal("cancelled or sealed after CAS cannot invoke network")
				return "", nil
			})
			require.Error(t, err)
			work.work.store = store
			require.NoError(t, work.Quiesce(context.Background()))
			loaded, err := store.LoadRTPResourceSteps(context.Background(), id)
			require.NoError(t, err)
			require.EqualValues(t, 1, loaded.Steps[0].OriginalCloseCallSequence)
			require.Equal(t, rtpCallNotInvoked, loaded.Steps[0].ResourceCloseCall.Outcome)
		})
	}
}

func TestDeviceRTPOriginalCloseJoinsActualCallBeforeQuiescence(t *testing.T) {
	_, store, id, work := originalCloseFixture(t)
	ctx := context.Background()
	entered, unblock, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
			close(entered)
			<-unblock
			return "close_pending", nil
		})
		done <- err
	}()
	<-entered
	short, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	err := work.Quiesce(short)
	cancel()
	close(unblock)
	callErr := <-done
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, callErr)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Nil(t, loaded.Steps[0].LocalQuiescedAt)
	require.NoError(t, work.Quiesce(ctx))
	loaded, err = store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
}

func TestDeviceRTPOriginalCloseFlushCannotMintSlot(t *testing.T) {
	_, store, id, work := originalCloseFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	work.work.step.OriginalCloseCallSequence = 1
	work.work.step.ResourceCloseCall = &DeviceRTPOriginalCloseCall{Sequence: 1, DispatchStartedAt: now, Outcome: rtpCallNotInvoked, LocalQuiescedAt: &now}
	require.ErrorIs(t, work.Flush(ctx), ErrDeviceIntentConflict)
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Zero(t, loaded.Steps[0].OriginalCloseCallSequence)
	require.Nil(t, loaded.Steps[0].ResourceCloseCall)
}

func TestDeviceRTPOriginalCloseFactsSurviveIndependentRecovery(t *testing.T) {
	f, store, id, original := originalCloseFixture(t)
	ctx := context.Background()
	_, err := original.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "close_pending", nil })
	require.NoError(t, err)
	require.NoError(t, original.Quiesce(ctx))
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	barrier := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	recovery, err := barrier.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
	require.NoError(t, err)
	require.NoError(t, recovery.Prepare(ctx))
	_, err = recovery.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "shutdown_scheduled", nil })
	require.NoError(t, err)
	before, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.NoError(t, original.Flush(ctx))
	require.NoError(t, original.Quiesce(ctx))
	after, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before, after, "old owner must not overwrite independent recovery or bump versions for already durable facts")
	require.Equal(t, "close_pending", after.Steps[0].ResourceCloseResult)
	require.Equal(t, "shutdown_scheduled", after.Steps[0].Recovery.ResourceEvidence.Result)
	require.NoError(t, recovery.Quiesce(ctx))
}

func TestDeviceRTPOriginalCloseRequiresConfirmedCAS(t *testing.T) {
	for _, ingress := range []bool{false, true} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("ingress=%v/committed=%v", ingress, committed), func(t *testing.T) {
				f, store, id, work := originalCloseFixture(t)
				ctx := context.Background()
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
				work.work.store = newIntentFixtureStore(faultDB)
				closeCall := work.CloseResource
				if ingress {
					closeCall = work.CloseIngress
				}
				calls := 0
				call := func(context.Context, DeviceRTPResourceIdentity) (string, error) {
					calls++
					return "close_pending", nil
				}
				_, err := closeCall(ctx, call)
				require.Error(t, err, "a lost SQL commit acknowledgement must deny the network call")
				require.Zero(t, calls)
				before, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				if committed {
					require.EqualValues(t, 1, before.Steps[0].OriginalCloseCallSequence)
				} else {
					require.Zero(t, before.Steps[0].OriginalCloseCallSequence)
				}
				work.work.store = store
				require.NoError(t, work.Quiesce(ctx))
				_, err = closeCall(ctx, call)
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
				require.Zero(t, calls)
				loaded, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				require.NotNil(t, loaded.Steps[0].LocalQuiescedAt)
				require.Empty(t, loaded.Steps[0].ResourceCloseResult)
				require.Empty(t, loaded.Steps[0].IngressCloseResult)
				if committed {
					c := loaded.Steps[0].ResourceCloseCall
					if ingress {
						c = loaded.Steps[0].IngressCloseCall
					}
					require.Equal(t, rtpCallNotInvoked, c.Outcome)
					require.NotNil(t, c.LocalQuiescedAt)
				} else {
					require.Zero(t, loaded.Steps[0].OriginalCloseCallSequence)
				}
			})
		}
	}
}

func TestDeviceRTPOriginalCloseSlotsAreDurableAndSingleUse(t *testing.T) {
	for _, ingressFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(ingressFirst), func(t *testing.T) {
			_, store, id, work := originalCloseFixture(t)
			ctx := context.Background()
			copy := *work
			calls := []func(context.Context, func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error){work.CloseResource, copy.CloseIngress}
			if ingressFirst {
				calls[0], calls[1] = calls[1], calls[0]
			}
			for i, closeCall := range calls {
				_, err := closeCall(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
					loaded, err := store.LoadRTPResourceSteps(ctx, id)
					require.NoError(t, err)
					require.EqualValues(t, i+1, loaded.Steps[0].OriginalCloseCallSequence)
					return "close_pending", nil
				})
				require.NoError(t, err)
				_, err = closeCall(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
					t.Fatal("copy or repeat must never resend")
					return "", nil
				})
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
			require.NoError(t, work.Quiesce(ctx))
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			for _, c := range []*DeviceRTPOriginalCloseCall{loaded.Steps[0].ResourceCloseCall, loaded.Steps[0].IngressCloseCall} {
				require.Equal(t, rtpCallObserved, c.Outcome)
				require.NotNil(t, c.LocalQuiescedAt)
			}
			require.Equal(t, IntentDispatched, loaded.Intent.State)
		})
	}
}

func TestDeviceRTPOriginalCloseUnknownResultsAndLostOutcomeCommit(t *testing.T) {
	for _, kind := range []string{"network-error", "invalid-result", "drained-tcp", "outcome-rollback", "outcome-committed"} {
		t.Run(kind, func(t *testing.T) {
			f, store, id, work := originalCloseFixture(t)
			ctx := context.Background()
			_, err := work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
				if kind == "network-error" {
					return "", errors.New("lost response")
				}
				if kind == "invalid-result" {
					return "complete", nil
				}
				if kind == "drained-tcp" {
					return "rtp_ingress_drained", nil
				}
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: kind == "outcome-committed"}
				work.work.store = newIntentFixtureStore(faultDB)
				return "close_pending", nil
			})
			require.Error(t, err)
			work.work.store = store
			require.NoError(t, work.Quiesce(ctx))
			loaded, err := store.LoadRTPResourceSteps(ctx, id)
			require.NoError(t, err)
			want := rtpCallUnknown
			if kind == "outcome-rollback" || kind == "outcome-committed" {
				want = rtpCallObserved
			}
			require.Equal(t, want, loaded.Steps[0].IngressCloseCall.Outcome)
			require.NotNil(t, loaded.Steps[0].IngressCloseCall.LocalQuiescedAt)
			_, err = work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
				t.Fatal("unknown never resends")
				return "", nil
			})
			require.ErrorIs(t, err, ErrDeviceIntentConflict)
		})
	}
}

func TestDeviceRTPOriginalCloseCanonicalAndMalformedSlots(t *testing.T) {
	f, store, id, work := originalCloseFixture(t)
	ctx := context.Background()
	for _, closeCall := range []func(context.Context, func(context.Context, DeviceRTPResourceIdentity) (string, error)) (string, error){work.CloseResource, work.CloseIngress} {
		_, err := closeCall(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) { return "close_pending", nil })
		require.NoError(t, err)
	}
	require.NoError(t, work.Quiesce(ctx))
	var raw string
	require.NoError(t, f.db.Table("gb_device_operation_intent").Select("rtp_steps_json").Scan(&raw).Error)
	for name, mutate := range map[string]func(*rtpStepWire){
		"zero-process": func(w *rtpStepWire) { w.OwnerProcessID = strings.Repeat("0", 32) },
		"offset-result": func(w *rtpStepWire) {
			stamp := w.ResourceCloseObservedAt.In(time.FixedZone("plus", 3600))
			w.ResourceCloseObservedAt = &stamp
		},
		"no-sequence":           func(w *rtpStepWire) { w.OriginalCloseCallSequence = 0 },
		"duplicate-sequence":    func(w *rtpStepWire) { w.IngressCloseCall.Sequence = 1 },
		"missing-slot":          func(w *rtpStepWire) { w.ResourceCloseCall = nil },
		"unknown-with-result":   func(w *rtpStepWire) { w.ResourceCloseCall.Outcome = rtpCallUnknown },
		"pending-after-quiesce": func(w *rtpStepWire) { w.ResourceCloseCall.Outcome = ""; w.ResourceCloseCall.LocalQuiescedAt = nil },
		"invalid-outcome":       func(w *rtpStepWire) { w.ResourceCloseCall.Outcome = "complete" },
		"early-call":            func(w *rtpStepWire) { w.ResourceCloseCall.DispatchStartedAt = w.PreparedAt.Add(-time.Second) },
		"nanoseconds": func(w *rtpStepWire) {
			w.ResourceCloseCall.DispatchStartedAt = w.ResourceCloseCall.DispatchStartedAt.Add(time.Nanosecond)
		},
		"reversed-order": func(w *rtpStepWire) {
			w.IngressCloseCall.DispatchStartedAt = w.ResourceCloseCall.DispatchStartedAt.Add(-time.Microsecond)
		},
		"mismatched-time": func(w *rtpStepWire) {
			stamp := w.ResourceCloseObservedAt.Add(time.Microsecond)
			w.ResourceCloseCall.LocalQuiescedAt = &stamp
		},
	} {
		t.Run(name, func(t *testing.T) {
			var wire rtpStepsWire
			require.NoError(t, json.Unmarshal([]byte(raw), &wire))
			mutate(&wire.Steps[0])
			body, err := json.Marshal(wire)
			require.NoError(t, err)
			require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?", string(body)).Error)
			_, err = store.LoadRTPResourceSteps(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
		})
	}
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?", raw).Error)
	_, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(t, err)
	for _, malformed := range []string{
		strings.Replace(raw, `"resourceCloseCall":{`, `"resourceCloseCall":null,"unknownCall":{`, 1),
		strings.Replace(raw, `"resourceCloseCall":{`, `"resourceCloseCall":{"unexpected":true,`, 1),
	} {
		require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET rtp_steps_json=?", malformed).Error)
		_, err := store.LoadRTPResourceSteps(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	}
}

func TestDeviceRTPOriginalCloseFlushFailureBlocksNextNetworkCall(t *testing.T) {
	f, store, _, work := originalCloseFixture(t)
	ctx := context.Background()
	failing := false
	const callback = "test:original-close-outcome-failure"
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if failing {
			tx.AddError(errors.New("fixture outcome persistence unavailable"))
		}
	}))
	t.Cleanup(func() { _ = f.db.Callback().Update().Remove(callback) })
	calls := 0
	_, err := work.CloseResource(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		calls++
		failing = true
		return "close_pending", nil
	})
	require.Error(t, err, "close must report failed outcome persistence")
	_, err = work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		calls++
		return "close_pending", nil
	})
	require.Error(t, err)
	require.Equal(t, 1, calls)
	failing = false
	work.work.store = store
	require.NoError(t, work.Flush(ctx))
	_, err = work.CloseIngress(ctx, func(context.Context, DeviceRTPResourceIdentity) (string, error) {
		calls++
		return "close_pending", nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.NoError(t, work.Quiesce(ctx))
}
