package ptz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakeTrackedSender struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeTrackedSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return uac.TrackedMessageResult{}, f.err
	}
	return uac.TrackedMessageResult{CallID: "call-1", CSeq: "1", StatusCode: 200}, nil
}

func newPTZTestService(t *testing.T, sender TrackedSender) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}))
	return NewService(db, sender, func() time.Time { return time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC) })
}

func testTarget() Target {
	return Target{DeviceID: 2, DeviceCode: "D", ChannelID: 1, ChannelCode: "C", IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true, PTZType: 1}
}

func testCommand() Command {
	return Command{CmdType: "DeviceControl", Action: "left", IdempotencyKey: "same", Build: func(sn int) ([]byte, error) {
		return []byte("<Control><SN>" + string(rune('0'+sn)) + "</SN></Control>"), nil
	}}
}

func TestServiceExecute_IdempotentAndTracked(t *testing.T) {
	sender := &fakeTrackedSender{}
	svc := newPTZTestService(t, sender)
	one, err := svc.Execute(context.Background(), testTarget(), testCommand())
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, one.Status)
	two, err := svc.Execute(context.Background(), testTarget(), testCommand())
	require.NoError(t, err)
	require.Equal(t, one.OperationID, two.OperationID)
	require.Equal(t, 1, sender.calls)
}

func TestServiceExecute_RejectsOfflineOrMissingAddress(t *testing.T) {
	sender := &fakeTrackedSender{}
	svc := newPTZTestService(t, sender)
	target := testTarget()
	target.DeviceOnline = false
	_, err := svc.Execute(context.Background(), target, testCommand())
	require.Error(t, err)
	target = testTarget()
	target.IP = ""
	_, err = svc.Execute(context.Background(), target, testCommand())
	require.Error(t, err)
	require.Equal(t, 0, sender.calls)
}

func TestServiceExecute_SenderFailurePersistsStatus(t *testing.T) {
	sender := &fakeTrackedSender{err: errors.New("network down")}
	svc := newPTZTestService(t, sender)
	op, err := svc.Execute(context.Background(), testTarget(), testCommand())
	require.Error(t, err)
	require.Equal(t, gbmodels.PTZOperationRejected, op.Status)
}

func TestServiceApplyResponse_MapsAndProtectsTerminalState(t *testing.T) {
	svc := newPTZTestService(t, &fakeTrackedSender{})
	op, err := svc.Execute(context.Background(), testTarget(), testCommand())
	require.NoError(t, err)
	updated, matched, err := svc.ApplyResponse(context.Background(), Response{OperationID: op.OperationID, SIPStatus: 200, DeviceResult: "OK"})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, gbmodels.PTZOperationAccepted, updated.Status)
	updated, matched, err = svc.ApplyResponse(context.Background(), Response{OperationID: op.OperationID, SIPStatus: 200, DeviceResult: "ERROR", DeviceError: "late"})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, gbmodels.PTZOperationAccepted, updated.Status)
}

func TestServiceApplyResponse_RejectAndTimeout(t *testing.T) {
	svc := newPTZTestService(t, &fakeTrackedSender{})
	op, err := svc.Execute(context.Background(), testTarget(), testCommand())
	require.NoError(t, err)
	updated, matched, err := svc.ApplyResponse(context.Background(), Response{OperationID: op.OperationID, SIPStatus: 486, DeviceResult: "ERROR", DeviceError: "busy"})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, gbmodels.PTZOperationRejected, updated.Status)

	secondCommand := testCommand()
	secondCommand.IdempotencyKey = "second"
	second, err := svc.Execute(context.Background(), testTarget(), secondCommand)
	require.NoError(t, err)
	result, err := svc.MarkTimeout(context.Background(), second.OperationID, "deadline")
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationTimeout, result.Status)
}

func TestServiceApplyResponse_NoMatch(t *testing.T) {
	svc := newPTZTestService(t, &fakeTrackedSender{})
	_, matched, err := svc.ApplyResponse(context.Background(), Response{OperationID: "missing", SIPStatus: 200, DeviceResult: "OK"})
	require.NoError(t, err)
	require.False(t, matched)
}
