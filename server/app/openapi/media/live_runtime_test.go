package media

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
)

type runtimePlayerFunc func(context.Context, play.Request) (*play.Result, error)

func (f runtimePlayerFunc) EnsureLive(ctx context.Context, request play.Request) (*play.Result, error) {
	return f(ctx, request)
}

func TestLiveRuntimeRetirementWaitsForActualCallAndRejectsReplacement(t *testing.T) {
	runtime := NewLivePlayerRuntime()
	require.False(t, runtime.Ready())
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	old := runtimePlayerFunc(func(ctx context.Context, _ play.Request) (*play.Result, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return &play.Result{StreamID: "old"}, nil
	})
	require.NoError(t, runtime.Publish(old))
	require.True(t, runtime.Ready())
	done := make(chan error, 1)
	go func() { _, err := runtime.EnsureLive(context.Background(), play.Request{}); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, runtime.Retire(ctx), "cancellation is not evidence the actual call returned")
	require.False(t, runtime.Ready())
	<-cancelled
	next := runtimePlayerFunc(func(context.Context, play.Request) (*play.Result, error) { return &play.Result{StreamID: "new"}, nil })
	require.Error(t, runtime.Publish(next), "old dependency cannot be replaced before join")
	_, err := runtime.EnsureLive(context.Background(), play.Request{})
	require.Error(t, err, "retirement closes new admission immediately")
	close(release)
	require.Error(t, <-done)
	require.NoError(t, runtime.Retire(context.Background()))
	require.NoError(t, runtime.Publish(next))
	require.True(t, runtime.Ready())
	result, err := runtime.EnsureLive(context.Background(), play.Request{})
	require.NoError(t, err)
	require.Equal(t, "new", result.StreamID)
	require.NoError(t, runtime.Retire(context.Background()))
	require.False(t, runtime.Ready())
}

func TestLiveRuntimeRejectsCompletionBetweenRetirementAndCancellation(t *testing.T) {
	runtime := NewLivePlayerRuntime()
	started, finish := make(chan struct{}), make(chan struct{})
	require.NoError(t, runtime.Publish(runtimePlayerFunc(func(context.Context, play.Request) (*play.Result, error) {
		close(started)
		<-finish
		return &play.Result{StreamID: "old"}, nil
	})))
	cancelEntered, cancelContinue := make(chan struct{}), make(chan struct{})
	originalCancel := runtime.current.cancel
	runtime.current.cancel = func() { close(cancelEntered); <-cancelContinue; originalCancel() }
	done, retired := make(chan error, 1), make(chan error, 1)
	go func() { _, err := runtime.EnsureLive(context.Background(), play.Request{}); done <- err }()
	<-started
	go func() { retired <- runtime.Retire(context.Background()) }()
	<-cancelEntered
	close(finish)
	err := <-done
	close(cancelContinue)
	require.NoError(t, <-retired)
	require.Error(t, err, "retired flag, not asynchronous cancellation, is the admission boundary")
}
