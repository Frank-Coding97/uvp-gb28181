package recordingplan

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

type LiveEnsurer interface {
	EnsureLive(context.Context, play.Request) (*play.Result, error)
}

type RecordingLifecycle interface {
	Enable(context.Context, uint) (*models.GbChannel, error)
	BeginPlayback(context.Context, string) error
	Disable(context.Context, uint) (*models.GbChannel, error)
}

type ConditionalGenerationStopper interface {
	StopOnNoneReader(context.Context, stream.LiveRef) (bool, error)
}

type SourceLeaseChecker interface {
	HasLease(string) bool
}

type ChannelTarget struct {
	ID          uint
	DeviceCode  string
	ChannelCode string
}

type OrchestrationError struct {
	Stage string
	Err   error
}

func (e *OrchestrationError) Error() string { return fmt.Sprintf("%s: %v", e.Stage, e.Err) }
func (e *OrchestrationError) Unwrap() error { return e.Err }

type activePlanRecording struct {
	result *play.Result
	lease  *play.SourceLease
}

type channelLock struct {
	mutex sync.Mutex
	refs  int
}

type Orchestrator struct {
	live       LiveEnsurer
	recording  RecordingLifecycle
	leases     *play.SourceLeaseRegistry
	stopper    ConditionalGenerationStopper
	allLeases  SourceLeaseChecker
	activeMu   sync.RWMutex
	active     map[uint]activePlanRecording
	channelMu  sync.Mutex
	channelMux map[uint]*channelLock
}

func NewOrchestrator(live LiveEnsurer, recording RecordingLifecycle, leases *play.SourceLeaseRegistry, stopper ConditionalGenerationStopper) *Orchestrator {
	if leases == nil {
		leases = play.NewSourceLeaseRegistry()
	}
	return &Orchestrator{live: live, recording: recording, leases: leases, stopper: stopper, active: make(map[uint]activePlanRecording), channelMux: make(map[uint]*channelLock)}
}

func (o *Orchestrator) SetCombinedLeaseChecker(checker SourceLeaseChecker) { o.allLeases = checker }

func (o *Orchestrator) Start(ctx context.Context, target ChannelTarget) (*play.Result, error) {
	unlock := o.lockChannel(target.ID)
	defer unlock()
	if current, ok := o.current(target.ID); ok {
		result := *current.result
		result.Reused = true
		return &result, nil
	}
	if _, err := o.recording.Enable(ctx, target.ID); err != nil {
		return nil, &OrchestrationError{Stage: FailureRecordStart, Err: err}
	}
	result, err := o.live.EnsureLive(ctx, play.Request{DeviceID: target.DeviceCode, ChannelID: target.ChannelCode, Trigger: "recording-plan"})
	if err != nil {
		stage := FailureStreamStart
		if errors.Is(err, play.ErrStreamNotReady) || errors.Is(err, play.ErrPlayTimeout) {
			stage = FailureMediaWait
		}
		return nil, &OrchestrationError{Stage: stage, Err: err}
	}
	if result == nil || result.StreamID == "" || result.Generation == 0 {
		return nil, &OrchestrationError{Stage: FailureMediaWait, Err: errors.New("媒体流缺少有效代际")}
	}
	lease := o.leases.Acquire(result.StreamID, result.Generation, fmt.Sprintf("recording-plan:%d", target.ID))
	if err := o.recording.BeginPlayback(ctx, result.StreamID); err != nil {
		_ = lease.Release()
		return nil, &OrchestrationError{Stage: FailureRecordStart, Err: err}
	}
	o.activeMu.Lock()
	o.active[target.ID] = activePlanRecording{result: result, lease: lease}
	o.activeMu.Unlock()
	copyResult := *result
	return &copyResult, nil
}

func (o *Orchestrator) Stop(ctx context.Context, channelID uint) error {
	unlock := o.lockChannel(channelID)
	defer unlock()
	if _, err := o.recording.Disable(ctx, channelID); err != nil {
		return &OrchestrationError{Stage: FailureRecordStart, Err: err}
	}
	current, ok := o.current(channelID)
	if !ok {
		return nil
	}
	if err := current.lease.Release(); err != nil {
		return err
	}
	o.activeMu.Lock()
	delete(o.active, channelID)
	o.activeMu.Unlock()
	if o.stopper == nil || (o.allLeases != nil && o.allLeases.HasLease(current.result.StreamID)) {
		return nil
	}
	nodeID := int64(0)
	if current.result.Node != nil {
		nodeID = current.result.Node.ID
	}
	_, err := o.stopper.StopOnNoneReader(ctx, stream.LiveRef{
		StreamID: current.result.StreamID, SSRC: current.result.SSRC,
		Generation: current.result.Generation, NodeID: nodeID,
	})
	return err
}

func (o *Orchestrator) current(channelID uint) (activePlanRecording, bool) {
	o.activeMu.RLock()
	current, ok := o.active[channelID]
	o.activeMu.RUnlock()
	return current, ok
}

func (o *Orchestrator) lockChannel(channelID uint) func() {
	o.channelMu.Lock()
	lock := o.channelMux[channelID]
	if lock == nil {
		lock = &channelLock{}
		o.channelMux[channelID] = lock
	}
	lock.refs++
	o.channelMu.Unlock()
	lock.mutex.Lock()
	return func() {
		lock.mutex.Unlock()
		o.channelMu.Lock()
		lock.refs--
		if lock.refs == 0 && o.channelMux[channelID] == lock {
			delete(o.channelMux, channelID)
		}
		o.channelMu.Unlock()
	}
}
