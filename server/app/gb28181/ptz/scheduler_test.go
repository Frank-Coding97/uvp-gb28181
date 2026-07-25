package ptz

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type schedulerFakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *schedulerFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *schedulerFakeClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type schedulerSendCall struct {
	body     string
	deadline time.Time
}

type schedulerFakeSender struct {
	mu      sync.Mutex
	calls   []schedulerSendCall
	results []uac.TrackedMessageResult
	errors  []error
}

func (s *schedulerFakeSender) SendMessageTracked(ctx context.Context, _, _, _ string, body []byte) (uac.TrackedMessageResult, error) {
	deadline, _ := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	index := len(s.calls)
	s.calls = append(s.calls, schedulerSendCall{body: string(body), deadline: deadline})
	result := uac.TrackedMessageResult{CallID: fmt.Sprintf("call-%d", index+1), CSeq: fmt.Sprint(index + 1), StatusCode: 200, Attempted: true}
	if index < len(s.results) {
		result = s.results[index]
	}
	if index < len(s.errors) {
		return result, s.errors[index]
	}
	return result, nil
}

func (s *schedulerFakeSender) Calls() []schedulerSendCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]schedulerSendCall(nil), s.calls...)
}

type schedulerManualDispatcher struct {
	mu       sync.Mutex
	capacity int
	reserved int
	tasks    []func(context.Context)
}

func (d *schedulerManualDispatcher) Reserve() (schedulerDispatchReservation, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reserved >= d.capacity {
		return nil, false
	}
	d.reserved++
	return &schedulerManualReservation{dispatcher: d}, true
}

func (d *schedulerManualDispatcher) Stop() {}

func (d *schedulerManualDispatcher) Drain() {
	for {
		d.mu.Lock()
		if len(d.tasks) == 0 {
			d.mu.Unlock()
			return
		}
		task := d.tasks[0]
		d.tasks = d.tasks[1:]
		d.mu.Unlock()
		task(context.Background())
		d.mu.Lock()
		d.reserved--
		d.mu.Unlock()
	}
}

type schedulerManualReservation struct {
	dispatcher *schedulerManualDispatcher
	once       sync.Once
}

func (r *schedulerManualReservation) Commit(task func(context.Context)) {
	r.once.Do(func() {
		r.dispatcher.mu.Lock()
		r.dispatcher.tasks = append(r.dispatcher.tasks, task)
		r.dispatcher.mu.Unlock()
	})
}

func (r *schedulerManualReservation) Release() {
	r.once.Do(func() {
		r.dispatcher.mu.Lock()
		r.dispatcher.reserved--
		r.dispatcher.mu.Unlock()
	})
}

type schedulerFixture struct {
	db         *gorm.DB
	clock      *schedulerFakeClock
	sender     *schedulerFakeSender
	dispatcher *schedulerManualDispatcher
	scheduler  *Scheduler
}

func newSchedulerFixture(t *testing.T, capacity int) *schedulerFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scheduler.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}).Error)
	clock := &schedulerFakeClock{now: time.Now().Add(time.Hour).Truncate(time.Second)}
	sender := &schedulerFakeSender{}
	service, err := NewService(db, sender, clock.Now)
	require.NoError(t, err)
	dispatcher := &schedulerManualDispatcher{capacity: capacity}
	return &schedulerFixture{
		db: db, clock: clock, sender: sender, dispatcher: dispatcher,
		scheduler: NewScheduler(service, WithSchedulerDispatcher(dispatcher)),
	}
}

func (f *schedulerFixture) createOperation(t *testing.T, maxAttempts int) gbmodels.GbPTZOperation {
	t.Helper()
	now := f.clock.Now()
	queueDeadline := now.Add(5 * time.Second)
	operationID := fmt.Sprintf("op-%d", now.UnixNano())
	require.NoError(t, f.db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id": operationID, "idempotency_key": fmt.Sprintf("key-%d", now.UnixNano()),
		"device_id": 1, "device_code": "D", "channel_id": 1, "channel_code": "C",
		"cmd_type": manscdp.CmdHomePositionQuery, "action": "refresh_home_position", "payload_json": "{}", "sn": 41,
		"status": gbmodels.PTZOperationQueued, "attempt": 0, "response_required": true, "max_attempts": maxAttempts,
		"queue_deadline_at": queueDeadline, "created_at": now,
	}).Error)
	var op gbmodels.GbPTZOperation
	require.NoError(t, f.db.Where("operation_id = ?", operationID).First(&op).Error)
	return op
}

func loadSchedulerOperation(t *testing.T, db *gorm.DB, id uint) gbmodels.GbPTZOperation {
	t.Helper()
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.First(&operation, id).Error)
	return operation
}

func loadSchedulerAttempts(t *testing.T, db *gorm.DB, operationID uint) []gbmodels.GbPTZOperationAttempt {
	t.Helper()
	var attempts []gbmodels.GbPTZOperationAttempt
	require.NoError(t, db.Where("operation_id = ?", operationID).Order("attempt_no").Find(&attempts).Error)
	return attempts
}

func TestSchedulerFixedQuerySlotsAndApplicationDeadline(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 3)
	t0 := f.clock.Now()

	for _, now := range []time.Time{t0, t0.Add(time.Second), t0.Add(6 * time.Second)} {
		f.clock.Set(now)
		require.NoError(t, f.scheduler.RunDue(now))
		f.dispatcher.Drain()
	}

	attempts := loadSchedulerAttempts(t, f.db, op.ID)
	require.Len(t, attempts, 3)
	for index, attempt := range attempts {
		require.Equal(t, index+1, attempt.AttemptNo)
		require.Equal(t, gbmodels.PTZOperationAttemptSent, attempt.Status)
		require.Equal(t, op.SN, attempt.SN)
	}
	calls := f.sender.Calls()
	require.Len(t, calls, 3)
	for _, call := range calls {
		require.Contains(t, call.body, "<SN>41</SN>")
	}
	require.NotEqual(t, attempts[0].CallID, attempts[1].CallID)
	require.True(t, attempts[0].LeaseUntil.Equal(t0.Add(time.Second)))
	require.True(t, attempts[1].LeaseUntil.Equal(t0.Add(6*time.Second)))
	require.True(t, attempts[2].LeaseUntil.Equal(t0.Add(15*time.Second)))
	require.True(t, calls[0].deadline.Equal(t0.Add(time.Second)))
	require.True(t, calls[1].deadline.Equal(t0.Add(6*time.Second)))
	require.True(t, calls[2].deadline.Equal(t0.Add(15*time.Second)))

	f.clock.Set(t0.Add(15 * time.Second))
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	f.dispatcher.Drain()
	updated := loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationTimeout, updated.Status)
	require.Equal(t, schedulerErrorApplicationTimeout, updated.ErrorCode)
	require.Len(t, f.sender.Calls(), 3, "截止点不得补发")
}

func TestSchedulerQueueDeadlineAndLegacyIsolation(t *testing.T) {
	f := newSchedulerFixture(t, 0)
	op := f.createOperation(t, 1)
	legacy := gbmodels.GbPTZOperation{
		OperationID: "legacy", IdempotencyKey: "legacy", DeviceID: 1, DeviceCode: "D", ChannelID: 2, ChannelCode: "L",
		CmdType: manscdp.CmdDeviceControl, Status: gbmodels.PTZOperationQueued, Attempt: 1, MaxAttempts: 1, CreatedAt: f.clock.Now(),
	}
	require.NoError(t, f.db.Create(&legacy).Error)

	f.clock.Set(f.clock.Now().Add(5 * time.Second))
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	require.Equal(t, gbmodels.PTZOperationRejected, loadSchedulerOperation(t, f.db, op.ID).Status)
	require.Equal(t, gbmodels.PTZOperationQueued, loadSchedulerOperation(t, f.db, legacy.ID).Status)
	require.Empty(t, loadSchedulerAttempts(t, f.db, op.ID))
}

func TestSchedulerFastResponseWinsSenderWriteback(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 1)
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	updated, matched, err := f.scheduler.service.ApplyResponse(context.Background(), Response{OperationID: op.OperationID, DeviceResult: "OK", SIPStatus: 200})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, gbmodels.PTZOperationAccepted, updated.Status)

	f.dispatcher.Drain()
	updated = loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationAccepted, updated.Status)
	require.NotEmpty(t, updated.CallID)
	require.Nil(t, updated.DeadlineAt)
}

func TestSchedulerControlTransportUncertainDoesNotRetry(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	f.sender.results = []uac.TrackedMessageResult{{CallID: "uncertain", CSeq: "1", Attempted: true}}
	f.sender.errors = []error{context.DeadlineExceeded}
	op := f.createOperation(t, 1)
	op.CmdType = manscdp.CmdDeviceControl
	op.Action = "home_position"
	op.PayloadJSON = `{"enabled":true,"resetTime":30,"presetId":3}`
	require.NoError(t, f.db.Save(&op).Error)
	t0 := f.clock.Now()

	require.NoError(t, f.scheduler.RunDue(t0))
	f.dispatcher.Drain()
	updated := loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationUnknown, updated.Status)
	require.Equal(t, schedulerErrorTransportUnknown, updated.ErrorCode)
	require.Equal(t, "uncertain", updated.CallID)
	require.Equal(t, "1", updated.CSeq)
	require.Zero(t, updated.SIPStatus)
	require.Nil(t, updated.SentAt)
	require.Nil(t, updated.DeadlineAt)

	f.clock.Set(t0.Add(15 * time.Second))
	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	f.dispatcher.Drain()
	require.Len(t, f.sender.Calls(), 1)
	require.Len(t, loadSchedulerAttempts(t, f.db, op.ID), 1)
}

func TestSchedulerNon2xxPersistsFirstOutboundAuditWithoutSentAt(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	f.sender.results = []uac.TrackedMessageResult{{CallID: "rejected-call", CSeq: "7", StatusCode: 486, Attempted: true}}
	f.sender.errors = []error{errors.New("MESSAGE response 486")}
	op := f.createOperation(t, 1)
	op.CmdType = manscdp.CmdDeviceControl
	op.Action = "home_position"
	op.PayloadJSON = `{"enabled":false}`
	require.NoError(t, f.db.Save(&op).Error)

	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	f.dispatcher.Drain()
	updated := loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationRejected, updated.Status)
	require.Equal(t, "rejected-call", updated.CallID)
	require.Equal(t, "7", updated.CSeq)
	require.Equal(t, 486, updated.SIPStatus)
	require.Nil(t, updated.SentAt)
	require.Nil(t, updated.DeadlineAt)
}

func TestSchedulerLateSenderCannotAdvanceParent(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 3)
	t0 := f.clock.Now()
	require.NoError(t, f.scheduler.RunDue(t0))
	attempts := loadSchedulerAttempts(t, f.db, op.ID)
	require.Len(t, attempts, 1)

	late := t0.Add(time.Second)
	f.clock.Set(late)
	require.NoError(t, f.scheduler.persistAttemptResult(context.Background(), attempts[0], uac.TrackedMessageResult{
		CallID: "late", CSeq: "9", StatusCode: 200, Attempted: true,
	}, nil, late))
	updated := loadSchedulerOperation(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationQueued, updated.Status)
	require.Empty(t, updated.CallID)
	require.Nil(t, updated.SentAt)
	attempts = loadSchedulerAttempts(t, f.db, op.ID)
	require.Equal(t, gbmodels.PTZOperationAttemptUnknown, attempts[0].Status)
}

func TestSchedulerRebuildsCanonicalHomePositionBody(t *testing.T) {
	f := newSchedulerFixture(t, 1)
	op := f.createOperation(t, 1)
	op.CmdType = manscdp.CmdDeviceControl
	op.Action = "home_position"
	op.PayloadJSON = `{"enabled":true,"resetTime":0,"presetId":0}`
	require.NoError(t, f.db.Save(&op).Error)

	require.NoError(t, f.scheduler.RunDue(f.clock.Now()))
	f.dispatcher.Drain()
	calls := f.sender.Calls()
	require.Len(t, calls, 1)
	require.True(t, strings.Contains(calls[0].body, "<Enabled>1</Enabled>"))
	require.True(t, strings.Contains(calls[0].body, "<ResetTime>0</ResetTime>"))
	require.True(t, strings.Contains(calls[0].body, "<PresetIndex>0</PresetIndex>"))
}

func TestBuildScheduledPTZBodyPrecise2022(t *testing.T) {
	operation := gbmodels.GbPTZOperation{
		DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
		CmdType: manscdp.CmdDeviceControl, Action: "precise",
		ProfileVersion: string(protocol.Version2022), SN: 41,
		PayloadJSON: "{\"pan\":12.5,\"tilt\":-3.25,\"zoom\":4}",
	}
	body, err := buildScheduledPTZBody(operation)
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "<CmdType>DeviceControl</CmdType>")
	require.Contains(t, text, "<PTZPreciseCtrl>")
	require.Contains(t, text, "<Pan>12.5</Pan>")
	require.Contains(t, text, "<Tilt>-3.25</Tilt>")
	require.Contains(t, text, "<Zoom>4</Zoom>")
}

func TestBuildScheduledPTZBodyRecordUsesOperationProfileStreamNumber(t *testing.T) {
	for _, test := range []struct {
		name             string
		profileVersion   string
		action           string
		wantStreamNumber bool
	}{
		{name: "2016 start", profileVersion: string(protocol.Version2016), action: "record_start"},
		{name: "2016 stop", profileVersion: string(protocol.Version2016), action: "record_stop"},
		{name: "2022 start", profileVersion: string(protocol.Version2022), action: "record_start", wantStreamNumber: true},
		{name: "2022 stop", profileVersion: string(protocol.Version2022), action: "record_stop", wantStreamNumber: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation := gbmodels.GbPTZOperation{
				DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
				CmdType: manscdp.CmdDeviceControl, Action: test.action,
				ProfileVersion: test.profileVersion, SN: 41,
				PayloadJSON: `{"action":"` + test.action + `"}`,
			}
			body, err := buildScheduledPTZBody(operation)
			require.NoError(t, err)
			hasStreamNumber := strings.Contains(string(body), "<StreamNumber>0</StreamNumber>")
			require.Equal(t, test.wantStreamNumber, hasStreamNumber, string(body))
		})
	}
}

func TestBuildScheduledPTZBodyUsesPersistedProfileCharset(t *testing.T) {
	operation := gbmodels.GbPTZOperation{
		DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
		CmdType: manscdp.CmdDeviceControl, Action: "iframe",
		ProfileVersion: string(protocol.Version2016), ProfileCharset: string(protocol.CharsetUTF8), SN: 42,
		PayloadJSON: `{"action":"iframe"}`,
	}

	body, err := buildScheduledPTZBody(operation)
	require.NoError(t, err)
	require.Contains(t, string(body), `encoding="UTF-8"`)
	require.Contains(t, string(body), "<IFameCmd>Send</IFameCmd>")
}
