package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestCoordinatorBeginShutdownRejectsEnsureButStillAllowsStop(t *testing.T) {
	var starts atomic.Int32
	var stops atomic.Int32
	result := &Result{StreamID: "shutdown-stream", SSRC: "0200000001", Generation: 1}
	coordinator := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) {
			starts.Add(1)
			return result, nil
		},
		func(context.Context, *Result) error {
			stops.Add(1)
			return nil
		},
	)
	request := coordinatorRequest("shutdown-device", "shutdown-channel")
	if _, err := coordinator.EnsureLive(context.Background(), request); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	if !coordinator.BeginShutdown() {
		t.Fatal("first BeginShutdown should close admission")
	}
	if coordinator.BeginShutdown() {
		t.Fatal("second BeginShutdown should be idempotent")
	}
	if _, err := coordinator.EnsureLive(context.Background(), request); !errors.Is(err, ErrLiveShutdown) {
		t.Fatalf("ready ensure after shutdown err=%v, want ErrLiveShutdown", err)
	}
	if err := coordinator.Stop(context.Background(), request); err != nil {
		t.Fatalf("stop after shutdown: %v", err)
	}
	if starts.Load() != 1 || stops.Load() != 1 {
		t.Fatalf("starts=%d stops=%d, want 1/1", starts.Load(), stops.Load())
	}
}

func TestServiceBeginShutdownRejectsDirectStart(t *testing.T) {
	service := &Service{}
	service.BeginShutdown()
	if _, err := service.Start(context.Background(), "shutdown-device", "shutdown-channel"); !errors.Is(err, ErrLiveShutdown) {
		t.Fatalf("direct start after shutdown err=%v, want ErrLiveShutdown", err)
	}
}

func TestServiceShutdownWaitsAcceptedStartAndDrainsAllStreams(t *testing.T) {
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	result := &Result{StreamID: "accepted-stream", SSRC: "0200000002", Generation: 2}
	var stopCalls atomic.Int32
	coordinator := NewCoordinatorWithStop(
		func(ctx context.Context, _ Request) (*Result, error) {
			close(startEntered)
			select {
			case <-releaseStart:
				return result, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		func(context.Context, *Result) error {
			stopCalls.Add(1)
			return nil
		},
	)
	channels := &shutdownTestChannels{list: gbmodels.GbChannelList{{StreamID: result.StreamID}}}
	service := &Service{channels: channels, liveCoordinator: coordinator}
	request := coordinatorRequest("accepted-device", "accepted-channel")

	startDone := make(chan error, 1)
	go func() {
		_, err := service.EnsureLive(context.Background(), request)
		startDone <- err
	}()
	<-startEntered

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- service.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before accepted start completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if channels.listCalls.Load() != 0 {
		t.Fatal("shutdown listed channels before the accepted start completed")
	}
	if _, err := service.EnsureLive(context.Background(), request); !errors.Is(err, ErrLiveShutdown) {
		t.Fatalf("ensure during shutdown err=%v, want ErrLiveShutdown", err)
	}

	close(releaseStart)
	if err := <-startDone; err != nil {
		t.Fatalf("accepted start: %v", err)
	}
	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if channels.listCalls.Load() != 1 || stopCalls.Load() != 1 {
		t.Fatalf("list calls=%d stop calls=%d, want 1/1", channels.listCalls.Load(), stopCalls.Load())
	}

	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated shutdown: %v", err)
	}
	if channels.listCalls.Load() != 1 || stopCalls.Load() != 1 {
		t.Fatalf("repeated shutdown duplicated cleanup: list=%d stop=%d", channels.listCalls.Load(), stopCalls.Load())
	}
}

func TestServiceShutdownStopsEveryListedStreamAndContinuesAfterErrors(t *testing.T) {
	stopErr1 := errors.New("stop stream one")
	stopErr3 := errors.New("stop stream three")
	listErr := errors.New("channel list incomplete")
	results := map[string]*Result{
		"stream-one":   {StreamID: "stream-one", SSRC: "0200000003", Generation: 3},
		"stream-two":   {StreamID: "stream-two", SSRC: "0200000004", Generation: 4},
		"stream-three": {StreamID: "stream-three", SSRC: "0200000005", Generation: 5},
	}
	var stopCalls atomic.Int32
	coordinator := NewCoordinatorWithStop(
		func(_ context.Context, req Request) (*Result, error) { return results[req.ChannelID], nil },
		func(_ context.Context, result *Result) error {
			stopCalls.Add(1)
			switch result.StreamID {
			case "stream-one":
				return stopErr1
			case "stream-three":
				return stopErr3
			default:
				return nil
			}
		},
	)
	channels := &shutdownTestChannels{list: gbmodels.GbChannelList{
		{StreamID: "stream-one"}, {StreamID: "stream-two"}, {StreamID: "stream-three"},
	}, listErr: listErr}
	service := &Service{channels: channels, liveCoordinator: coordinator}
	for _, channelID := range []string{"stream-one", "stream-two", "stream-three"} {
		if _, err := coordinator.EnsureLive(context.Background(), coordinatorRequest("device", channelID)); err != nil {
			t.Fatalf("ensure %s: %v", channelID, err)
		}
	}

	err := service.Shutdown(context.Background())
	if !errors.Is(err, listErr) || !errors.Is(err, stopErr1) || !errors.Is(err, stopErr3) {
		t.Fatalf("shutdown err=%v, want list and both stop errors", err)
	}
	if stopCalls.Load() != 3 {
		t.Fatalf("stop calls=%d, want all 3 streams attempted", stopCalls.Load())
	}
	if err := service.Shutdown(context.Background()); !errors.Is(err, listErr) || !errors.Is(err, stopErr1) || !errors.Is(err, stopErr3) {
		t.Fatalf("repeated shutdown err=%v, want shared errors", err)
	}
	if stopCalls.Load() != 3 || channels.listCalls.Load() != 1 {
		t.Fatalf("repeated shutdown duplicated work: stops=%d lists=%d", stopCalls.Load(), channels.listCalls.Load())
	}
}

func TestServiceShutdownHonorsDeadlineWhileAcceptedStartIsRunning(t *testing.T) {
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	coordinator := NewCoordinator(func(ctx context.Context, _ Request) (*Result, error) {
		close(startEntered)
		select {
		case <-releaseStart:
			return &Result{StreamID: "deadline-stream"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	channels := &shutdownTestChannels{}
	service := &Service{channels: channels, liveCoordinator: coordinator}
	startDone := make(chan error, 1)
	go func() {
		_, err := service.EnsureLive(context.Background(), coordinatorRequest("deadline-device", "deadline-channel"))
		startDone <- err
	}()
	<-startEntered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown err=%v, want context deadline", err)
	}
	if channels.listCalls.Load() != 0 {
		t.Fatal("deadline shutdown should not enumerate before accepted start drains")
	}
	close(releaseStart)
	if err := <-startDone; err != nil {
		t.Fatalf("accepted start after deadline: %v", err)
	}
}

func TestServiceShutdownHonorsDeadlineDuringStop(t *testing.T) {
	stopEntered := make(chan struct{})
	result := &Result{StreamID: "stop-deadline-stream", SSRC: "0200000006", Generation: 6}
	coordinator := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) { return result, nil },
		func(ctx context.Context, _ *Result) error {
			close(stopEntered)
			<-ctx.Done()
			return ctx.Err()
		},
	)
	if _, err := coordinator.EnsureLive(context.Background(), coordinatorRequest("stop-deadline-device", "stop-deadline-channel")); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	channels := &shutdownTestChannels{list: gbmodels.GbChannelList{{StreamID: result.StreamID}}}
	service := &Service{channels: channels, liveCoordinator: coordinator}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown err=%v, want context deadline", err)
	}
	select {
	case <-stopEntered:
	case <-time.After(time.Second):
		t.Fatal("stop callback was not invoked")
	}
}

type shutdownTestChannels struct {
	list      gbmodels.GbChannelList
	listErr   error
	listCalls atomic.Int32
	mu        sync.Mutex
}

func (r *shutdownTestChannels) FindChannel(context.Context, string, string) (*gbmodels.GbChannel, error) {
	return nil, nil
}

func (r *shutdownTestChannels) FindChannelByStream(context.Context, string) (*gbmodels.GbChannel, error) {
	return nil, nil
}

func (r *shutdownTestChannels) UpdateStream(context.Context, string, string, string) error {
	return nil
}

func (r *shutdownTestChannels) ClearStream(context.Context, string) error { return nil }

func (r *shutdownTestChannels) SetCurrent(context.Context, string, string, string, string) error {
	return nil
}

func (r *shutdownTestChannels) ClearIfCurrent(context.Context, string, string) (bool, error) {
	return true, nil
}

func (r *shutdownTestChannels) ListPlayingChannels(context.Context) (gbmodels.GbChannelList, error) {
	r.listCalls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	return append(gbmodels.GbChannelList(nil), r.list...), r.listErr
}
