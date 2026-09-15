package ptz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type schedulerRetryBlockingSender struct {
	mu            sync.Mutex
	calls         int
	firstEntered  chan struct{}
	firstCanceled chan struct{}
	secondSent    chan struct{}
}

func (s *schedulerRetryBlockingSender) SendMessageTracked(ctx context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.firstEntered)
		<-ctx.Done()
		close(s.firstCanceled)
		return uac.TrackedMessageResult{CallID: "slow-1", CSeq: "1", Attempted: true}, ctx.Err()
	}
	close(s.secondSent)
	return uac.TrackedMessageResult{CallID: "retry-2", CSeq: "2", StatusCode: 200, Attempted: true}, nil
}

type schedulerStopBlockingSender struct {
	entered        chan struct{}
	cancelObserved chan struct{}
	allowExit      chan struct{}
}

func (s *schedulerStopBlockingSender) SendMessageTracked(ctx context.Context, _, _, _ string, _ []byte) (uac.TrackedMessageResult, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.cancelObserved)
	<-s.allowExit
	return uac.TrackedMessageResult{CallID: "stopped", CSeq: "1", Attempted: true}, ctx.Err()
}

func schedulerDispatcherWork(d *schedulerManualDispatcher) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.tasks)
}

func runSchedulerRace(t *testing.T, calls ...func() error) {
	t.Helper()
	start := make(chan struct{})
	errorsCh := make(chan error, len(calls))
	var wg sync.WaitGroup
	for _, call := range calls {
		call := call
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errorsCh <- call()
		}()
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
}

func TestSchedulerCoordinatorConcurrentClaimIsUnique(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 3)
	otherDispatcher := &schedulerManualDispatcher{capacity: 1}
	other := NewScheduler(fixture.scheduler.service, WithSchedulerDispatcher(otherDispatcher))
	now := fixture.clock.Now()

	runSchedulerRace(t,
		func() error { return fixture.scheduler.RunDue(now) },
		func() error { return other.RunDue(now) },
	)

	require.Len(t, loadSchedulerAttempts(t, fixture.db, operation.ID), 1)
	require.Equal(t, 1, schedulerDispatcherWork(fixture.dispatcher)+schedulerDispatcherWork(otherDispatcher))
	fixture.dispatcher.Drain()
	otherDispatcher.Drain()
	require.Len(t, fixture.sender.Calls(), 1)
}

func TestSchedulerCoordinatorLeaseRecoveryWinsLateSender(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 3)
	t0 := fixture.clock.Now()
	require.NoError(t, fixture.scheduler.RunDue(t0))
	first := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]

	now := t0.Add(time.Second)
	fixture.clock.Set(now)
	otherDispatcher := &schedulerManualDispatcher{capacity: 1}
	other := NewScheduler(fixture.scheduler.service, WithSchedulerDispatcher(otherDispatcher))
	runSchedulerRace(t,
		func() error { return other.RunDue(now) },
		func() error {
			return fixture.scheduler.persistAttemptResult(context.Background(), first, uac.TrackedMessageResult{
				CallID: "late-call", CSeq: "9", StatusCode: 200, Attempted: true,
			}, nil, now)
		},
	)

	updated := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationQueued, updated.Status)
	require.Empty(t, updated.CallID)
	require.Nil(t, updated.SentAt)
	attempts := loadSchedulerAttempts(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, attempts[0].Status)
	require.LessOrEqual(t, len(attempts), 2)
}

func TestSchedulerCoordinatorCancelsSlowSenderBeforeRetry(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	sender := &schedulerRetryBlockingSender{
		firstEntered: make(chan struct{}), firstCanceled: make(chan struct{}), secondSent: make(chan struct{}),
	}
	fixture.scheduler.service.sender = sender
	scheduler := NewScheduler(fixture.scheduler.service, WithSchedulerCapacity(2))
	operation := fixture.createOperation(t, 3)
	t0 := fixture.clock.Now()
	require.NoError(t, scheduler.RunDue(t0))
	<-sender.firstEntered

	fixture.clock.Set(t0.Add(time.Second))
	require.NoError(t, scheduler.RunDue(fixture.clock.Now()))
	<-sender.firstCanceled
	<-sender.secondSent
	scheduler.Stop()

	attempts := loadSchedulerAttempts(t, fixture.db, operation.ID)
	require.Len(t, attempts, 2)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, attempts[0].Status)
	require.Equal(t, gbmodels.PTZOperationAttemptSent, attempts[1].Status)
}

func TestSchedulerCoordinatorStopCancelsAndWaitsForSender(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	sender := &schedulerStopBlockingSender{
		entered: make(chan struct{}), cancelObserved: make(chan struct{}), allowExit: make(chan struct{}),
	}
	fixture.scheduler.service.sender = sender
	scheduler := NewScheduler(fixture.scheduler.service, WithSchedulerCapacity(1))
	fixture.createOperation(t, 1)
	require.NoError(t, scheduler.RunDue(fixture.clock.Now()))
	<-sender.entered

	stopped := make(chan struct{})
	go func() {
		scheduler.Stop()
		close(stopped)
	}()
	<-sender.cancelObserved
	select {
	case <-stopped:
		require.Fail(t, "Stop returned before the sender exited")
	default:
	}
	close(sender.allowExit)
	<-stopped
}

func TestSchedulerApplyResponseConcurrentWithSenderKeepsTerminalState(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 3)
	t0 := fixture.clock.Now()
	require.NoError(t, fixture.scheduler.RunDue(t0))
	attempt := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
	now := t0.Add(500 * time.Millisecond)
	fixture.clock.Set(now)

	runSchedulerRace(t,
		func() error {
			_, _, err := fixture.scheduler.service.ApplyResponse(context.Background(), Response{
				OperationID: operation.OperationID, DeviceResult: "OK", SIPStatus: 200,
			})
			return err
		},
		func() error {
			return fixture.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{
				CallID: "sender-call", CSeq: "1", StatusCode: 200, Attempted: true,
			}, nil, now)
		},
	)

	updated := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationAccepted, updated.Status)
	require.Equal(t, "sender-call", updated.CallID)
	require.NotNil(t, updated.SentAt)
	require.NotNil(t, updated.CompletedAt)
}

func TestSchedulerDeadlineAtExactBoundaryPrecedesSender(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 1)
	operation.CmdType = manscdp.CmdDeviceControl
	operation.Action = "home_position"
	operation.PayloadJSON = `{"enabled":false}`
	require.NoError(t, fixture.db.Save(&operation).Error)
	t0 := fixture.clock.Now()
	require.NoError(t, fixture.scheduler.RunDue(t0))
	attempt := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
	now := t0.Add(15 * time.Second)
	fixture.clock.Set(now)

	runSchedulerRace(t,
		func() error { return fixture.scheduler.RunDue(now) },
		func() error {
			return fixture.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{
				CallID: "boundary-call", CSeq: "1", StatusCode: 200, Attempted: true,
			}, nil, now)
		},
	)

	updated := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationUnknown, updated.Status)
	require.Empty(t, updated.CallID)
	require.Nil(t, updated.SentAt)
	require.Empty(t, fixture.sender.Calls(), "截止同刻不得真实下发")
}

func TestSchedulerApplyResponseAtApplicationDeadlineCannotBeatTimeout(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 3)
	t0 := fixture.clock.Now()
	require.NoError(t, fixture.scheduler.RunDue(t0))
	attempt := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
	require.NoError(t, fixture.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{
		CallID: "sent", CSeq: "1", StatusCode: 200, Attempted: true,
	}, nil, t0))
	deadline := t0.Add(15 * time.Second)
	fixture.clock.Set(deadline)

	runSchedulerRace(t,
		func() error { return fixture.scheduler.RunDue(deadline) },
		func() error {
			_, _, err := fixture.scheduler.service.ApplyResponse(context.Background(), Response{
				OperationID: operation.OperationID, DeviceResult: "OK", SIPStatus: 200,
			})
			return err
		},
	)
	require.Equal(t, gbmodels.PTZOperationTimeout, loadSchedulerOperation(t, fixture.db, operation.ID).Status)
}

func TestSchedulerTimelyObservedResultSurvivesDelayedWriteback(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 1)
	operation.CmdType = manscdp.CmdDeviceControl
	operation.Action = "home_position"
	operation.PayloadJSON = `{"enabled":false}`
	require.NoError(t, fixture.db.Save(&operation).Error)
	t0 := fixture.clock.Now()
	require.NoError(t, fixture.scheduler.RunDue(t0))
	attempt := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
	observedAt := t0.Add(15*time.Second - time.Nanosecond)

	fixture.clock.Set(t0.Add(15 * time.Second))
	require.NoError(t, fixture.scheduler.RunDue(fixture.clock.Now()))
	require.Equal(t, gbmodels.PTZOperationUnknown, loadSchedulerOperation(t, fixture.db, operation.ID).Status)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, loadSchedulerAttempts(t, fixture.db, operation.ID)[0].Status)

	require.NoError(t, fixture.scheduler.persistAttemptResult(context.Background(), attempt, uac.TrackedMessageResult{
		CallID: "timely", CSeq: "1", StatusCode: 200, Attempted: true,
	}, nil, observedAt))
	updated := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationSent, updated.Status)
	require.Equal(t, "timely", updated.CallID)
	require.Nil(t, updated.CompletedAt)
	require.NotNil(t, updated.DeadlineAt)
	require.True(t, updated.DeadlineAt.Equal(observedAt.Add(15*time.Second)))
	require.Equal(t, gbmodels.PTZOperationAttemptSent, loadSchedulerAttempts(t, fixture.db, operation.ID)[0].Status)
}

func TestSchedulerPreSendCancellationIsExplicitNoSend(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	operation := fixture.createOperation(t, 1)
	operation.CmdType = manscdp.CmdDeviceControl
	operation.Action = "home_position"
	operation.PayloadJSON = `{"enabled":false}`
	require.NoError(t, fixture.db.Save(&operation).Error)
	require.NoError(t, fixture.scheduler.RunDue(fixture.clock.Now()))
	attempt := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]

	dispatchCtx, cancel := context.WithCancel(context.Background())
	cancel()
	fixture.scheduler.dispatchAttempt(dispatchCtx, attempt)

	updated := loadSchedulerOperation(t, fixture.db, operation.ID)
	require.Equal(t, gbmodels.PTZOperationRejected, updated.Status)
	require.Equal(t, schedulerErrorHomePositionUnavailable, updated.ErrorCode)
	require.Equal(t, gbmodels.PTZOperationAttemptFailed, loadSchedulerAttempts(t, fixture.db, operation.ID)[0].Status)
	require.Empty(t, fixture.sender.Calls())
}

func TestSchedulerReloadCompensationAtEightAndFifteenSeconds(t *testing.T) {
	t.Run("eight seconds sends at most one compensation", func(t *testing.T) {
		fixture := newSchedulerFixture(t, 1)
		operation := fixture.createOperation(t, 3)
		t0 := fixture.clock.Now()
		require.NoError(t, fixture.scheduler.RunDue(t0))
		first := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
		observed := t0.Add(500 * time.Millisecond)
		require.NoError(t, fixture.scheduler.persistAttemptResult(context.Background(), first,
			uac.TrackedMessageResult{CallID: "uncertain", CSeq: "1", Attempted: true},
			context.DeadlineExceeded, observed,
		))

		reloadAt := t0.Add(8 * time.Second)
		fixture.clock.Set(reloadAt)
		dispatcher := &schedulerManualDispatcher{capacity: 3}
		reloaded := NewScheduler(fixture.scheduler.service, WithSchedulerDispatcher(dispatcher))
		require.NoError(t, reloaded.RunDue(reloadAt))
		require.Equal(t, 1, schedulerDispatcherWork(dispatcher))
		attempts := loadSchedulerAttempts(t, fixture.db, operation.ID)
		require.Len(t, attempts, 2)
		require.True(t, attempts[1].LeaseUntil.Equal(t0.Add(15*time.Second)))
		updated := loadSchedulerOperation(t, fixture.db, operation.ID)
		require.Nil(t, updated.NextAttemptAt)
		dispatcher.Drain()
		require.Len(t, fixture.sender.Calls(), 1)
	})

	t.Run("transport deadline sends no compensation", func(t *testing.T) {
		fixture := newSchedulerFixture(t, 1)
		operation := fixture.createOperation(t, 3)
		t0 := fixture.clock.Now()
		require.NoError(t, fixture.scheduler.RunDue(t0))
		first := loadSchedulerAttempts(t, fixture.db, operation.ID)[0]
		require.NoError(t, fixture.scheduler.persistAttemptResult(context.Background(), first,
			uac.TrackedMessageResult{CallID: "uncertain", CSeq: "1", Attempted: true},
			context.DeadlineExceeded, t0.Add(500*time.Millisecond),
		))

		reloadAt := t0.Add(15 * time.Second)
		fixture.clock.Set(reloadAt)
		dispatcher := &schedulerManualDispatcher{capacity: 3}
		reloaded := NewScheduler(fixture.scheduler.service, WithSchedulerDispatcher(dispatcher))
		require.NoError(t, reloaded.RunDue(reloadAt))
		require.Zero(t, schedulerDispatcherWork(dispatcher))
		require.Empty(t, fixture.sender.Calls())
		require.Equal(t, gbmodels.PTZOperationUnknown, loadSchedulerOperation(t, fixture.db, operation.ID).Status)
	})
}

func TestPTZServiceReloadDrainsOldExecuteBeforeReadingMaxSN(t *testing.T) {
	fixture := newSchedulerFixture(t, 1)
	oldService := fixture.scheduler.service
	entered := make(chan int, 1)
	releaseBuild := make(chan struct{})
	oldResult := make(chan gbmodels.GbPTZOperation, 1)
	oldError := make(chan error, 1)
	target := Target{
		DeviceID: 1, DeviceCode: "D", ChannelID: 1, ChannelCode: "C",
		IP: "192.0.2.10", Port: 5060, Transport: "UDP", DeviceOnline: true, ChannelOnline: true,
	}
	go func() {
		operation, err := oldService.Execute(context.Background(), target, Command{
			CmdType: manscdp.CmdHomePositionQuery, Action: "refresh_home_position", IdempotencyKey: "old-in-flight",
			ResponseRequired: true, MaxAttempts: 3, Payload: map[string]interface{}{},
			Build: func(sn int) ([]byte, error) {
				entered <- sn
				<-releaseBuild
				return manscdp.BuildHomePositionQuery(target.ChannelCode, sn)
			},
		})
		oldResult <- operation
		oldError <- err
	}()
	oldSN := <-entered
	retired := make(chan struct{})
	go func() {
		oldService.Retire()
		close(retired)
	}()
	select {
	case <-retired:
		require.Fail(t, "Retire returned before the in-flight Execute persisted")
	default:
	}
	close(releaseBuild)
	oldOperation := <-oldResult
	require.NoError(t, <-oldError)
	<-retired
	require.Equal(t, oldSN, oldOperation.SN)

	newService, err := NewService(fixture.db, fixture.sender, fixture.clock.Now)
	require.NoError(t, err)
	newOperation, err := newService.Execute(context.Background(), target, Command{
		CmdType: manscdp.CmdHomePositionQuery, Action: "refresh_home_position", IdempotencyKey: "new-generation",
		ResponseRequired: true, MaxAttempts: 3, Payload: map[string]interface{}{},
		Build: func(sn int) ([]byte, error) { return manscdp.BuildHomePositionQuery(target.ChannelCode, sn) },
	})
	require.NoError(t, err)
	require.Greater(t, newOperation.SN, oldOperation.SN)

	_, err = oldService.Execute(context.Background(), target, Command{
		CmdType: manscdp.CmdHomePositionQuery, Action: "refresh_home_position", IdempotencyKey: "retired-generation",
		ResponseRequired: true, MaxAttempts: 3, Payload: map[string]interface{}{},
		Build: func(sn int) ([]byte, error) { return manscdp.BuildHomePositionQuery(target.ChannelCode, sn) },
	})
	var operationErr *OperationError
	require.True(t, errors.As(err, &operationErr))
	require.Equal(t, ErrorCodeHomePositionUnavailable, operationErr.Code)
}
