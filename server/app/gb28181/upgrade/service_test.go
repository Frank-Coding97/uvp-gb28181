package upgrade

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakeSender struct {
	mu      sync.Mutex
	results []uac.TrackedMessageResult
	bodies  [][]byte
}

func (s *fakeSender) SendMessageTracked(_ context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	if len(s.results) == 0 {
		return uac.TrackedMessageResult{CallID: "call-1", CSeq: "1", StatusCode: http.StatusOK, Attempted: true}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

type fakeSN struct{ next int }

func (s *fakeSN) NextSN() int { s.next++; return s.next }

func (s *fakeSN) EnsureSNFloor(floor int) error {
	if floor > s.next {
		s.next = floor
	}
	return nil
}

type callbackSender struct {
	callback func()
}

func (s *callbackSender) SendMessageTracked(_ context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	if s.callback != nil {
		s.callback()
	}
	return uac.TrackedMessageResult{CallID: "callback-call", CSeq: "1", StatusCode: http.StatusOK, Attempted: true}, nil
}

type blockingSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingSender) SendMessageTracked(_ context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	close(s.started)
	<-s.release
	return uac.TrackedMessageResult{CallID: "blocking-call", CSeq: "1", StatusCode: http.StatusOK, Attempted: true}, nil
}

func newUpgradeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000001320000001", IP: "127.0.0.1", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}).Error)
	return db
}

func newUpgradeService(t *testing.T, sender TrackedSender, now *time.Time) (*Service, *gorm.DB) {
	t.Helper()
	db := newUpgradeDB(t)
	service, err := NewService(db, sender, &fakeSN{next: 100}, func() time.Time { return *now })
	require.NoError(t, err)
	return service, db
}

func upgradeTarget() Target {
	return Target{
		DeviceID: 1, DeviceCode: "34020000001320000001", IP: "127.0.0.1", Port: 5060,
		Transport: "UDP", DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2022),
	}
}

func upgradeRequest(key string) Request {
	return Request{Confirmed: true, IdempotencyKey: key, Firmware: "v2.3.4", FileURL: "https://fixture.example/v2.3.4.bin?token=secret", Manufacturer: "UVP", ActorID: 7, ActorDeptID: 8}
}

func deviceControlResponse(sn int, result string) []byte {
	return []byte(fmt.Sprintf(`<Response><CmdType>DeviceControl</CmdType><SN>%d</SN><DeviceID>34020000001320000001</DeviceID><Result>%s</Result></Response>`, sn, result))
}

func upgradeResult(sn int, sessionID, result, firmware, reason string) []byte {
	return []byte(fmt.Sprintf(`<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>%d</SN><DeviceID>34020000001320000001</DeviceID><SessionID>%s</SessionID><UpgradeResult>%s</UpgradeResult><Firmware>%s</Firmware><UpgradeFailedReason>%s</UpgradeFailedReason></Notify>`, sn, sessionID, result, firmware, reason))
}

func TestExecuteBuildsAndTracksDeviceUpgrade(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	sender := &fakeSender{}
	service, db := newUpgradeService(t, sender, &now)
	op, deduplicated, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-1"))
	require.NoError(t, err)
	require.False(t, deduplicated)
	require.Equal(t, gbmodels.FirmwareUpgradeSent, op.Status)
	require.Equal(t, http.StatusOK, op.SIPStatus)
	require.Len(t, sender.bodies, 1)
	require.Contains(t, string(sender.bodies[0]), "<CmdType>DeviceControl</CmdType>")
	require.Contains(t, string(sender.bodies[0]), "<DeviceUpgrade>")
	require.NotEmpty(t, op.SessionID)
	require.NotEmpty(t, op.DeadlineAt)

	var stored gbmodels.GbDeviceFirmwareUpgrade
	require.NoError(t, db.First(&stored, "operation_id = ?", op.OperationID).Error)
	require.Equal(t, "https://fixture.example/v2.3.4.bin?token=secret", stored.FileURL)
}

func TestExecuteIdempotencyAndDeviceConcurrency(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, _ := newUpgradeService(t, &fakeSender{}, &now)
	first, deduplicated, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-1"))
	require.NoError(t, err)
	require.False(t, deduplicated)
	reused, deduplicated, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-1"))
	require.NoError(t, err)
	require.True(t, deduplicated)
	require.Equal(t, first.OperationID, reused.OperationID)
	_, _, err = service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-2"))
	require.ErrorIs(t, err, ErrDeviceUpgradeBusy)
}

func TestUpgradeResponseOrderAndFinalResult(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, _ := newUpgradeService(t, &fakeSender{}, &now)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-order"))
	require.NoError(t, err)
	require.True(t, op.Status == gbmodels.FirmwareUpgradeSent)
	require.True(t, mustConsume(t, service, "34020000001320000001", "call-business", "2", deviceControlResponse(op.SN, "OK")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeAccepted, op.Status)
	require.NotNil(t, op.AcceptedAt)
	require.True(t, mustConsume(t, service, "34020000001320000001", "call-final", "3", upgradeResult(op.SN, op.SessionID, "OK", "v2.3.4", "")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, op.Status)
	require.Equal(t, "v2.3.4", op.CurrentFirmware)
	require.NotNil(t, op.CompletedAt)

	// A late business response cannot roll a terminal final result back.
	require.True(t, mustConsume(t, service, "34020000001320000001", "late", "4", deviceControlResponse(op.SN, "ERROR")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, op.Status)
}

func TestUpgradeFinalResultRejectsWrongSessionAndVersionMismatch(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, _ := newUpgradeService(t, &fakeSender{}, &now)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-mismatch"))
	require.NoError(t, err)
	require.False(t, mustConsume(t, service, "34020000001320000001", "wrong", "1", upgradeResult(op.SN, "0123456789abcdef0123456789abcdef-wrong", "OK", "v2.3.4", "")))
	require.True(t, mustConsume(t, service, "34020000001320000001", "final", "2", upgradeResult(op.SN, op.SessionID, "OK", "v2.3.3", "")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeFailed, op.Status)
	require.Equal(t, "VERSION_MISMATCH", op.ErrorCode)
	require.Equal(t, "v2.3.3", op.CurrentFirmware)
}

func TestUpgradeFailureCodeAndExpiryCanConverge(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, _ := newUpgradeService(t, &fakeSender{}, &now)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-failed"))
	require.NoError(t, err)
	require.True(t, mustConsume(t, service, "34020000001320000001", "final", "2", upgradeResult(op.SN, op.SessionID, "ERROR", "v2.3.3", "02")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeFailed, op.Status)
	require.Equal(t, "02", op.FailedReason)

	// A fresh operation becomes unknown at its deadline and still accepts a
	// valid late final notification.
	secondTarget := upgradeTarget()
	second, _, err := service.Execute(context.Background(), secondTarget, upgradeRequest("upgrade-expiry"))
	require.NoError(t, err)
	now = now.Add(upgradeDeadline + time.Second)
	require.NoError(t, service.ExpireDue(context.Background(), now))
	second, err = service.Get(context.Background(), second.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeUnknown, second.Status)
	require.True(t, mustConsume(t, service, "34020000001320000001", "late-final", "3", upgradeResult(second.SN, second.SessionID, "OK", "v2.3.4", "")))
	second, err = service.Get(context.Background(), second.OperationID)
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, second.Status)
}

func TestUpgradeFinalResultUpdatesDeviceFirmwareAndWinsBeforeTransportPersistence(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	var service *Service
	var db *gorm.DB
	sender := &callbackSender{callback: func() {
		var row gbmodels.GbDeviceFirmwareUpgrade
		require.NoError(t, db.Where("status = ?", gbmodels.FirmwareUpgradeQueued).First(&row).Error)
		consumed, err := service.OnUpgradeMessage(context.Background(), row.DeviceCode, "final-before-send", "2", upgradeResult(row.SN, row.SessionID, "OK", row.Firmware, ""))
		require.NoError(t, err)
		require.True(t, consumed)
	}}
	service, db = newUpgradeService(t, sender, &now)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("final-before-send"))
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeSucceeded, op.Status)
	require.Equal(t, "v2.3.4", op.CurrentFirmware)
	var device gbmodels.GbDevice
	require.NoError(t, db.First(&device, 1).Error)
	require.Equal(t, "v2.3.4", device.Firmware, "matching final success must update the reported device firmware")
	require.Equal(t, http.StatusOK, op.SIPStatus, "late transport persistence must retain the outbound SIP metadata")
}

func TestUpgradeBusinessResponseMayArriveBeforeTransportPersistence(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	var service *Service
	var db *gorm.DB
	sender := &callbackSender{callback: func() {
		var row gbmodels.GbDeviceFirmwareUpgrade
		require.NoError(t, db.Where("status = ?", gbmodels.FirmwareUpgradeQueued).First(&row).Error)
		consumed, err := service.OnUpgradeMessage(context.Background(), row.DeviceCode, "business-before-send", "2", deviceControlResponse(row.SN, "OK"))
		require.NoError(t, err)
		require.True(t, consumed)
	}}
	service, db = newUpgradeService(t, sender, &now)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("business-before-send"))
	require.NoError(t, err)
	require.Equal(t, gbmodels.FirmwareUpgradeAccepted, op.Status)
	require.NotNil(t, op.AcceptedAt)
	acceptedAt := *op.AcceptedAt
	now = now.Add(time.Second)
	require.True(t, mustConsume(t, service, upgradeTarget().DeviceCode, "business-duplicate", "3", deviceControlResponse(op.SN, "OK")))
	op, err = service.Get(context.Background(), op.OperationID)
	require.NoError(t, err)
	require.Equal(t, acceptedAt, *op.AcceptedAt, "duplicate OK must preserve the first acceptedAt")
	require.Equal(t, http.StatusOK, op.SIPStatus)
}

func TestExecuteBlocksAfterRecentDeviceReboot(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, db := newUpgradeService(t, &fakeSender{}, &now)
	reboot := gbmodels.GbPTZOperation{
		OperationID: "recent-reboot", IdempotencyKey: "recent-reboot-key", DeviceID: 1, DeviceCode: upgradeTarget().DeviceCode,
		ChannelID: 0, ChannelCode: "", CmdType: "DeviceControl", Action: "teleboot", SN: 77,
		Status: gbmodels.PTZOperationAccepted, CreatedAt: now.Add(-30 * time.Second),
	}
	require.NoError(t, db.Create(&reboot).Error)
	_, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("upgrade-after-reboot"))
	require.ErrorIs(t, err, ErrDeviceRebootBusy)
}

func TestExecuteExactIdempotencyCanReplayWhileDeviceIsOffline(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	service, db := newUpgradeService(t, &fakeSender{}, &now)
	first, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("offline-replay"))
	require.NoError(t, err)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("id = ?", 1).Update("status", gbmodels.DeviceStatusOffline).Error)
	target := upgradeTarget()
	target.DeviceOnline = false
	replayed, deduplicated, err := service.Execute(context.Background(), target, upgradeRequest("offline-replay"))
	require.NoError(t, err)
	require.True(t, deduplicated)
	require.Equal(t, first.OperationID, replayed.OperationID)
	require.Equal(t, first.Status, replayed.Status)
}

func TestNewServiceFailsClosedWithoutUpgradeTableOrSharedSNFloor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "missing-upgrade.db")), &gorm.Config{})
	require.NoError(t, err)
	_, err = NewService(db, &fakeSender{}, &fakeSN{}, time.Now)
	require.ErrorIs(t, err, ErrServiceUnavailable)

	db = newUpgradeDB(t)
	createdAt := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	legacy := gbmodels.GbDeviceFirmwareUpgrade{
		OperationID: "legacy-upgrade", IdempotencyKey: "legacy-key", DeviceID: 1, DeviceCode: upgradeTarget().DeviceCode,
		Firmware: "v1", FileURL: "https://fixture.example/v1.bin", Manufacturer: "UVP", SessionID: strings.Repeat("a", 32), SN: 400,
		ProfileVersion: "2022", ProfileCharset: "GB18030", Status: gbmodels.FirmwareUpgradeSucceeded, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&legacy).Error)
	allocator := &fakeSN{next: 1}
	service, err := NewService(db, &fakeSender{}, allocator, func() time.Time { return createdAt.Add(time.Hour) })
	require.NoError(t, err)
	op, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("after-restart"))
	require.NoError(t, err)
	require.Equal(t, 401, op.SN, "new service must raise the shared allocator floor from persisted upgrade rows")
}

func TestUpgradeRetireWaitsForInFlightSendAndRejectsNewWork(t *testing.T) {
	now := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	sender := &blockingSender{started: make(chan struct{}), release: make(chan struct{})}
	service, _ := newUpgradeService(t, sender, &now)
	completed := make(chan error, 1)
	go func() {
		_, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("retire-inflight"))
		completed <- err
	}()
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		t.Fatal("upgrade did not enter sender")
	}
	retired := make(chan struct{})
	go func() { service.Retire(); close(retired) }()
	select {
	case <-retired:
		t.Fatal("Retire returned before in-flight send and persistence completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(sender.release)
	require.NoError(t, <-completed)
	select {
	case <-retired:
	case <-time.After(3 * time.Second):
		t.Fatal("Retire did not finish after in-flight work completed")
	}
	_, _, err := service.Execute(context.Background(), upgradeTarget(), upgradeRequest("after-retire"))
	require.ErrorIs(t, err, ErrServiceUnavailable)
}

func mustConsume(t *testing.T, service *Service, deviceCode, callID, cseq string, body []byte) bool {
	t.Helper()
	consumed, err := service.OnMessage(context.Background(), deviceCode, callID, cseq, body)
	require.NoError(t, err)
	return consumed
}
