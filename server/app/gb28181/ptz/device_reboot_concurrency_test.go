package ptz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func TestDeviceRebootConcurrentServicesSendOnce(t *testing.T) {
	sender := &rebootSender{results: []uac.TrackedMessageResult{{StatusCode: 200, Attempted: true}}}
	first, db, device := newDeviceRebootService(t, sender, time.Now)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	second, err := NewService(db, sender, time.Now)
	require.NoError(t, err)
	services := []*Service{first, second}
	target := deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022))
	start := make(chan struct{})
	results := make(chan gbmodels.GbPTZOperation, 12)
	errorsCh := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			op, executeErr := services[index%2].ExecuteDeviceReboot(context.Background(), target, []string{"key-a", "key-b"}[index%2], 1, 1)
			results <- op
			errorsCh <- executeErr
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	close(errorsCh)
	for executeErr := range errorsCh {
		require.NoError(t, executeErr)
	}
	var operationID string
	for op := range results {
		if operationID == "" {
			operationID = op.OperationID
		}
		require.Equal(t, operationID, op.OperationID)
	}
	require.NotEmpty(t, operationID)
	require.Equal(t, 1, sender.calls)
}

func TestDeviceRebootDatabaseFailureNeverSends(t *testing.T) {
	sender := &rebootSender{}
	service, db, device := newDeviceRebootService(t, sender, time.Now)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reboot_test_fail_create", func(tx *gorm.DB) {
		if tx.Statement.Table == (gbmodels.GbPTZOperation{}).TableName() {
			tx.AddError(errors.New("operation storage unavailable"))
		}
	}))
	_, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "db-failure", 1, 1)
	require.ErrorContains(t, err, "operation storage unavailable")
	require.Zero(t, sender.calls)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.Zero(t, count)
}

type waitingRebootSender struct {
	started chan struct{}
	release chan struct{}
}

func (s *waitingRebootSender) SendMessageTracked(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
	close(s.started)
	<-s.release
	return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
}

func TestDeviceRebootRetireWaitsForInflightPersistence(t *testing.T) {
	sender := &waitingRebootSender{started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(sender.release) }) }
	t.Cleanup(release)
	service, db, device := newDeviceRebootService(t, sender, time.Now)
	completed := make(chan error, 1)
	go func() {
		_, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "inflight", 1, 1)
		completed <- err
	}()
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		t.Fatal("reboot did not enter sender")
	}
	retired := make(chan struct{})
	go func() { service.Retire(); close(retired) }()
	select {
	case <-retired:
		t.Fatal("Retire returned before in-flight reboot was persisted")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	require.NoError(t, <-completed)
	select {
	case <-retired:
	case <-time.After(3 * time.Second):
		t.Fatal("Retire did not finish")
	}
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.First(&operation).Error)
	require.Equal(t, gbmodels.PTZOperationSent, operation.Status)
}

func TestDeviceRebootExactKeySurvivesDedupWindow(t *testing.T) {
	now := time.Now()
	sender := &rebootSender{results: []uac.TrackedMessageResult{{StatusCode: 200}, {StatusCode: 200}}}
	service, _, device := newDeviceRebootService(t, sender, func() time.Time { return now })
	target := deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022))
	first, err := service.ExecuteDeviceReboot(context.Background(), target, "original", 1, 1)
	require.NoError(t, err)
	now = now.Add(2 * time.Minute)
	second, err := service.ExecuteDeviceReboot(context.Background(), target, "later", 1, 1)
	require.NoError(t, err)
	require.NotEqual(t, first.OperationID, second.OperationID)
	replay, err := service.ExecuteDeviceReboot(context.Background(), target, "original", 1, 1)
	require.NoError(t, err)
	require.Equal(t, first.OperationID, replay.OperationID)
	require.Equal(t, 2, sender.calls)
}
