package ptz

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

const ptzRecoveryChildEnv = "UVP_PTZ_RECOVERY_CHILD"

func TestPTZRecoveryOriginalProcess(t *testing.T) {
	dir := os.Getenv(ptzRecoveryChildEnv)
	if dir == "" {
		return
	}
	db := authoritytest.OpenSQLite(t, filepath.Join(dir, "ptz.sqlite"))
	authority := authoritytest.Register(t, db, dir)
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	id := playauth.DeviceOperationIntentIdentity{OperationID: "90000000000000000000000000000001", DevicePK: 1, DeviceCode: "34020000001320000001",
		DeviceEpoch: 1, TargetScope: "channel", TargetPK: 1, TargetCode: "34020000001320000002", Kind: "ptz"}
	op, err := store.ReservePTZOperation(context.Background(), id, gbmodels.GbPTZOperation{
		OperationID: "retired-one-way", IdempotencyKey: "retired-one-way", DeviceID: 1, DeviceCode: id.DeviceCode,
		ChannelID: 1, ChannelCode: id.TargetCode, TargetScope: "channel", TargetCode: id.TargetCode,
		CmdType: "DeviceControl", Action: "guard_set", Status: gbmodels.PTZOperationQueued, MaxAttempts: 1, SN: 1, CreatedAt: time.Now()})
	require.NoError(t, err)
	// A controlled future clock makes this explicitly independent of lease
	// expiry. Only genuine process retirement can authorize reconciliation.
	_, err = store.ClaimPTZAttempt(context.Background(), id, op.ID, 0, time.Now().Add(time.Hour))
	require.NoError(t, err)
	fmt.Fprintln(os.Stdout, "PTZ_READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestSchedulerRetiredProcessNeverReplaysOneWay(t *testing.T) {
	testSchedulerRetiredProcess(t, false, false)
}

func TestSchedulerRetiredProcessCommitReceiptLoss(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%t", committed), func(t *testing.T) {
			testSchedulerRetiredProcess(t, true, committed)
		})
	}
}

func testSchedulerRetiredProcess(t *testing.T, injectFault, committed bool) {
	t.Helper()
	if !authoritytest.InProcess(t) {
		return
	}
	dir := authoritytest.StateDirectory(t)
	db := authoritytest.OpenSQLite(t, filepath.Join(dir, "ptz.sqlite"))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &playauth.DeviceOperationIntent{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000001320000001"}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 1, DeviceID: "34020000001320000001", ChannelID: "34020000001320000002"}).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPTZRecoveryOriginalProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), ptzRecoveryChildEnv+"="+dir)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	waited := false
	t.Cleanup(func() {
		_ = input.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(output).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "PTZ_READY\n", line)
	lock, err := processauthority.AcquireLocalLock(dir)
	require.ErrorIs(t, err, processauthority.ErrLocalAuthorityBusy)
	require.Nil(t, lock)
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	waited = true
	authority := authoritytest.Register(t, db, dir)
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
	require.NoError(t, err)
	sender := &schedulerFakeSender{}
	if injectFault {
		faultDB := authoritytest.CommitFaultDB(t, db, 1, committed)
		faultStore, err := playauth.NewAuthorizedDeviceOperationIntentStore(faultDB, authority)
		require.NoError(t, err)
		failed, err := NewAuthorizedService(faultDB, sender, time.Now, faultStore, barrier)
		require.Error(t, err, "unknown commit must not publish the PTZ runtime")
		require.Nil(t, failed)
		require.Empty(t, sender.Calls())
		var attempt gbmodels.GbPTZOperationAttempt
		var operation gbmodels.GbPTZOperation
		require.NoError(t, db.First(&attempt).Error)
		require.NoError(t, db.First(&operation, attempt.OperationID).Error)
		if committed {
			require.NotNil(t, attempt.RetiredAt)
			require.NotNil(t, attempt.RetiredByProcessID)
			require.Equal(t, gbmodels.PTZOperationUnknown, operation.Status)
			require.Nil(t, operation.NextAttemptAt)
		} else {
			require.Nil(t, attempt.RetiredAt)
			require.Nil(t, attempt.RetiredByProcessID)
			require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
		}
		require.Nil(t, attempt.LocalQuiescedAt)
	}
	service, err := NewAuthorizedService(db, sender, time.Now, store, barrier)
	require.NoError(t, err)
	scheduler := NewScheduler(service)
	t.Cleanup(func() {
		service.Retire()
		scheduler.Stop()
		require.NoError(t, service.FlushResults(context.Background()))
	})
	var before gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.First(&before).Error)
	require.True(t, before.LeaseUntil.After(time.Now()))
	require.NoError(t, scheduler.RunDue(time.Now()))
	var op gbmodels.GbPTZOperation
	require.NoError(t, db.First(&op, "operation_id=?", "retired-one-way").Error)
	require.Equal(t, gbmodels.PTZOperationUnknown, op.Status, "retired one-way may have executed; never leave it queued for replay")
	require.Empty(t, sender.Calls())
	var after gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.First(&after, before.ID).Error)
	require.Equal(t, before.OwnerProcessID, after.OwnerProcessID)
	require.Nil(t, after.LocalQuiescedAt, "retired generation is not a local sender callback")
	require.NotNil(t, after.RetiredAt)
	require.NotNil(t, after.RetiredByProcessID)
	require.Equal(t, authority.GenerationID(), *after.RetiredByProcessID)
	require.Equal(t, playauth.PTZOwnerProcessRetired, after.ErrorCode)
	require.NoError(t, store.RetirePTZAttempt(context.Background(), before), "same snapshot recovery is idempotent")
}
