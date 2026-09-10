package play

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
)

func TestCoordinatorPinBlocksEveryStopUntilRelease(t *testing.T) {
	ctx := context.Background()
	stops := 0
	c := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) { return nil, nil }, func(context.Context, *Result) error { stops++; return nil })
	req := Request{DeviceID: "d", ChannelID: "c"}
	result := &Result{StreamID: "s", SSRC: "1", Generation: 1}
	require.True(t, c.Restore(req, result))
	ref := stream.LiveRef{StreamID: "s", SSRC: "1", Generation: 1}
	release, err := c.PinIfCurrent(ref)
	require.NoError(t, err)
	require.ErrorIs(t, c.Stop(ctx, req), ErrLivePinned)
	handled, err := c.StopStream(ctx, "s")
	require.True(t, handled)
	require.ErrorIs(t, err, ErrLivePinned)
	handled, err = c.StopIfCurrent(ctx, ref)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrLivePinned)
	require.Zero(t, stops)
	reused, err := c.EnsureLive(ctx, req)
	require.NoError(t, err)
	require.Equal(t, uint64(1), reused.Generation)
	release()
	release() // a deferred and explicit release cannot decrement twice.
	require.NoError(t, c.Stop(ctx, req))
	require.Equal(t, 1, stops)
}
func TestCoordinatorPinRejectsStaleOrStoppingGeneration(t *testing.T) {
	c := NewCoordinator(func(context.Context, Request) (*Result, error) { return nil, nil })
	req := Request{DeviceID: "d", ChannelID: "c"}
	require.True(t, c.Restore(req, &Result{StreamID: "s", SSRC: "2", Generation: 2}))
	release, err := c.PinIfCurrent(stream.LiveRef{StreamID: "s", SSRC: "1", Generation: 1})
	require.ErrorIs(t, err, ErrLiveGenerationChanged)
	require.Nil(t, release)
	c.mu.Lock()
	c.entries[coordinatorKey{deviceID: "d", channelID: "c"}].state = LiveStateStopping
	c.mu.Unlock()
	release, err = c.PinIfCurrent(stream.LiveRef{StreamID: "s", SSRC: "2", Generation: 2})
	require.ErrorIs(t, err, ErrLiveGenerationChanged)
	require.Nil(t, release)
}
func TestCoordinatorMultiplePinsMustAllRelease(t *testing.T) {
	c := NewCoordinator(func(context.Context, Request) (*Result, error) { return nil, nil })
	req := Request{DeviceID: "d", ChannelID: "c"}
	ref := stream.LiveRef{StreamID: "s", SSRC: "1", Generation: 1}
	require.True(t, c.Restore(req, &Result{StreamID: "s", SSRC: "1", Generation: 1}))
	releaseA, err := c.PinIfCurrent(ref)
	require.NoError(t, err)
	releaseB, err := c.PinIfCurrent(ref)
	require.NoError(t, err)
	releaseA()
	require.ErrorIs(t, c.Stop(context.Background(), req), ErrLivePinned)
	releaseB()
	require.NoError(t, c.Stop(context.Background(), req))
}
