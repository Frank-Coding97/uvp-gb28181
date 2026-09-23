package media

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderReusesOneSourceAndIsolatesConsumerLeases(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "stream-1", SSRC: "ssrc-1"}}
	provider := NewProvider(starter)

	first, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1"})
	require.NoError(t, err)
	second, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-b", DeviceID: "d1", ChannelID: "c1"})
	require.NoError(t, err)
	require.Equal(t, first.Source, second.Source)
	require.Equal(t, 1, starter.starts)
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
	lease, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1"})
	require.NoError(t, err)
	require.NoError(t, lease.Release(context.Background()))
	require.False(t, provider.HasLease("browser-stream"))
	require.Equal(t, 0, starter.stops, "provider must leave browser-owned cleanup to ZLM none-reader policy")
}

func TestProviderFailureLeavesNoLeaseAndCloseRejectsNewAcquires(t *testing.T) {
	starter := &fakeStarter{err: errors.New("device offline")}
	provider := NewProvider(starter)
	_, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1"})
	require.EqualError(t, err, "device offline")
	require.False(t, provider.HasLease("stream-1"))

	provider.Close()
	starter.err = nil
	_, err = provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1"})
	require.ErrorIs(t, err, ErrProviderClosed)
}

func TestNoneReaderPolicyHonorsLeasesBeforeExistingPolicy(t *testing.T) {
	starter := &fakeStarter{result: StartResult{StreamID: "stream-1"}}
	provider := NewProvider(starter)
	lease, err := provider.Acquire(context.Background(), AcquireRequest{ConsumerKey: "platform-a", DeviceID: "d1", ChannelID: "c1"})
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

func (f *fakeStarter) Start(context.Context, string, string) (StartResult, error) {
	f.starts++
	return f.result, f.err
}

type fakePolicy struct{ close bool }

func (f fakePolicy) ShouldCloseOnNoneReader(context.Context, string) (bool, error) {
	return f.close, nil
}
