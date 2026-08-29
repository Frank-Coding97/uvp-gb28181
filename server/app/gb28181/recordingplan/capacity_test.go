package recordingplan

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

func TestCapacityTenThousandDueStatesAreClaimedInBoundedBatches(t *testing.T) {
	db := newRepositoryTestDB(t)
	now := time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)
	states := make([]models.GbRecordingPlanChannelState, 10000)
	for i := range states {
		states[i] = models.GbRecordingPlanChannelState{
			ChannelID: uint(i + 1), DesiredState: models.RecordingDesiredRecording,
			ActualState: models.RecordingStateIdle, ReconcileAt: now,
		}
	}
	require.NoError(t, db.CreateInBatches(&states, 500).Error)
	started := time.Now()
	claimed, err := NewRepository(db).ClaimDueStates(context.Background(), "capacity", now, time.Minute, 200)
	require.NoError(t, err)
	require.Len(t, claimed, 200)
	var total int64
	require.NoError(t, db.Model(&models.GbRecordingPlanChannelState{}).Count(&total).Error)
	require.EqualValues(t, 10000, total)
	t.Logf("10k claim: batch=%d elapsed=%s", len(claimed), time.Since(started))
}

func TestCapacityDispatchUsesBoundedWorkersAndPerDeviceConcurrency(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}))
	now := time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)
	const channelCount = 24
	for i := 1; i <= channelCount; i++ {
		channel := models.GbChannel{
			DeviceID: "shared-device", ChannelID: fmt.Sprintf("C%03d", i), Status: models.ChannelStatusOnline,
			RecordingMode: models.RecordingModeContinuous,
		}
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{
			ChannelID: channel.ID, DesiredState: models.RecordingDesiredIdle,
			ActualState: models.RecordingStateIdle, ReconcileAt: now,
		}).Error)
	}
	operator := &capacityOperator{delay: 20 * time.Millisecond}
	engine := NewEngine(db, operator, EngineOptions{
		InstanceID: "bounded", BatchSize: 200, Workers: 6, DeviceConcurrency: 2,
		Now: func() time.Time { return now },
	})
	started := time.Now()
	require.NoError(t, engine.Dispatch(context.Background()))
	elapsed := time.Since(started)
	require.EqualValues(t, channelCount, operator.calls.Load())
	require.LessOrEqual(t, operator.maxActive.Load(), int64(6))
	require.Greater(t, operator.maxActive.Load(), int64(1))
	require.LessOrEqual(t, operator.maxByDevice.Load(), int64(2))
	require.Empty(t, engine.devices)
	t.Logf("bounded dispatch: channels=%d elapsed=%s max_global=%d max_device=%d", channelCount, elapsed, operator.maxActive.Load(), operator.maxByDevice.Load())
}

func TestCapacityTwoInstancesProduceOneEffectiveStart(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}))
	now := time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)
	channel := models.GbChannel{DeviceID: "D", ChannelID: "C", Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeContinuous}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, DesiredState: models.RecordingDesiredIdle, ActualState: models.RecordingStateIdle, ReconcileAt: now}).Error)
	operator := &capacityOperator{delay: 10 * time.Millisecond}
	engines := []*Engine{
		NewEngine(db, operator, EngineOptions{InstanceID: "instance-a", Workers: 1, Now: func() time.Time { return now }}),
		NewEngine(db, operator, EngineOptions{InstanceID: "instance-b", Workers: 1, Now: func() time.Time { return now }}),
	}
	start := make(chan struct{})
	errs := make(chan error, len(engines))
	var wait sync.WaitGroup
	for _, engine := range engines {
		wait.Add(1)
		go func(engine *Engine) {
			defer wait.Done()
			<-start
			errs <- engine.Dispatch(context.Background())
		}(engine)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, operator.calls.Load())
}

func TestCapacityRecorderOutageRecoversAfterFiveMinutes(t *testing.T) {
	db := newRepositoryTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}))
	now := time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)
	channel := models.GbChannel{DeviceID: "D", ChannelID: "C", Status: models.ChannelStatusOnline, RecordingMode: models.RecordingModeContinuous}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&models.GbRecordingPlanChannelState{ChannelID: channel.ID, DesiredState: models.RecordingDesiredIdle, ActualState: models.RecordingStateIdle, ReconcileAt: now}).Error)
	operator := &recoveringOperator{failuresRemaining: 9}
	engine := NewEngine(db, operator, EngineOptions{InstanceID: "recovery", Workers: 1, Now: func() time.Time { return now }})
	for attempt := 0; attempt < 10; attempt++ {
		require.NoError(t, engine.Dispatch(context.Background()))
		var state models.GbRecordingPlanChannelState
		require.NoError(t, db.First(&state, "channel_id = ?", channel.ID).Error)
		if state.ActualState == models.RecordingStateRecording {
			break
		}
		require.NotNil(t, state.NextRetryAt)
		now = *state.NextRetryAt
	}
	var state models.GbRecordingPlanChannelState
	require.NoError(t, db.First(&state, "channel_id = ?", channel.ID).Error)
	require.Equal(t, models.RecordingStateRecording, state.ActualState)
	require.Zero(t, state.AttemptCount)
	require.GreaterOrEqual(t, now.Sub(time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)), 5*time.Minute)
	var openGaps int64
	require.NoError(t, db.Model(&models.GbRecordingPlanGap{}).Where("channel_id = ? AND ended_at IS NULL", channel.ID).Count(&openGaps).Error)
	require.Zero(t, openGaps)
	var gap models.GbRecordingPlanGap
	require.NoError(t, db.Where("channel_id = ?", channel.ID).First(&gap).Error)
	require.Nil(t, gap.PlanID, "continuous recording gaps must not persist a synthetic plan id")
	t.Logf("recorder recovery: outage=%s attempts=%d", now.Sub(time.Date(2026, 8, 29, 18, 0, 0, 0, time.UTC)), operator.calls)
}

func TestCapacityBoundaryJitterIsDeterministicAndBounded(t *testing.T) {
	const maximum = 5 * time.Second
	values := make([]time.Duration, 0, 10000)
	for channelID := uint(1); channelID <= 10000; channelID++ {
		value := deterministicJitter(channelID, maximum)
		require.Equal(t, value, deterministicJitter(channelID, maximum))
		require.GreaterOrEqual(t, value, time.Duration(0))
		require.Less(t, value, maximum)
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	require.GreaterOrEqual(t, values[len(values)-1]-values[0], 4990*time.Millisecond)
	t.Logf("boundary jitter: p95=%s max=%s", values[len(values)*95/100], values[len(values)-1])
}

type capacityOperator struct {
	delay       time.Duration
	calls       atomic.Int64
	active      atomic.Int64
	maxActive   atomic.Int64
	device      atomic.Int64
	maxByDevice atomic.Int64
}

func (o *capacityOperator) Start(_ context.Context, target ChannelTarget) (*play.Result, error) {
	o.calls.Add(1)
	active := o.active.Add(1)
	updateMaximum(&o.maxActive, active)
	deviceActive := o.device.Add(1)
	updateMaximum(&o.maxByDevice, deviceActive)
	time.Sleep(o.delay)
	o.device.Add(-1)
	o.active.Add(-1)
	return &play.Result{StreamID: target.ChannelCode, Generation: 1}, nil
}

func (o *capacityOperator) Stop(context.Context, uint) error { return nil }

func updateMaximum(maximum *atomic.Int64, candidate int64) {
	for current := maximum.Load(); candidate > current; current = maximum.Load() {
		if maximum.CompareAndSwap(current, candidate) {
			return
		}
	}
}

type recoveringOperator struct {
	mu                sync.Mutex
	failuresRemaining int
	calls             int
}

func (o *recoveringOperator) Start(_ context.Context, target ChannelTarget) (*play.Result, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	if o.failuresRemaining > 0 {
		o.failuresRemaining--
		return nil, &OrchestrationError{Stage: FailureRecordStart, Err: errors.New("zlm unavailable")}
	}
	return &play.Result{StreamID: target.ChannelCode, Generation: 1}, nil
}

func (o *recoveringOperator) Stop(context.Context, uint) error { return nil }
