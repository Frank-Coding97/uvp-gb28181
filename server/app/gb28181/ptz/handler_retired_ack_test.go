package ptz

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// A HomePosition ACK that arrives after the original process was retired must
// still record the observed device fact, and a complete retirement certificate
// reopens the reconcile path even past the transport deadline.
func TestHandlerLateHomePositionACKAfterRetirementRecordsFactAndReconcile(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	dir := authoritytest.StateDirectory(t)
	db := authoritytest.OpenSQLite(t, filepath.Join(dir, "retired-ack.sqlite"))
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbPTZHomePosition{}, &gbmodels.GbPTZState{}, &gbmodels.GbPTZPreset{},
		&gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbDeviceControlState{}, &gbmodels.GbChannel{},
		&gbmodels.GbDevice{}, &playauth.DeviceOperationIntent{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT DEFAULT 1").Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "D"}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{ID: 1, DeviceID: "D", ChannelID: "C"}).Error)

	authority := authoritytest.Register(t, db, dir)
	store, err := playauth.NewAuthorizedDeviceOperationIntentStore(db, authority)
	require.NoError(t, err)
	barrier, err := playauth.NewAuthorizedDeviceOperationBarrier(playauth.NewDeviceSecurityStore(db), authority)
	require.NoError(t, err)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	service, err := NewAuthorizedService(db, &fakeTrackedSender{}, func() time.Time { return now }, store, barrier)
	require.NoError(t, err)

	control := createHandlerHomeControl(t, service, "late-retired", false, nil, nil)
	// The original process died; recovery already converged operation and
	// attempt to the retirement terminal state and the transport deadline has
	// long expired. The certificate is complete.
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("id = ?", control.ID).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationUnknown, "error_code": playauth.PTZOwnerProcessRetired,
		"transport_deadline_at": now.Add(-time.Hour), "attempt": 1,
	}).Error)
	ownerProcess, ownerRun, retiredBy := strings.Repeat("f", 32), strings.Repeat("e", 32), strings.Repeat("d", 32)
	retiredAt := now
	attempt := gbmodels.GbPTZOperationAttempt{
		OperationID: control.ID, AttemptNo: 1, SN: control.SN, CallID: "late-ack", CSeq: "9",
		Status: gbmodels.PTZOperationAttemptUnknown, ErrorCode: playauth.PTZOwnerProcessRetired,
		OwnerProcessID: &ownerProcess, OwnerRunID: &ownerRun,
		RetiredByProcessID: &retiredBy, RetiredAt: &retiredAt,
		StartedAt: now.Add(-time.Hour), LeaseUntil: now.Add(-time.Hour + 15*time.Second), CreatedAt: now,
	}
	require.NoError(t, db.Create(&attempt).Error)

	require.NoError(t, service.OnPTZMessage(t.Context(), "D", "late-ack", "9", deviceControlResponse(control.SN, "OK")))

	stored := storedOperation(t, db, control.OperationID)
	require.Equal(t, gbmodels.PTZOperationAccepted, stored.Status, "a late ACK after retirement must still record the device fact")
	require.NotNil(t, stored.ReconcileOperationID, "a complete retirement certificate reopens the reconcile path")
}
