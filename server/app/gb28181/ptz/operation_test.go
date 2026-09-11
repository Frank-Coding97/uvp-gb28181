package ptz

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func newOperationTestDB(t *testing.T, concurrent bool) *gorm.DB {
	t.Helper()
	dsn := ":memory:"
	if concurrent {
		dsn = filepath.Join(t.TempDir(), "ptz-operation.db") + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}))
	if concurrent {
		sqlDB, sqlErr := db.DB()
		require.NoError(t, sqlErr)
		sqlDB.SetMaxOpenConns(8)
	}
	return db
}

func newOperationService(t *testing.T, db *gorm.DB, sender TrackedSender, now func() time.Time) *Service {
	t.Helper()
	service, err := NewService(db, sender, now)
	require.NoError(t, err)
	return service
}

func responseRequiredCommand(key string) Command {
	return Command{
		CmdType:          "DeviceControl",
		Action:           "home_position",
		IdempotencyKey:   key,
		Payload:          map[string]interface{}{"enabled": false, "presetId": 0, "resetTime": 0},
		ResponseRequired: true,
		MaxAttempts:      1,
		ActorID:          17,
		ActorDeptID:      23,
		Build: func(sn int) ([]byte, error) {
			return []byte(fmt.Sprintf("<Control><SN>%d</SN></Control>", sn)), nil
		},
	}
}

func TestServiceSNContinuesFromDatabaseMaximum(t *testing.T) {
	db := newOperationTestDB(t, false)
	require.NoError(t, db.Create(&gbmodels.GbPTZOperation{
		OperationID: "old", IdempotencyKey: "old", DeviceID: 2, DeviceCode: "D",
		ChannelID: 1, ChannelCode: "C", CmdType: "DeviceControl", SN: 41,
		Status: gbmodels.PTZOperationSent, CreatedAt: time.Now(),
	}).Error)

	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	command := testCommand()
	command.IdempotencyKey = "after-reload"
	operation, err := service.Execute(context.Background(), testTarget(), command)
	require.NoError(t, err)
	require.Equal(t, 42, operation.SN)
}

func TestServiceSNInitializationFailurePreventsAssembly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	service, err := NewService(db, &fakeTrackedSender{}, time.Now)
	require.Error(t, err)
	require.Nil(t, service)
}

func TestOperationResponseRequiredCreationIsAtomicAndQueued(t *testing.T) {
	db := newOperationTestDB(t, false)
	sender := &fakeTrackedSender{}
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	service := newOperationService(t, db, sender, func() time.Time { return now })
	command := responseRequiredCommand("home-control")
	trigger := "control-parent"
	command.TriggerOperationID = trigger

	operation, err := service.Execute(context.Background(), testTarget(), command)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationQueued, operation.Status)
	require.True(t, operation.ResponseRequired)
	require.Zero(t, operation.Attempt)
	require.Equal(t, 1, operation.MaxAttempts)
	require.Equal(t, uint(17), operation.ActorID)
	require.Equal(t, uint(23), operation.ActorDeptID)
	require.NotNil(t, operation.QueueDeadlineAt)
	require.True(t, now.Add(5*time.Second).Equal(*operation.QueueDeadlineAt))
	require.NotNil(t, operation.TriggerOperationID)
	require.Equal(t, trigger, *operation.TriggerOperationID)
	require.JSONEq(t, `{"enabled":false,"presetId":0,"resetTime":0}`, operation.PayloadJSON)
	require.Zero(t, sender.calls, "response-required operation must be dispatched by the scheduler")

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.First(&stored, operation.ID).Error)
	require.Equal(t, operation.OperationID, stored.OperationID)
	require.Zero(t, stored.Attempt)
	require.True(t, stored.ResponseRequired)
}

func TestOperationIdempotencyReturnsOriginalForEveryStatus(t *testing.T) {
	statuses := []gbmodels.PTZOperationStatus{
		gbmodels.PTZOperationQueued,
		gbmodels.PTZOperationAccepted,
		gbmodels.PTZOperationRejected,
		gbmodels.PTZOperationTimeout,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			db := newOperationTestDB(t, false)
			sender := &fakeTrackedSender{}
			service := newOperationService(t, db, sender, time.Now)
			command := responseRequiredCommand("same")
			first, err := service.Execute(context.Background(), testTarget(), command)
			require.NoError(t, err)
			require.NoError(t, db.Model(&first).Update("status", status).Error)

			replayed, err := service.Execute(context.Background(), testTarget(), command)
			require.NoError(t, err)
			require.Equal(t, first.OperationID, replayed.OperationID)
			require.Equal(t, status, replayed.Status)
			require.Zero(t, sender.calls)
		})
	}
}

func TestOperationIdempotencyReplayIgnoresCurrentSendability(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Service, *Target)
	}{
		{
			name: "channel offline",
			mutate: func(_ *Service, target *Target) {
				target.ChannelOnline = false
			},
		},
		{
			name: "source address missing",
			mutate: func(_ *Service, target *Target) {
				target.IP = ""
			},
		},
		{
			name: "sender unavailable",
			mutate: func(service *Service, _ *Target) {
				service.sender = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newOperationTestDB(t, false)
			sender := &fakeTrackedSender{}
			service := newOperationService(t, db, sender, time.Now)
			command := responseRequiredCommand("replay-after-runtime-change")
			target := testTarget()

			created, err := service.Execute(context.Background(), target, command)
			require.NoError(t, err)
			test.mutate(service, &target)

			replayed, err := service.Execute(context.Background(), target, command)
			require.NoError(t, err)
			require.Equal(t, created.OperationID, replayed.OperationID)
			require.Zero(t, sender.calls)
		})
	}
}

func TestOperationIdempotencyReplayStillRequiresTargetIdentity(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	command := responseRequiredCommand("identity-required")
	target := testTarget()
	_, err := service.Execute(context.Background(), target, command)
	require.NoError(t, err)

	target.ChannelCode = ""
	_, err = service.Execute(context.Background(), target, command)
	var operationErr *OperationError
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, ErrorCodeHomePositionUnavailable, operationErr.Code)
}

func TestOperationRejectsUnsafeIdempotencyKeysWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "more than 128 bytes", key: strings.Repeat("k", 129)},
		{name: "nul", key: "key\x00value"},
		{name: "control character", key: "key\x1fvalue"},
		{name: "invalid utf8", key: string([]byte{'k', 0xff})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newOperationTestDB(t, false)
			sender := &fakeTrackedSender{}
			service := newOperationService(t, db, sender, time.Now)
			command := responseRequiredCommand(test.key)

			_, err := service.Execute(context.Background(), testTarget(), command)
			var operationErr *OperationError
			require.ErrorAs(t, err, &operationErr)
			require.Equal(t, ErrorCodeHomePositionInvalidArgument, operationErr.Code)

			var operationCount int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&operationCount).Error)
			require.Zero(t, operationCount)
			require.Zero(t, sender.calls)
		})
	}
}

func TestOperationAcceptsMaximumLengthIdempotencyKey(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	command := responseRequiredCommand(strings.Repeat("k", 128))

	operation, err := service.Execute(context.Background(), testTarget(), command)
	require.NoError(t, err)
	require.Len(t, operation.IdempotencyKey, 128)
}

func TestOperationIdempotencyRejectsCanonicalPayloadConflict(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	first := responseRequiredCommand("same")
	first.Payload = map[string]interface{}{"nested": map[string]interface{}{"b": 2, "a": 1}, "enabled": true}
	created, err := service.Execute(context.Background(), testTarget(), first)
	require.NoError(t, err)

	equivalent := responseRequiredCommand("same")
	equivalent.Payload = map[string]interface{}{"enabled": true, "nested": map[string]interface{}{"a": 1, "b": 2}}
	replayed, err := service.Execute(context.Background(), testTarget(), equivalent)
	require.NoError(t, err)
	require.Equal(t, created.OperationID, replayed.OperationID)

	conflicting := equivalent
	conflicting.Payload = map[string]interface{}{"enabled": false, "nested": map[string]interface{}{"a": 1, "b": 2}}
	_, err = service.Execute(context.Background(), testTarget(), conflicting)
	var operationErr *OperationError
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, ErrorCodeHomePositionIdempotencyConflict, operationErr.Code)
}

func TestOperationIdempotencyConcurrentCreateHasOneWinner(t *testing.T) {
	db := newOperationTestDB(t, true)
	const workers = 8
	services := make([]*Service, workers)
	for i := range services {
		services[i] = newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	}

	var wait sync.WaitGroup
	wait.Add(workers)
	operations := make(chan gbmodels.GbPTZOperation, workers)
	errors := make(chan error, workers)
	for _, service := range services {
		go func(service *Service) {
			defer wait.Done()
			operation, err := service.Execute(context.Background(), testTarget(), responseRequiredCommand("concurrent"))
			operations <- operation
			errors <- err
		}(service)
	}
	wait.Wait()
	close(operations)
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}
	var operationID string
	for operation := range operations {
		if operationID == "" {
			operationID = operation.OperationID
		}
		require.Equal(t, operationID, operation.OperationID)
	}
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

type applyResponseSender struct {
	service *Service
}

func (s *applyResponseSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	var operation gbmodels.GbPTZOperation
	if err := s.service.db.Where("idempotency_key = ?", "quick-ack").First(&operation).Error; err != nil {
		return uac.TrackedMessageResult{}, err
	}
	_, _, err := s.service.ApplyResponse(context.Background(), Response{
		OperationID: operation.OperationID, CallID: "response-call", CSeq: "99", SIPStatus: 200, DeviceResult: "OK",
	})
	return uac.TrackedMessageResult{CallID: "outbound-call", CSeq: "1", StatusCode: 200}, err
}

func TestOperationFastResponseDoesNotRegressAndKeepsMetadataSeparate(t *testing.T) {
	db := newOperationTestDB(t, false)
	sender := &applyResponseSender{}
	service := newOperationService(t, db, sender, time.Now)
	sender.service = service
	command := testCommand()
	command.IdempotencyKey = "quick-ack"

	operation, err := service.Execute(context.Background(), testTarget(), command)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationAccepted, operation.Status)
	require.Equal(t, "outbound-call", operation.CallID)
	require.Equal(t, "1", operation.CSeq)
	require.Equal(t, 200, operation.SIPStatus)
	require.NotNil(t, operation.ResponseCallID)
	require.Equal(t, "response-call", *operation.ResponseCallID)
	require.NotNil(t, operation.ResponseCSeq)
	require.Equal(t, "99", *operation.ResponseCSeq)
}

func TestOperationTerminalCASAndUnknownLateResponse(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	accepted, err := service.Execute(context.Background(), testTarget(), responseRequiredCommand("accepted"))
	require.NoError(t, err)
	require.NoError(t, db.Model(&accepted).Update("status", gbmodels.PTZOperationAccepted).Error)
	accepted, err = service.MarkTimeout(context.Background(), accepted.OperationID, "late timeout")
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationAccepted, accepted.Status)
	accepted, _, err = service.ApplyResponse(context.Background(), Response{OperationID: accepted.OperationID, DeviceResult: "ERROR"})
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationAccepted, accepted.Status)

	unknown, err := service.Execute(context.Background(), testTarget(), responseRequiredCommand("unknown"))
	require.NoError(t, err)
	transportDeadline := time.Now().Add(time.Minute)
	require.NoError(t, db.Model(&unknown).Updates(map[string]interface{}{
		"status": gbmodels.PTZOperationUnknown, "transport_deadline_at": transportDeadline,
	}).Error)
	unknown, matched, err := service.ApplyResponse(context.Background(), Response{OperationID: unknown.OperationID, CallID: "late", CSeq: "7", DeviceResult: "OK"})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, gbmodels.PTZOperationAccepted, unknown.Status)
}

func TestOperationLegacyOneWayStillSendsSynchronously(t *testing.T) {
	db := newOperationTestDB(t, false)
	sender := &fakeTrackedSender{}
	service := newOperationService(t, db, sender, time.Now)
	operation, err := service.Execute(context.Background(), testTarget(), testCommand())
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, operation.Status)
	require.False(t, operation.ResponseRequired)
	require.Equal(t, 1, operation.Attempt)
	require.Equal(t, 1, operation.MaxAttempts)
	require.Nil(t, operation.QueueDeadlineAt)
	require.Equal(t, 1, sender.calls)
}

func TestOperationAuditFieldsCoverControlRefreshAndReconcile(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)

	controlCommand := responseRequiredCommand("manual-control")
	control, err := service.Execute(context.Background(), testTarget(), controlCommand)
	require.NoError(t, err)

	refreshCommand := responseRequiredCommand("manual-refresh")
	refreshCommand.CmdType = "HomePositionQuery"
	refreshCommand.Action = "refresh_home_position"
	refreshCommand.MaxAttempts = 3
	refreshCommand.Payload = map[string]interface{}{}
	refresh, err := service.Execute(context.Background(), testTarget(), refreshCommand)
	require.NoError(t, err)
	require.Equal(t, 3, refresh.MaxAttempts)
	require.Equal(t, control.ActorID, refresh.ActorID)
	require.Equal(t, control.ActorDeptID, refresh.ActorDeptID)

	reconcileCommand := refreshCommand
	reconcileCommand.IdempotencyKey = "home-reconcile:" + control.OperationID
	reconcileCommand.TriggerOperationID = control.OperationID
	reconcileCommand.Payload = map[string]interface{}{"triggerOperationId": control.OperationID}
	reconcile, err := service.Execute(context.Background(), testTarget(), reconcileCommand)
	require.NoError(t, err)
	require.NotNil(t, reconcile.TriggerOperationID)
	require.Equal(t, control.OperationID, *reconcile.TriggerOperationID)
	require.Contains(t, reconcile.PayloadJSON, control.OperationID)
	require.Greater(t, refresh.SN, control.SN)
	require.Greater(t, reconcile.SN, refresh.SN)
}

func TestOperationTargetErrorsAreTyped(t *testing.T) {
	db := newOperationTestDB(t, false)
	service := newOperationService(t, db, &fakeTrackedSender{}, time.Now)
	target := testTarget()
	target.ChannelOnline = false
	_, err := service.Execute(context.Background(), target, responseRequiredCommand("offline"))
	var operationErr *OperationError
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, ErrorCodeHomePositionDeviceOffline, operationErr.Code)

	target = testTarget()
	target.IP = ""
	_, err = service.Execute(context.Background(), target, responseRequiredCommand("unavailable"))
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, ErrorCodeHomePositionUnavailable, operationErr.Code)
}
