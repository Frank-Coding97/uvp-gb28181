package media

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
)

func TestProviderReusesOneSourceAndIsolatesConsumerLeases(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "stream-1", SSRC: "ssrc-1"}}
	provider := NewProvider(starter)

	first, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	second, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-b", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	require.Equal(t, first.Source, second.Source)
	require.Equal(t, 2, starter.starts, "each consumer must pass the playback authorization check")
	require.True(t, provider.HasLease("stream-1"))
	require.Equal(t, 2, provider.LeaseCount("stream-1"))

	require.NoError(t, first.Release(context.Background()))
	require.True(t, provider.HasLease("stream-1"), "releasing A must not clear B")
	require.Equal(t, 1, provider.LeaseCount("stream-1"))
	require.NoError(t, first.Release(context.Background()), "release is idempotent")
	require.NoError(t, second.Release(context.Background()))
	require.False(t, provider.HasLease("stream-1"))
}

func TestProviderDoesNotOwnBrowserReusedSource(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "browser-stream", Reused: true}}
	provider := NewProvider(starter)
	lease, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	require.NoError(t, lease.Release(context.Background()))
	require.False(t, provider.HasLease("browser-stream"))
	require.Equal(t, 0, starter.stops, "provider must leave browser-owned cleanup to ZLM none-reader policy")
}

func TestProviderFailureLeavesNoLeaseAndCloseRejectsNewAcquires(t *testing.T) {
	starter := &fakeStarter{err: errors.New("device offline")}
	provider := NewProvider(starter)
	_, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.EqualError(t, err, "device offline")
	require.False(t, provider.HasLease("stream-1"))

	provider.Close()
	starter.err = nil
	_, err = provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.ErrorIs(t, err, ErrProviderClosed)
}

func TestNoneReaderPolicyHonorsLeasesBeforeExistingPolicy(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "stream-1"}}
	provider := NewProvider(starter)
	lease, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	delegate := fakePolicy{close: true}
	policy := NoneReaderPolicy{Leases: provider, Delegate: delegate}
	closeStream, err := policy.ShouldCloseOnNoneReader(context.Background(), "stream-1")
	require.NoError(t, err)
	require.False(t, closeStream)
	require.NoError(t, lease.Release(context.Background()))
	closeStream, err = policy.ShouldCloseOnNoneReader(context.Background(), "stream-1")
	require.NoError(t, err)
	require.True(t, closeStream)
}

type fakeStarter struct {
	result StartResult
	err    error
	starts int
	stops  int
}

func (f *fakeStarter) Start(context.Context, AcquireRequest) (StartResult, error) {
	f.starts++
	return f.result, f.err
}

type fakePolicy struct{ close bool }

func (f fakePolicy) ShouldCloseOnNoneReader(context.Context, string) (bool, error) {
	return f.close, nil
}

func TestProviderRechecksAuthorizationBeforeReusingSource(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "shared-source"}}
	provider := NewProvider(starter)
	first, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "first", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	starter.err = errors.New("device authorization revoked")
	_, err = provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "second", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.EqualError(t, err, "device authorization revoked")
	require.Equal(t, 1, provider.LeaseCount("shared-source"))
	require.NoError(t, first.Release(context.Background()))
}

func TestProviderTracksNewSourceWithoutLosingOldLease(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "old-source"}}
	provider := NewProvider(starter)
	first, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "first", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	starter.result = StartResult{StreamID: "new-source"}
	second, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "second", DeviceID: "d1", ChannelID: "c1", DeviceEpoch: 1})
	require.NoError(t, err)
	require.Equal(t, "new-source", second.Source.StreamID)
	require.NoError(t, first.Release(context.Background()))
	require.False(t, provider.HasLease("old-source"))
	require.True(t, provider.HasLease("new-source"))
	require.NoError(t, second.Release(context.Background()))
}

type liveEnsurerFunc func(context.Context, play.Request) (*play.Result, error)

func (f liveEnsurerFunc) EnsureLive(ctx context.Context, req play.Request) (*play.Result, error) {
	return f(ctx, req)
}

func TestPlayServiceAdapterPassesAuthorizedEpochToInternalPlayback(t *testing.T) {
	var captured play.Request
	adapter := NewPlayServiceAdapter(liveEnsurerFunc(func(_ context.Context, req play.Request) (*play.Result, error) {
		captured = req
		return &play.Result{StreamID: "live-source", App: "rtp", SSRC: "123", Node: &play.ResultNode{ID: 7}, Reused: true}, nil
	}))
	result, err := adapter.Start(context.Background(), AcquireRequest{DeviceID: "device", ChannelID: "channel", DeviceEpoch: 3})
	require.NoError(t, err)
	require.Equal(t, play.Request{DeviceID: "device", ChannelID: "channel", DeviceEpoch: 3, Trigger: "cascade"}, captured)
	require.Equal(t, StartResult{StreamID: "live-source", App: "rtp", SSRC: "123", NodeID: 7, Reused: true}, result)
	for _, epoch := range []int64{0, -1} {
		_, err = adapter.Start(context.Background(), AcquireRequest{DeviceID: "device", ChannelID: "channel", DeviceEpoch: epoch})
		require.ErrorIs(t, err, play.ErrPlayAuthorizationUnavailable)
		require.EqualValues(t, 3, captured.DeviceEpoch, "invalid epochs must not reach playback")
	}
}
