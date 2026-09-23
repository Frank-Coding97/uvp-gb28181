package ptz

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestSchedulerDoesNotDispatchTransferredQueuedCommand(t *testing.T) {
	for _, window := range []string{"before-claim", "after-claim", "unchanged", "writeback-fails", "synchronous", "other-device", "historical", "claim-commit-unknown", "retry-slots", "authority-sealed-after-claim", "synchronous-claim-unknown", "synchronous-reserve-unknown", "alarm", "queue-expired", "queue-expiry-write-fails", "home-reconcile", "home-reconcile-transferred", "home-reconcile-insert-fails", "home-reconcile-link-fails", "home-reconcile-legacy"} {
		t.Run(window, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			f := newSchedulerFixture(t, 1, true)
			require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1").Error)
			require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1").Error)
			require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
			const device = "34020000001320000001"
			const channel = "34020000001320000002"
			require.NoError(t, f.db.Exec("UPDATE gb_device SET device_id=? WHERE id=1", device).Error)
			require.NoError(t, f.db.Exec("UPDATE gb_channel SET device_id=?, channel_id=? WHERE id=1", device, channel).Error)
			require.NoError(t, f.db.AutoMigrate(&playauth.DeviceOperationIntent{}))
			authority := authoritytest.Register(t, f.db, "")
			intents, err := playauth.NewAuthorizedDeviceOperationIntentStore(f.db, authority)
			require.NoError(t, err)
			barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(f.db), authority)
			require.NoError(t, err)
			service, err := NewAuthorizedService(f.db, f.sender, f.clock.Now, intents, barrier)
			require.NoError(t, err)
			f.scheduler = NewScheduler(service, WithSchedulerDispatcher(f.dispatcher))
			gbconfig.RequirePlayAuth()
			target := Target{DeviceID: 1, DeviceCode: device, DeviceEpoch: 1, ChannelID: 1, ChannelCode: channel,
				IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true}
			command := Command{CmdType: manscdp.CmdHomePositionQuery, IdempotencyKey: "epoch-test", ResponseRequired: true, MaxAttempts: 3,
				Build: func(sn int) ([]byte, error) { return []byte("prepared"), nil }}
			if window == "synchronous" || window == "synchronous-claim-unknown" || window == "synchronous-reserve-unknown" {
				command.ResponseRequired = false
			}
			if window == "synchronous-claim-unknown" {
				faultDB := authoritytest.CommitFaultDB(t, f.db, 2, true)
				service.intents, err = playauth.NewAuthorizedDeviceOperationIntentStore(faultDB, authority)
				require.NoError(t, err)
			}
			if window == "synchronous-reserve-unknown" {
				faultDB := authoritytest.CommitFaultDB(t, f.db, 1, true)
				service.intents, err = playauth.NewAuthorizedDeviceOperationIntentStore(faultDB, authority)
				require.NoError(t, err)
			}
			if window == "retry-slots" {
				f.sender.results = []uac.TrackedMessageResult{{Attempted: true}, {Attempted: true}, {Attempted: true, StatusCode: 200}}
			}
			if window == "alarm" {
				command.TargetScope, command.TargetCode = "alarm", "34020000001340000001"
				command.ResponseRequired = false
			}
			if strings.HasPrefix(window, "home-reconcile") {
				require.NoError(t, f.db.AutoMigrate(&gbmodels.GbPTZHomePosition{}))
				command.CmdType, command.Action = manscdp.CmdDeviceControl, "home_position"
				command.Payload = map[string]interface{}{"enabled": false}
			}
			op, err := service.Execute(context.Background(), target, command)
			if strings.HasPrefix(window, "home-reconcile") {
				require.NoError(t, err)
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
				f.dispatcher.Drain()
				if window == "home-reconcile-transferred" {
					require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
				}
				if window == "home-reconcile-legacy" {
					require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Where("id=?", op.ID).Updates(map[string]interface{}{"device_epoch": nil, "device_intent_id": nil}).Error)
					require.NoError(t, service.OnPTZMessage(context.Background(), device, "legacy-response", "1", deviceControlResponseWithHead(manscdp.CmdDeviceControl, op.SN, channel, "OK")))
					got := loadSchedulerOperation(t, f.db, op.ID)
					require.Equal(t, gbmodels.PTZOperationAccepted, got.Status)
					require.Nil(t, got.ReconcileOperationID, "locked runtime cannot derive an unbound child")
					var count int64
					require.NoError(t, f.db.Model(&gbmodels.GbPTZHomePosition{}).Count(&count).Error)
					require.EqualValues(t, 1, count)
					return
				}
				if window == "home-reconcile-insert-fails" || window == "home-reconcile-link-fails" {
					before := loadSchedulerOperation(t, f.db, op.ID)
					trigger := `CREATE TRIGGER reject_reconcile BEFORE INSERT ON gb_ptz_operation WHEN NEW.trigger_operation_id IS NOT NULL BEGIN SELECT RAISE(ABORT, 'injected'); END`
					if window == "home-reconcile-link-fails" {
						trigger = `CREATE TRIGGER reject_reconcile BEFORE UPDATE OF reconcile_operation_id ON gb_ptz_operation BEGIN SELECT RAISE(ABORT, 'injected'); END`
					}
					require.NoError(t, f.db.Exec(trigger).Error)
					require.Error(t, service.OnPTZMessage(context.Background(), device, "home-response", "1", deviceControlResponseWithHead(manscdp.CmdDeviceControl, op.SN, channel, "OK")))
					rolledBack := loadSchedulerOperation(t, f.db, op.ID)
					require.Equal(t, before.Status, rolledBack.Status)
					require.Nil(t, rolledBack.ReconcileOperationID)
					for _, model := range []interface{}{&gbmodels.GbPTZOperation{}, &playauth.DeviceOperationIntent{}} {
						var count int64
						require.NoError(t, f.db.Model(model).Count(&count).Error)
						require.EqualValues(t, 1, count)
					}
					var cached int64
					require.NoError(t, f.db.Model(&gbmodels.GbPTZHomePosition{}).Count(&cached).Error)
					require.Zero(t, cached)
					require.NoError(t, f.db.Exec("DROP TRIGGER reject_reconcile").Error)
				}
				require.NoError(t, service.OnPTZMessage(context.Background(), device, "home-response", "1", deviceControlResponseWithHead(manscdp.CmdDeviceControl, op.SN, channel, "OK")))
				parentOp := loadSchedulerOperation(t, f.db, op.ID)
				require.NotNil(t, parentOp.ReconcileOperationID)
				var child gbmodels.GbPTZOperation
				require.NoError(t, f.db.First(&child, "operation_id=?", *parentOp.ReconcileOperationID).Error)
				require.NotNil(t, child.DeviceIntentID)
				require.NotNil(t, child.DeviceEpoch)
				require.EqualValues(t, 1, *child.DeviceEpoch)
				require.NotEqual(t, *op.DeviceIntentID, *child.DeviceIntentID)
				var childParent playauth.DeviceOperationIntent
				require.NoError(t, f.db.First(&childParent, "operation_id=?", *child.DeviceIntentID).Error)
				if window == "home-reconcile-transferred" {
					require.Equal(t, playauth.IntentCancelled, childParent.State)
					require.Equal(t, gbmodels.PTZOperationRejected, child.Status)
					require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
					f.dispatcher.Drain()
					require.Len(t, f.sender.Calls(), 1, "late ACK must not create a sendable child")
				} else {
					require.Equal(t, playauth.IntentReserved, childParent.State)
				}
				require.NoError(t, service.OnPTZMessage(context.Background(), device, "home-duplicate", "2", deviceControlResponseWithHead(manscdp.CmdDeviceControl, op.SN, channel, "OK")))
				var childCount int64
				require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Where("trigger_operation_id=?", op.OperationID).Count(&childCount).Error)
				require.EqualValues(t, 1, childCount)
				return
			}
			if window == "queue-expired" || window == "queue-expiry-write-fails" {
				require.NoError(t, err)
				f.clock.Set(f.clock.Now().Add(6 * time.Second))
				if window == "queue-expiry-write-fails" {
					require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_expiry BEFORE UPDATE ON gb_ptz_operation BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
					require.Error(t, f.scheduler.RunDue(f.clock.Now()))
					var pending playauth.DeviceOperationIntent
					require.NoError(t, f.db.First(&pending, "operation_id=?", *op.DeviceIntentID).Error)
					require.Equal(t, playauth.IntentReserved, pending.State)
					require.Equal(t, gbmodels.PTZOperationQueued, loadSchedulerOperation(t, f.db, op.ID).Status)
					require.NoError(t, f.db.Exec("DROP TRIGGER reject_expiry").Error)
				}
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
				var parent playauth.DeviceOperationIntent
				require.NoError(t, f.db.First(&parent, "operation_id=?", *op.DeviceIntentID).Error)
				require.Equal(t, playauth.IntentCancelled, parent.State)
				require.Equal(t, gbmodels.PTZOperationRejected, loadSchedulerOperation(t, f.db, op.ID).Status)
				require.Empty(t, f.sender.Calls())
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
				return
			}
			if window == "alarm" {
				require.NoError(t, err)
				require.Equal(t, "alarm", op.TargetScope)
				require.Equal(t, command.TargetCode, op.TargetCode)
				id, err := ptzIntentIdentity(op)
				require.NoError(t, err)
				require.Equal(t, "channel", id.TargetScope)
				require.Equal(t, channel, id.TargetCode)
				require.Len(t, f.sender.Calls(), 1)
				return
			}
			if window == "synchronous-reserve-unknown" {
				require.NoError(t, err)
				require.Equal(t, gbmodels.PTZOperationSent, op.Status, "confirmed reservation needs a fresh claim, not a silent queued return")
				require.Len(t, f.sender.Calls(), 1)
				return
			}
			if window == "synchronous-claim-unknown" {
				require.Error(t, err)
				require.NotZero(t, op.ID)
				require.Empty(t, f.sender.Calls())
				f.clock.Set(f.clock.Now().Add(20 * time.Second))
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
				f.dispatcher.Drain()
				got := loadSchedulerOperation(t, f.db, op.ID)
				require.Equal(t, gbmodels.PTZOperationUnknown, got.Status)
				require.Equal(t, "DISPATCH_NOT_INVOKED", got.ErrorCode)
				require.Empty(t, f.sender.Calls(), "one-way recovery must never resend")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, op.DeviceEpoch)
			require.NotNil(t, op.DeviceIntentID)
			replayed, err := service.Execute(context.Background(), target, command)
			require.NoError(t, err)
			require.Equal(t, op.ID, replayed.ID)
			changed := target
			changed.DeviceEpoch = 2
			_, err = service.Execute(context.Background(), changed, command)
			require.Error(t, err, "an idempotency key cannot upgrade the original epoch")
			if window == "historical" {
				require.NoError(t, f.db.Exec("UPDATE gb_ptz_operation SET device_epoch=NULL, device_intent_id=NULL WHERE id=?", op.ID).Error)
			}
			if window == "other-device" {
				const otherDevice = "34020000001320000003"
				const otherChannel = "34020000001320000004"
				require.NoError(t, f.db.Create(&gbmodels.GbDevice{DeviceID: otherDevice, IP: "192.0.2.11", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}).Error)
				require.NoError(t, f.db.Create(&gbmodels.GbChannel{DeviceID: otherDevice, ChannelID: otherChannel, Status: gbmodels.ChannelStatusOnline}).Error)
				other := target
				other.DeviceID, other.ChannelID = 2, 2
				other.DeviceCode, other.ChannelCode = otherDevice, otherChannel
				_, err := service.Execute(context.Background(), other, command)
				require.NoError(t, err)
			}
			if window == "before-claim" || window == "other-device" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
			}
			if window == "claim-commit-unknown" {
				faultDB := authoritytest.CommitFaultDB(t, f.db, 1, true)
				service.intents, err = playauth.NewAuthorizedDeviceOperationIntentStore(faultDB, authority)
				require.NoError(t, err)
			}
			err = f.scheduler.RunDue(f.clock.Now())
			if window == "claim-commit-unknown" {
				require.Error(t, err)
				f.dispatcher.Drain()
				require.Empty(t, f.sender.Calls())
				attempts := loadSchedulerAttempts(t, f.db, op.ID)
				require.Len(t, attempts, 1)
				require.Equal(t, gbmodels.PTZOperationAttemptDispatching, attempts[0].Status)
				service.intents = intents
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_unissued BEFORE UPDATE OF local_quiesced_at ON gb_ptz_operation_attempt BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
				require.Error(t, f.scheduler.RunDue(f.clock.Now()), "failed reconciliation must retain its ticket")
				require.Error(t, f.scheduler.FlushResults(context.Background()))
				require.Len(t, f.scheduler.pendingClaims, 1)
				require.Empty(t, f.sender.Calls())
				require.NoError(t, f.db.Exec("DROP TRIGGER reject_unissued").Error)
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
				f.dispatcher.Drain()
				require.Empty(t, f.sender.Calls(), "reading a committed row cannot restore dispatch permission")
				f.clock.Set(f.clock.Now().Add(2 * time.Second))
				require.NoError(t, f.scheduler.RunDue(f.clock.Now()), "a never-invoked attempt must not strand the command after its lease")
				f.dispatcher.Drain()
				require.Len(t, f.sender.Calls(), 1, "only the newly committed retry may send")
				attempts = loadSchedulerAttempts(t, f.db, op.ID)
				require.Len(t, attempts, 2)
				require.Equal(t, "DISPATCH_NOT_INVOKED", attempts[0].ErrorCode)
				require.NotNil(t, attempts[0].LocalQuiescedAt)
				return
			}
			if window == "before-claim" || window == "other-device" || window == "historical" {
				require.NoError(t, err, "a revoked command must not abort the whole scheduling batch")
				got := loadSchedulerOperation(t, f.db, op.ID)
				require.Equal(t, gbmodels.PTZOperationRejected, got.Status)
				require.Equal(t, "DEVICE_EPOCH_REVOKED", got.ErrorCode)
			} else {
				require.NoError(t, err)
			}
			if window == "after-claim" {
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
			}
			if window == "authority-sealed-after-claim" {
				authority.Seal()
			}
			if window == "writeback-fails" {
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_exit BEFORE UPDATE OF local_quiesced_at ON gb_ptz_operation_attempt BEGIN SELECT RAISE(ABORT, 'injected'); END`).Error)
			}
			f.dispatcher.Drain()
			if window == "retry-slots" {
				start := f.clock.Now()
				for index, offset := range []time.Duration{time.Second, 6 * time.Second} {
					f.clock.Set(start.Add(offset - time.Millisecond))
					require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
					f.dispatcher.Drain()
					require.Len(t, f.sender.Calls(), index+1)
					f.clock.Set(start.Add(offset))
					require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
					f.dispatcher.Drain()
					require.Len(t, f.sender.Calls(), index+2)
				}
				attempts := loadSchedulerAttempts(t, f.db, op.ID)
				require.Len(t, attempts, 3)
				for _, a := range attempts {
					require.NotNil(t, a.LocalQuiescedAt)
				}
				require.NotEqual(t, *attempts[0].OwnerRunID, *attempts[1].OwnerRunID)
				require.Equal(t, gbmodels.PTZOperationSent, loadSchedulerOperation(t, f.db, op.ID).Status)
				return
			}
			if window == "writeback-fails" {
				waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				err := barrier.WaitBefore(waitCtx, 1, 2)
				cancel()
				require.Error(t, err, "failed writeback must retain the original lease")
				f.clock.Set(f.clock.Now().Add(7 * time.Second))
				require.NoError(t, f.db.Exec("DROP TRIGGER reject_exit").Error)
				// A fact-only flush never sends. RunDue may separately perform
				// the existing application-query retry after a SIP 200.
				require.NoError(t, f.scheduler.FlushResults(context.Background()))
				f.dispatcher.Drain()
				waitCtx, cancel = context.WithTimeout(context.Background(), time.Second)
				require.NoError(t, barrier.WaitBefore(waitCtx, 1, 2))
				cancel()
			}
			if window == "other-device" {
				require.Len(t, f.sender.Calls(), 1)
				require.Contains(t, f.sender.Calls()[0].body, "34020000001320000004")
				require.Empty(t, loadSchedulerAttempts(t, f.db, op.ID), "revoked command must have no dispatch attempt")
			} else if window == "unchanged" || window == "writeback-fails" || window == "synchronous" {
				require.Len(t, f.sender.Calls(), 1)
				var attempt gbmodels.GbPTZOperationAttempt
				require.NoError(t, f.db.First(&attempt).Error)
				require.NotNil(t, attempt.LocalQuiescedAt, "successful result and sender exit must commit together")
			} else {
				require.Empty(t, f.sender.Calls(), "an old queued command must not acquire the new owner's epoch")
			}
		})
	}
}
