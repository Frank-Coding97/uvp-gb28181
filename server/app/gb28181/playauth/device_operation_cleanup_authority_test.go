package playauth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceSIPCleanupRequiresRetiredGeneration(t *testing.T) {
	for _, field := range []string{"invite", "info", "cleanup", "missing-invite"} {
		for _, retired := range []bool{false, true} {
			t.Run(field+"/"+map[bool]string{false: "unknown", true: "retired"}[retired], func(t *testing.T) {
				f, store, id := sipINFOFixture(t)
				ctx := context.Background()
				version := int64(6)
				next := sipCleanupIdentity(1)
				if field == "info" {
					_, err := store.PrepareSIPINFO(ctx, id, version, sipINFOIdentity(t, 1, DeviceSIPINFOCommand{Action: "pause"}))
					require.NoError(t, err)
					version++
					next = sipCleanupIdentity(2)
				} else if field == "cleanup" {
					_, err := store.PrepareSIPBranchCleanup(ctx, id, version, next)
					require.NoError(t, err)
					version++
					next = sipCleanupIdentity(2)
				}
				oldID := strings.Repeat("d", 32)
				_, err := store.mutateSIPStep(ctx, id, version, true, func(out *DeviceSIPInviteSteps, _ time.Time) (bool, error) {
					s := &out.Steps[0]
					switch field {
					case "invite":
						s.OwnerProcessID = oldID
					case "missing-invite":
						s.OwnerProcessID = ""
					case "info":
						s.KnownBranch.InfoSteps[0].OwnerRunID = oldID
					case "cleanup":
						s.KnownBranch.CleanupAttempts[0].OwnerRunID = oldID
					}
					return true, nil
				})
				require.NoError(t, err)
				version++
				before, err := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, err)
				var checked *sql.Tx
				checks := 0
				a := &intentCheckingAuthority{check: func(tx *gorm.DB) error {
					checked = tx.Statement.ConnPool.(*sql.Tx)
					return nil
				}, retired: func(tx *gorm.DB, generation string) error {
					require.NotNil(t, checked)
					require.Same(t, checked, tx.Statement.ConnPool)
					require.Equal(t, oldID, generation)
					checks++
					if !retired {
						return ErrDeviceIntentUnavailable
					}
					return nil
				}}
				s := newDeviceOperationIntentStore(f.db, a)
				_, err = s.PrepareSIPBranchCleanup(ctx, id, version, next)
				after, loadErr := store.LoadSIPInviteSteps(ctx, id)
				require.NoError(t, loadErr)
				if !retired || field == "missing-invite" {
					require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
					require.Equal(t, before, after)
				} else {
					require.NoError(t, err)
					require.Equal(t, before.Steps[0].OwnerProcessID, after.Steps[0].OwnerProcessID)
					require.Equal(t, before.Steps[0].KnownBranch.InfoSteps, after.Steps[0].KnownBranch.InfoSteps)
					oldAttempts := before.Steps[0].KnownBranch.CleanupAttempts
					for i := range oldAttempts {
						require.Equal(t, oldAttempts[i], after.Steps[0].KnownBranch.CleanupAttempts[i])
					}
					require.Equal(t, IntentDispatched, after.Intent.State)
				}
				if field != "missing-invite" {
					require.Equal(t, 1, checks)
				}
			})
		}
	}
}

func TestDeviceRTPCleanupRequiresRetiredGeneration(t *testing.T) {
	for _, field := range []string{"original", "recovery", "missing-original"} {
		for _, retired := range []bool{false, true} {
			t.Run(field+"/"+map[bool]string{false: "unknown", true: "retired"}[retired], func(t *testing.T) {
				f, store, id := newRTPStepFixture(t)
				ctx, identity := context.Background(), rtpStepIdentity(1)
				_, err := store.AddRTPResourceStep(ctx, id, 2, identity)
				require.NoError(t, err)
				_, original, err := store.DispatchRTPResourceWork(ctx, id, 3, identity.StepID)
				require.NoError(t, err)
				if field == "recovery" {
					require.NoError(t, original.Quiesce(ctx))
				}
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				out, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				oldID := strings.Repeat("d", 32)
				_, err = store.mutateRTPFacts(ctx, id, out.Intent.RowVersion, authorizeRTPCleanupDevice, func(out *DeviceRTPResourceSteps, now time.Time) (bool, error) {
					s := &out.Steps[0]
					if field == "recovery" {
						s.Recovery = &DeviceRTPRecovery{Version: 1, Generation: 1, OwnerProcessID: oldID, OwnerRunID: strings.Repeat("e", 32), ReservedAt: now}
					} else if field == "missing-original" {
						s.OwnerProcessID, s.OwnerRunID = "", ""
					} else {
						s.OwnerProcessID = oldID
					}
					return true, nil
				})
				require.NoError(t, err)
				before, err := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, err)
				var checked *sql.Tx
				checks := 0
				a := &intentCheckingAuthority{check: func(tx *gorm.DB) error {
					checked = tx.Statement.ConnPool.(*sql.Tx)
					return nil
				}, retired: func(tx *gorm.DB, generation string) error {
					require.NotNil(t, checked)
					require.Same(t, checked, tx.Statement.ConnPool)
					require.Equal(t, oldID, generation)
					checks++
					if !retired {
						return ErrDeviceIntentUnavailable
					}
					return nil
				}}
				s := newDeviceOperationIntentStore(f.db, a)
				b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
				work, err := b.ReserveRTPCleanup(ctx, s, id, identity.StepID)
				require.NoError(t, err)
				err = work.Prepare(ctx)
				after, loadErr := store.LoadRTPResourceSteps(ctx, id)
				require.NoError(t, loadErr)
				if !retired || field == "missing-original" {
					require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
					require.Equal(t, before, after)
				} else {
					require.NoError(t, err)
					require.Equal(t, before.Steps[0].LocalQuiescedAt, after.Steps[0].LocalQuiescedAt)
					require.Equal(t, before.Steps[0].OwnerProcessID, after.Steps[0].OwnerProcessID)
					require.True(t, after.Steps[0].Recovery.PriorObservationIncomplete)
					require.Nil(t, after.Steps[0].Recovery.LocalQuiescedAt)
					require.Equal(t, IntentDispatched, after.Intent.State)
				}
				if field != "missing-original" {
					require.Equal(t, 1, checks)
				}
				require.NoError(t, work.Quiesce(ctx))
			})
		}
	}
}
