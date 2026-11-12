package playauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Same-package state-machine fixtures do not claim OS lifetime coverage.
// Cross-package effect tests must obtain a concrete Authority via Register.
type intentFixtureAuthority struct{}

func (intentFixtureAuthority) GenerationID() string { id, _ := sipCleanupProcessID(); return id }
func (intentFixtureAuthority) CheckTx(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil {
		return ErrDeviceIntentUnavailable
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return ErrDeviceIntentUnavailable
	}
	return nil
}
func (intentFixtureAuthority) RequireRetiredTx(tx *gorm.DB, id string) error {
	return ErrDeviceIntentUnavailable
}

func newIntentFixtureStore(db *gorm.DB) *DeviceOperationIntentStore {
	return newDeviceOperationIntentStore(db, intentFixtureAuthority{})
}

type intentCheckingAuthority struct {
	check   func(*gorm.DB) error
	retired func(*gorm.DB, string) error
}

func (a *intentCheckingAuthority) GenerationID() string      { id, _ := sipCleanupProcessID(); return id }
func (a *intentCheckingAuthority) CheckTx(tx *gorm.DB) error { return a.check(tx) }
func (a *intentCheckingAuthority) RequireRetiredTx(tx *gorm.DB, id string) error {
	if a.retired != nil {
		return a.retired(tx, id)
	}
	return ErrDeviceIntentUnavailable
}

func TestDeviceIntentAuthorityPrecedesDeviceLockInSameTransaction(t *testing.T) {
	f, store := newIntentFixture(t)
	ctx, id := context.Background(), intentIdentity(1)
	_, err := store.Reserve(ctx, id)
	require.NoError(t, err)
	var checked *sql.Tx
	checker := &intentCheckingAuthority{check: func(tx *gorm.DB) error {
		var ok bool
		checked, ok = tx.Statement.ConnPool.(*sql.Tx)
		require.True(t, ok)
		return nil
	}}
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("authority_order", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device" || tx.Statement.Table == "gb_device_operation_intent" {
			require.NotNil(t, checked, "authority must precede device/intent row access")
			require.Same(t, checked, tx.Statement.ConnPool)
		}
	}))
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove("authority_order") })
	_, err = newDeviceOperationIntentStore(f.db, checker).Dispatch(ctx, id, 1)
	require.NoError(t, err)
	require.NotNil(t, checked)
}

func TestDeviceIntentObserverCannotDispatch(t *testing.T) {
	f, store := newIntentFixture(t)
	ctx, id := context.Background(), intentIdentity(1)
	_, err := store.Reserve(ctx, id)
	require.NoError(t, err)
	out, err := NewDeviceOperationIntentStore(f.db).Dispatch(ctx, id, 1)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	require.Empty(t, out.OperationID)
	rows, err := store.ListUnsettled(ctx, 1, id.DeviceCode, 2, "", 1)
	require.NoError(t, err)
	require.Equal(t, IntentReserved, rows[0].State)
}

func TestDeviceIntentObserverCannotDispatchSIPOrRTP(t *testing.T) {
	for _, protocol := range []string{"sip", "rtp"} {
		t.Run(protocol, func(t *testing.T) {
			f, store, id := newSIPStepFixture(t)
			ctx := context.Background()
			observer := NewDeviceOperationIntentStore(f.db)
			if protocol == "sip" {
				_, err := store.AddSIPInviteStep(ctx, id, 2, sipStepIdentity(1))
				require.NoError(t, err)
				out, err := observer.DispatchSIPInviteStep(ctx, id, 3, sipStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out.Intent.OperationID)
			} else {
				_, err := store.AddRTPResourceStep(ctx, id, 2, rtpStepIdentity(1))
				require.NoError(t, err)
				out, err := observer.DispatchRTPResourceStep(ctx, id, 3, rtpStepIdentity(1).StepID)
				require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
				require.Empty(t, out.Intent.OperationID)
			}
		})
	}
}

func TestDeviceIntentObserverCannotCreateSIPCleanupTicket(t *testing.T) {
	f, _, id := sipCleanupFixture(t)
	out, ticket, err := NewDeviceOperationIntentStore(f.db).PrepareSIPBranchCleanupWork(context.Background(), id, 5, sipCleanupIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	require.Nil(t, ticket)
	require.Empty(t, out.Intent.OperationID)
}

func TestDeviceIntentObserverCannotReserveRTPRecovery(t *testing.T) {
	f, _, id := newRTPStepFixture(t)
	barrier := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	work, err := barrier.ReserveRTPCleanup(context.Background(), NewDeviceOperationIntentStore(f.db), id, rtpStepIdentity(1).StepID)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	require.Nil(t, work)
}

func TestDeviceIntentAuthorityFailureBlocksEveryDispatchBeforeDeviceQuery(t *testing.T) {
	for _, effect := range []string{"parent", "invite", "ack", "cancel", "info", "cleanup-prepare", "cleanup-ack", "cleanup-bye", "rtp", "rtp-work", "rtp-close", "rtp-recovery-prepare", "rtp-recovery-close"} {
		t.Run(effect, func(t *testing.T) {
			ctx := context.Background()
			f, store, id := sipINFOFixture(t)
			var invoke func(*DeviceOperationIntentStore) error
			switch effect {
			case "parent":
				id = intentIdentity(2)
				_, err := store.Reserve(ctx, id)
				require.NoError(t, err)
				invoke = func(s *DeviceOperationIntentStore) error { _, err := s.Dispatch(ctx, id, 1); return err }
			case "invite":
				_, err := store.AddSIPInviteStep(ctx, id, 6, sipStepIdentity(2))
				require.NoError(t, err)
				invoke = func(s *DeviceOperationIntentStore) error {
					_, err := s.DispatchSIPInviteStep(ctx, id, 7, sipStepIdentity(2).StepID)
					return err
				}
			case "ack":
				invoke = func(s *DeviceOperationIntentStore) error {
					_, err := s.DispatchSIPKnownBranchACK(ctx, id, 6, sipKnownBranch())
					return err
				}
			case "cancel":
				invoke = func(s *DeviceOperationIntentStore) error {
					_, err := s.DispatchSIPCancel(ctx, id, 6, sipCancelIdentity())
					return err
				}
			case "info":
				i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
				_, err := store.PrepareSIPINFO(ctx, id, 6, i)
				require.NoError(t, err)
				invoke = func(s *DeviceOperationIntentStore) error {
					_, err := s.DispatchSIPINFO(ctx, id, 7, i.InfoID)
					return err
				}
			case "cleanup-prepare":
				invoke = func(s *DeviceOperationIntentStore) error {
					_, err := s.PrepareSIPBranchCleanup(ctx, id, 6, sipCleanupIdentity(1))
					return err
				}
			case "cleanup-ack", "cleanup-bye":
				_, err := store.PrepareSIPBranchCleanup(ctx, id, 6, sipCleanupIdentity(1))
				require.NoError(t, err)
				if effect == "cleanup-bye" {
					_, err = store.DispatchSIPCleanupACK(ctx, id, 7, sipCleanupIdentity(1).AttemptID)
					require.NoError(t, err)
				}
				invoke = func(s *DeviceOperationIntentStore) error {
					if effect == "cleanup-ack" {
						_, err := s.DispatchSIPCleanupACK(ctx, id, 7, sipCleanupIdentity(1).AttemptID)
						return err
					}
					_, err := s.DispatchSIPCleanupBYE(ctx, id, 8, sipCleanupIdentity(1).AttemptID)
					return err
				}
			default:
				_, err := store.AddRTPResourceStep(ctx, id, 6, rtpStepIdentity(1))
				require.NoError(t, err)
				if effect == "rtp" {
					invoke = func(s *DeviceOperationIntentStore) error {
						_, err := s.DispatchRTPResourceStep(ctx, id, 7, rtpStepIdentity(1).StepID)
						return err
					}
					break
				}
				work, err := store.PrepareRTPResourceWork(ctx, id, rtpStepIdentity(1).StepID)
				require.NoError(t, err)
				if effect == "rtp-work" {
					invoke = func(s *DeviceOperationIntentStore) error {
						work.work.store = s
						_, err := work.Dispatch(ctx, 7)
						return err
					}
					break
				}
				_, err = work.Dispatch(ctx, 7)
				require.NoError(t, err)
				call := func(context.Context, DeviceRTPResourceIdentity) (string, error) {
					t.Error("rejected authority sent network")
					return "close_pending", nil
				}
				if effect == "rtp-close" {
					invoke = func(s *DeviceOperationIntentStore) error {
						work.work.store = s
						_, err := work.CloseResource(ctx, call)
						return err
					}
					break
				}
				require.NoError(t, work.Quiesce(ctx))
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
				recovery, err := b.ReserveRTPCleanup(ctx, store, id, rtpStepIdentity(1).StepID)
				require.NoError(t, err)
				if effect == "rtp-recovery-prepare" {
					invoke = func(s *DeviceOperationIntentStore) error { recovery.work.store = s; return recovery.Prepare(ctx) }
				} else {
					require.NoError(t, recovery.Prepare(ctx))
					invoke = func(s *DeviceOperationIntentStore) error {
						recovery.work.store = s
						_, err := recovery.CloseResource(ctx, call)
						return err
					}
				}
				t.Cleanup(func() { recovery.work.store = store; require.NoError(t, recovery.Quiesce(ctx)) })
			}
			checks := 0
			checker := &intentCheckingAuthority{check: func(tx *gorm.DB) error {
				_, ok := tx.Statement.ConnPool.(*sql.Tx)
				require.True(t, ok)
				checks++
				return errors.New("fixture revoked generation")
			}}
			queries := 0
			name := fmt.Sprintf("authority_deny_%s", effect)
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == "gb_device" {
					queries++
				}
			}))
			err := invoke(newDeviceOperationIntentStore(f.db, checker))
			require.NoError(t, f.db.Callback().Query().Remove(name))
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Equal(t, 1, checks)
			require.Zero(t, queries, "a rejected fence must not acquire device rows")
		})
	}
}

func TestDeviceIntentObserverPreservesLateFactsWithoutAuthority(t *testing.T) {
	f, store, id := sipINFOFixture(t)
	ctx := context.Background()
	i := sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"})
	_, err := store.PrepareSIPINFO(ctx, id, 6, i)
	require.NoError(t, err)
	_, err = store.DispatchSIPINFO(ctx, id, 7, i.InfoID)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	observer := NewDeviceOperationIntentStore(f.db)
	out, err := observer.ObserveSIPINFOResponse(ctx, id, 8, sipINFOResponse(i))
	require.NoError(t, err)
	require.NotNil(t, out.Steps[0].KnownBranch.InfoSteps[0].Response)
	out, err = observer.ObserveSIPINFOQuiesced(ctx, id, 9, i.InfoID)
	require.NoError(t, err)
	require.NotNil(t, out.Steps[0].KnownBranch.InfoSteps[0].LocalQuiescedAt)
}
