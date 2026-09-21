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
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
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

func newSchedulerFixture(t *testing.T, capacity int, managedDriver ...bool) *schedulerFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scheduler.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	var db *gorm.DB
	if len(managedDriver) != 0 && managedDriver[0] {
		db = authoritytest.OpenSQLite(t, dsn)
	} else {
		var err error
		db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	// 显式设置 WAL 与 busy_timeout:并发调度测试下多个 goroutine 同时读写,
	// 缺省 DELETE 日志模式会让读与写互斥并立即报 database is locked
	require.NoError(t, db.Exec("PRAGMA journal_mode=WAL").Error)
	require.NoError(t, db.Exec("PRAGMA busy_timeout=5000").Error)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}))
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "D", IP: "192.0.2.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline}).Error)
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

// ⛔ 回归锚点（2026-09-19 真机验证发现）：`VideoParamAttribute`（A-5）与配置族共用
// `CmdType=DeviceConfig`，重建时**只能按 Action 分流**。曾经只看 CmdType，把所有配置族
// 下发都重建成 `<VideoParamAttribute Num="0">`（一块配置都没有），而设备照回 `Result=OK`、
// 平台回读只表现为 `mismatch` —— 遮挡 / 镜像 / OSD / 基本参数 / 录像计划 / 报警上报**整族失效**，
// 且症状酷似"设备没照做"。
func TestBuildScheduledPTZBodyDeviceConfigSplitsByAction(t *testing.T) {
	apply := gbmodels.GbPTZOperation{
		DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
		CmdType: manscdp.CmdDeviceConfig, Action: ActionApplyDeviceConfig,
		ProfileVersion: string(protocol.Version2022), SN: 795,
		PayloadJSON: `{"configTypes":["PictureMask"],"blocks":{"pictureMask":{"on":1,` +
			`"regions":[{"seq":1,"left":10,"top":10,"right":100,"bottom":100}]}}}`,
	}
	body, err := buildScheduledPTZBody(apply)
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "<PictureMask>", "配置族下发必须发自己的块: %s", text)
	require.Contains(t, text, "<On>1</On>", text)
	require.Contains(t, text, "<Seq>1</Seq>", text)
	require.Contains(t, text, "<Point>10,10,100,100</Point>", text)
	require.NotContains(t, text, "VideoParamAttribute", "别把配置族重建成 A-5 的报文: %s", text)

	// A-5 自己那一路不能被这次改动带走。
	videoParam := gbmodels.GbPTZOperation{
		DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
		CmdType: manscdp.CmdDeviceConfig, Action: actionApplyVideoParams,
		ProfileVersion: string(protocol.Version2022), SN: 796,
		PayloadJSON: `{"items":[{"streamNumber":0,"videoFormat":"2",` +
			`"resolution":"5","frameRate":"25","bitRateType":"1","videoBitRate":"2048"}]}`,
	}
	body, err = buildScheduledPTZBody(videoParam)
	require.NoError(t, err)
	text = string(body)
	require.Contains(t, text, "<VideoParamAttribute", text)
	require.Contains(t, text, `<VideoParamAttribute Num="1">`, text)
	require.Contains(t, text, "<Resolution>5</Resolution>", text)

	// 配置族形态却缺 blocks：**必须报错，不许发空块** —— 发空块会让故障又变成"设备没照做"。
	broken := apply
	broken.PayloadJSON = `{"configTypes":["PictureMask"]}`
	_, err = buildScheduledPTZBody(broken)
	require.Error(t, err)
}

// ⛔⛔ 回归锚点（2026-09-20 海康真机定位）：**抓拍会话的报文从未真正发出过**。
//
// `snapshot_config` 这个 action 当时只作为一个字面量写在 `controllers/device_snapshot.go` 里，
// `ptz` 包完全不认识它 —— 于是重建落到 A-5 分支，平台实际发给设备的是
// `<VideoParamAttribute Num="0">`（一块配置都没有）。设备照回 `<Result>OK</Result>`、
// operation 记 accepted、回读还判 read_ok，**唯一的症状是"设备没上传图片"**。
//
// trace 取证（`gb_sip_trace_message`，设备 37010301021320000002）：
//
//	SN=10179 / 10181 / 10183 三条 outbound MESSAGE 的正文**全部**是
//	<Control><CmdType>DeviceConfig</CmdType><SN>…</SN><DeviceID>…</DeviceID>
//	<VideoParamAttribute Num="0"></VideoParamAttribute></Control>
//	而对应 operation 的 action 都是 `snapshot_config`。
//
// ⭐ 用例刻意写成**性质**（"payload 里装了 blocks 就不许重建成 A-5"）而不是"再补一条 action"：
// 判据走的是 payload 这个**数据事实**，所以将来任何新增的配置族 action 都自动被覆盖 ——
// action 白名单会漏，数据事实不会。第三个 action 名是故意不存在的，用来代表"未来的新 action"。
func TestBuildScheduledPTZBodyBlocksPayloadNeverRebuildsAsVideoParamAttribute(t *testing.T) {
	payload := `{"configTypes":["SnapShotConfig"],"blocks":{"snapShot":{` +
		`"snapNum":3,"interval":3,` +
		`"uploadUrl":"http://192.168.10.120:8280/api/gb28181/device-snapshots/uploads/tok/",` +
		`"sessionId":"` + strings.Repeat("s", 32) + `"}}}`
	for _, action := range []string{
		ActionApplyDeviceConfig,
		ActionSnapshotConfig,
		"some_future_config_family_action",
	} {
		t.Run(action, func(t *testing.T) {
			operation := gbmodels.GbPTZOperation{
				DeviceCode: "D", ChannelCode: "C", TargetCode: "C",
				CmdType: manscdp.CmdDeviceConfig, Action: action,
				ProfileVersion: string(protocol.Version2022), SN: 10183,
				PayloadJSON: payload,
			}
			body, err := buildScheduledPTZBody(operation)
			require.NoError(t, err, "带 blocks 的配置族报文必须能重建")
			text := string(body)
			require.NotContains(t, text, "VideoParamAttribute",
				"带 blocks 的配置族下发被重建成 A-5 报文 ⇒ 一块配置都发不出去: %s", text)
			// ⛔ 下发侧元素名是 `SnapShotConfig`（A.2.3.2.12），应答侧才是 `SnapShot`（A.2.6.9）。
			require.Contains(t, text, "<SnapShotConfig>", text)
			require.Contains(t, text, "<SnapNum>3</SnapNum>", text)
			require.Contains(t, text, "<Interval>3</Interval>", text)
			require.Contains(t, text, "<SessionID>", text)
			require.Contains(t, text, "<UploadURL>", text)
		})
	}
}

// 抓拍会话的 action 必须被登记为"配置族 action"。
//
// ⛔ 这条防的是**名单漏项**这个更隐蔽的一半：`deviceConfigBlockActions` 只影响
// "payload 里没有 blocks 时报不报错"。如果新 action 忘了登记，带 blocks 的正常路径
// 仍然对（上面那条性质用例保证），但 payload 一旦缺 blocks 就会**静默回落成 A-5 报文**
// —— 又变回"设备回 OK、平台记 accepted、配置没传出去"。
func TestSnapshotConfigActionIsDeclaredAsBlockFamily(t *testing.T) {
	require.NotEmpty(t, ActionSnapshotConfig)
	_, declared := deviceConfigBlockActions[ActionSnapshotConfig]
	require.True(t, declared, "抓拍会话 action 必须登记进 deviceConfigBlockActions")

	// 缺 blocks 时必须报错，不许回落成 A-5 报文。
	_, blockFamily, err := deviceConfigOperationForm(ActionSnapshotConfig, `{"configTypes":["SnapShotConfig"]}`)
	require.True(t, blockFamily)
	require.Error(t, err)

	// 反过来：A-5 那条路（payload 装 `items`）不许被误判成配置族。
	_, blockFamily, err = deviceConfigOperationForm(actionApplyVideoParams, `{"items":[]}`)
	require.NoError(t, err)
	require.False(t, blockFamily, "A-5 视频参数不能被当成配置族")
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
