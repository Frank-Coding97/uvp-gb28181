package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	cascademodel "uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
)

type cascadeTestLease struct {
	ctx   context.Context
	epoch int64
}

func (l cascadeTestLease) Context() context.Context { return l.ctx }
func (l cascadeTestLease) OperationEpoch() int64    { return l.epoch }
func (l cascadeTestLease) Release()                 {}

type cascadeTestBarrier struct{}

func (cascadeTestBarrier) BeginEpoch(ctx context.Context, _ string, epoch int64) (playauth.DeviceOperationLease, error) {
	return cascadeTestLease{ctx, epoch}, nil
}

func cascadeUpgradeFixture(t *testing.T) (*Service, *fakeSender, CascadeBinding) {
	t.Helper()
	now := time.Now()
	sender := &fakeSender{}
	service, db := newUpgradeService(t, sender, &now)
	service.operations = cascadeTestBarrier{}
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannel{}, &cascademodel.GbCascadePlatform{}, &cascademodel.GbCascadeDeviceProjection{}, &cascademodel.GbCascadeChannelProjection{}, &cascademodel.GbCascadeBusinessNode{}, &cascademodel.GbCascadeBusinessBinding{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Exec("UPDATE gb_device SET effective_version='2022'").Error)
	platform := cascademodel.GbCascadePlatform{ID: 1, Name: "upstream", UpstreamServerID: "34020000002000000001", LocalDeviceID: "34020000002000000009", Host: "192.0.2.1", Port: 5060, Enabled: true, UpgradeEnabled: true, ConfigRevision: 2, ProjectionRevision: 3, EffectiveVersion: "2022"}
	require.NoError(t, db.Create(&platform).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 2, DeviceID: upgradeTarget().DeviceCode, ChannelID: "34020000001320000002"}).Error)
	require.NoError(t, db.Create(&cascademodel.GbCascadeDeviceProjection{ID: 1, PlatformID: 1, SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011", Active: true}).Error)
	require.NoError(t, db.Create(&cascademodel.GbCascadeChannelProjection{ID: 1, PlatformID: 1, DeviceProjectionID: 1, SourceChannelID: 2, PublishedChannelID: "34020000001320000012", Active: true}).Error)
	binding := CascadeBinding{PlatformID: 1, ConfigRevision: 2, ProjectionRevision: 3, PublishedID: "34020000001320000012", SourceChannelID: 2, SourceChannelCode: "34020000001320000002", SourceDeviceEpoch: 1, UpstreamSN: 97, UpstreamSessionID: strings.Repeat("a", 32), RequestKey: "original-transaction"}
	return service, sender, binding
}

func TestCascadeUpgradeBindingIsDurableBeforeSendAndRestores(t *testing.T) {
	service, _, binding := cascadeUpgradeFixture(t)
	service.sender = &callbackSender{callback: func() {
		var row gbmodels.GbDeviceFirmwareUpgrade
		require.NoError(t, service.db.Take(&row).Error)
		var persisted CascadeBinding
		require.NoError(t, json.Unmarshal([]byte(row.CascadeBinding), &persisted))
		require.Equal(t, binding, persisted)
	}}
	request := upgradeRequest("cascade-original")
	request.Cascade = &binding
	op, _, err := service.Execute(context.Background(), upgradeTarget(), request)
	require.NoError(t, err)
	require.NotEqual(t, binding.UpstreamSessionID, op.SessionID)
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "device-business", "1", deviceControlResponse(op.SN, "OK")))
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "device-final", "2", upgradeResult(8, op.SessionID, "OK", "v2.3.4", "")))
	restarted, err := NewService(service.db, &fakeSender{}, &fakeSN{}, time.Now)
	require.NoError(t, err)
	restarted.operations = cascadeTestBarrier{}
	pending, err := restarted.PendingCascadeResults(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "OK", pending[0].CascadeBusinessResult)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, pending[0].Status)
	replay, duplicate, err := restarted.Execute(context.Background(), upgradeTarget(), request)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, op.OperationID, replay.OperationID)
}

func TestCascadeUpgradeRequiresCurrentGrantAndSourceEpoch(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE gb_cascade_platform SET upgrade_enabled=0",
		"UPDATE gb_cascade_platform SET config_revision=config_revision+1",
		"UPDATE gb_cascade_channel_projection SET active=0",
		"UPDATE gb_channel SET owner_dept_id=9",
		"UPDATE gb_device SET access_epoch=2",
		"UPDATE gb_device SET cleanup_completed_epoch=0",
		"UPDATE gb_device SET access_epoch=NULL",
	} {
		t.Run(mutation, func(t *testing.T) {
			service, sender, binding := cascadeUpgradeFixture(t)
			require.NoError(t, service.db.Exec(mutation).Error)
			request := upgradeRequest("revoked")
			request.Cascade = &binding
			_, _, err := service.Execute(context.Background(), upgradeTarget(), request)
			require.ErrorIs(t, err, ErrCascadeUnauthorized)
			require.Empty(t, sender.bodies)
		})
	}
}

func TestCascadeUpgradeReplayCannotChangeBindingOrReuseSession(t *testing.T) {
	service, sender, binding := cascadeUpgradeFixture(t)
	request := upgradeRequest("one")
	request.Cascade = &binding
	op, _, err := service.Execute(context.Background(), upgradeTarget(), request)
	require.NoError(t, err)
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "final", "2", upgradeResult(8, op.SessionID, "OK", "v2.3.4", "")))
	changed := binding
	changed.UpstreamSN++
	request.Cascade = &changed
	_, _, err = service.Execute(context.Background(), upgradeTarget(), request)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	changed = binding
	changed.RequestKey = "new-transaction"
	request.Cascade, request.IdempotencyKey = &changed, "two"
	_, _, err = service.Execute(context.Background(), upgradeTarget(), request)
	require.Error(t, err, "one upstream SessionID cannot cause a second firmware command")
	require.Len(t, sender.bodies, 1)
}

func TestCascadeUpgradeRevocationBetweenCommitAndSend(t *testing.T) {
	service, sender, binding := cascadeUpgradeFixture(t)
	require.NoError(t, service.db.Callback().Create().After("gorm:create").Register("revoke_cascade_upgrade", func(tx *gorm.DB) {
		if tx.Statement.Table == "gb_device_firmware_upgrade" {
			require.NoError(t, tx.Exec("UPDATE gb_cascade_platform SET upgrade_enabled=0").Error)
		}
	}))
	request := upgradeRequest("revoked-after-commit")
	request.Cascade = &binding
	_, _, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.ErrorIs(t, err, ErrCascadeUnauthorized)
	require.Empty(t, sender.bodies)
	_, _, err = service.Execute(t.Context(), upgradeTarget(), request)
	require.ErrorIs(t, err, ErrCascadeUnauthorized, "idempotency fallback must not bypass revocation")
}

func TestCascadeUpgradeSameSessionAcrossUpstreamsIsIsolated(t *testing.T) {
	service, sender, binding := cascadeUpgradeFixture(t)
	request := upgradeRequest("first-upstream")
	request.Cascade = &binding
	first, _, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.NoError(t, err)
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "first-final", "2", upgradeResult(8, first.SessionID, "OK", "v2.3.4", "")))
	var platform cascademodel.GbCascadePlatform
	require.NoError(t, service.db.Take(&platform).Error)
	platform.ID, platform.Name, platform.UpstreamServerID = 2, "second", "34020000002000000002"
	require.NoError(t, service.db.Create(&platform).Error)
	require.NoError(t, service.db.Create(&cascademodel.GbCascadeDeviceProjection{ID: 2, PlatformID: 2, SourceDeviceID: 1, PublishedDeviceID: "34020000001320000021", Active: true}).Error)
	require.NoError(t, service.db.Create(&cascademodel.GbCascadeChannelProjection{ID: 2, PlatformID: 2, DeviceProjectionID: 2, SourceChannelID: 2, PublishedChannelID: "34020000001320000022", Active: true}).Error)
	secondBinding := binding
	secondBinding.PlatformID, secondBinding.PublishedID = 2, "34020000001320000022"
	request.Cascade, request.IdempotencyKey = &secondBinding, "second-upstream"
	second, _, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, second.SessionID)
	require.Len(t, sender.bodies, 2)
	require.NoError(t, service.db.Exec("UPDATE gb_device SET access_epoch=2,cleanup_completed_epoch=2").Error)
	consumed, status, err := service.OnUpgradeResultMessage(t.Context(), upgradeTarget().DeviceCode, "stale-final", "3", upgradeResult(8, second.SessionID, "OK", "v2.3.4", ""))
	require.True(t, consumed)
	require.Equal(t, 403, status)
	require.ErrorIs(t, err, ErrCascadeUnauthorized)
	observed, err := service.Get(t.Context(), second.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSent, observed.Status)
}

func TestCascadeUpgradeBusyRejectionIsDurableAndCannotExecuteOnReplay(t *testing.T) {
	service, sender, binding := cascadeUpgradeFixture(t)
	request := upgradeRequest("running")
	request.Cascade = &binding
	first, _, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.NoError(t, err)
	rejectedBinding := binding
	rejectedBinding.RequestKey, rejectedBinding.UpstreamSessionID = "busy-transaction", strings.Repeat("b", 32)
	request.IdempotencyKey, request.Cascade = "busy", &rejectedBinding
	rejected, _, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.NoError(t, err, "an authorized request rejected before dispatch needs a durable business ERROR")
	require.Equal(t, gbmodels.FirmwareUpgradeRejected, rejected.Status)
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "final", "2", upgradeResult(8, first.SessionID, "OK", "v2.3.4", "")))
	replay, duplicate, err := service.Execute(t.Context(), upgradeTarget(), request)
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, rejected.OperationID, replay.OperationID)
	require.Len(t, sender.bodies, 1)
	var row gbmodels.GbDeviceFirmwareUpgrade
	require.NoError(t, service.db.Where("operation_id=?", rejected.OperationID).Take(&row).Error)
	require.Equal(t, "ERROR", row.CascadeBusinessResult)
}

func TestCascadeUpgradeLateBusinessPreservesFinalAndFirstBusinessResult(t *testing.T) {
	for _, finalResult := range []string{"OK", "ERROR"} {
		for _, businessResult := range []string{"OK", "ERROR"} {
			t.Run(finalResult+"/"+businessResult, func(t *testing.T) {
				service, _, binding := cascadeUpgradeFixture(t)
				request := upgradeRequest("final-first")
				request.Cascade = &binding
				op, _, err := service.Execute(t.Context(), upgradeTarget(), request)
				require.NoError(t, err)
				reason := ""
				if finalResult == "ERROR" {
					reason = "02"
				}
				require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "final", "2", upgradeResult(8, op.SessionID, finalResult, "v2.3.4", reason)))
				var before gbmodels.GbDeviceFirmwareUpgrade
				require.NoError(t, service.db.Take(&before).Error)
				require.NoError(t, service.MarkCascadeDelivered(t.Context(), before, true))
				require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "business", "3", deviceControlResponse(op.SN, businessResult)))
				pending, err := service.PendingCascadeResults(t.Context(), 100)
				require.NoError(t, err)
				require.Len(t, pending, 1, "late business response must remain deliverable after final delivery")
				require.Equal(t, businessResult, pending[0].CascadeBusinessResult)
				require.Equal(t, before.Status, pending[0].Status)
				require.Equal(t, before.DeviceResult, pending[0].DeviceResult)
				require.Equal(t, before.ResponseCallID, pending[0].ResponseCallID)
				require.Equal(t, before.CompletedAt, pending[0].CompletedAt)
				require.Equal(t, before.CurrentFirmware, pending[0].CurrentFirmware)
				require.NoError(t, service.MarkCascadeDelivered(t.Context(), pending[0], false))
				changed := "ERROR"
				if businessResult == "ERROR" {
					changed = "OK"
				}
				require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "changed-replay", "4", deviceControlResponse(op.SN, changed)))
				var after gbmodels.GbDeviceFirmwareUpgrade
				require.NoError(t, service.db.Take(&after).Error)
				require.Equal(t, businessResult, after.CascadeBusinessResult, "duplicate cannot change an already delivered milestone")
				require.Equal(t, before.DeviceResult, after.DeviceResult)
			})
		}
	}
}

func TestCascadeUpgradeProtocolDowngradeBeforeDispatch(t *testing.T) {
	for _, timing := range []string{"before-create", "after-create"} {
		t.Run(timing, func(t *testing.T) {
			service, sender, binding := cascadeUpgradeFixture(t)
			if timing == "before-create" {
				require.NoError(t, service.db.Exec("UPDATE gb_device SET effective_version='2016'").Error)
			} else {
				require.NoError(t, service.db.Callback().Create().After("gorm:create").Register("downgrade_upgrade_source", func(tx *gorm.DB) {
					if tx.Statement.Table == "gb_device_firmware_upgrade" {
						require.NoError(t, tx.Exec("UPDATE gb_device SET effective_version='2016'").Error)
					}
				}))
			}
			request := upgradeRequest("version-downgrade")
			request.Cascade = &binding
			_, _, err := service.Execute(t.Context(), upgradeTarget(), request)
			require.ErrorIs(t, err, ErrCascadeUnauthorized)
			require.Empty(t, sender.bodies)
		})
	}
}

func TestCascadeUpgradeAcceptanceFailureNeverSendsOnReplay(t *testing.T) {
	service, sender, binding := cascadeUpgradeFixture(t)
	request := upgradeRequest("acceptance-failed")
	request.Cascade = &binding
	ackErr := errors.New("SIP response transport failed")
	op, _, err := service.ExecuteCascade(t.Context(), upgradeTarget(), request, func() error { return ackErr })
	require.ErrorIs(t, err, ackErr)
	require.Empty(t, sender.bodies)
	accepted := 0
	replay, duplicate, err := service.ExecuteCascade(t.Context(), upgradeTarget(), request, func() error { accepted++; return nil })
	require.NoError(t, err)
	require.True(t, duplicate)
	require.Equal(t, op.OperationID, replay.OperationID)
	require.Equal(t, gbmodels.FirmwareUpgradeRejected, replay.Status)
	require.Equal(t, 1, accepted)
	require.Empty(t, sender.bodies)
}
