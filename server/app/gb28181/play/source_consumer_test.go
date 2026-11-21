package play

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
)

func sourceConsumerFixture(t *testing.T, start StartFunc) (*Service, *Coordinator, *SourceLeaseRegistry, *atomic.Int32) {
	t.Helper()
	withFixedAddressPlaybackSettings(t, false, false)
	withPlayAuthorization(t, false, false)
	stops := &atomic.Int32{}
	c := NewCoordinatorWithStop(start, func(context.Context, *Result) error { stops.Add(1); return nil })
	registry := NewSourceLeaseRegistry()
	channel := &gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", StreamID: "stream", OnDemandLive: true}
	s := &Service{liveCoordinator: c, channels: &fakeChannels{c: channel}, zlm: &readerCountZLM{readers: 0}}
	s.SetSourceLeaseChecker(registry)
	return s, c, registry, stops
}

func consumerResult() *Result { return &Result{StreamID: "stream", SSRC: "0200000001", Generation: 7} }
func consumerRequest() AuthorizedRequest {
	return AuthorizedRequest{DeviceID: "device", ChannelID: "channel"}
}

func TestSourceConsumerReadyLeaseBlocksNoneReaderButNotManagementStop(t *testing.T) {
	s, c, registry, stops := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) { return consumerResult(), nil })
	require.True(t, c.Restore(Request{DeviceID: "device", ChannelID: "channel"}, consumerResult()))
	result, lease, err := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:ready")
	require.NoError(t, err)
	require.True(t, registry.HasLease(result.StreamID))
	stopped, err := s.StopOnNoneReader(context.Background(), resultLiveRef(result))
	require.NoError(t, err)
	require.False(t, stopped)
	require.Zero(t, stops.Load())
	_, err = s.StopIfCurrent(context.Background(), resultLiveRef(result))
	require.NoError(t, err)
	require.EqualValues(t, 1, stops.Load(), "management stop must override consumer ownership")
	require.NoError(t, lease.Release())
	require.False(t, registry.HasLease(result.StreamID))
}

func TestSourceConsumerStartingWaiterIsLeasedBeforeReadyPublication(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	s, c, registry, stops := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) {
		close(started)
		<-finish
		return consumerResult(), nil
	})
	ownerDone := make(chan error, 1)
	go func() { _, err := s.StartAuthorized(context.Background(), consumerRequest()); ownerDone <- err }()
	<-started
	type answer struct {
		result *Result
		lease  *SourceLease
		err    error
	}
	waiterDone := make(chan answer, 1)
	go func() {
		r, l, e := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:waiter")
		waiterDone <- answer{r, l, e}
	}()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		entry := c.entries[coordinatorKey{"device", "channel"}]
		return len(entry.consumerClaims) == 1
	}, time.Second, time.Millisecond)
	// A none-reader task waiting on this exact publication must see the claim
	// even if it wins the mutex before the share caller resumes.
	stopDone := make(chan error, 1)
	c.mu.Lock()
	published := c.entries[coordinatorKey{"device", "channel"}].done
	c.mu.Unlock()
	go func() {
		<-published
		_, err := s.StopOnNoneReader(context.Background(), stream.LiveRef{StreamID: "stream", SSRC: "0200000001", Generation: 7})
		stopDone <- err
	}()
	close(finish)
	require.NoError(t, <-ownerDone)
	require.NoError(t, <-stopDone)
	answerValue := <-waiterDone
	require.NoError(t, answerValue.err)
	require.NotNil(t, answerValue.lease)
	require.Zero(t, stops.Load())
	require.NoError(t, answerValue.lease.Release())
	require.False(t, registry.HasLease("stream"))
}

func TestSourceConsumerCancelledWaiterNeverRetainsLease(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	s, c, registry, _ := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) {
		close(started)
		<-finish
		return consumerResult(), nil
	})
	ownerDone := make(chan error, 1)
	go func() { _, err := s.StartAuthorized(context.Background(), consumerRequest()); ownerDone <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waiterDone := make(chan error, 1)
	go func() {
		_, lease, err := s.StartAuthorizedWithSourceLease(ctx, consumerRequest(), registry, "share:cancelled")
		if lease != nil {
			_ = lease.Release()
		}
		waiterDone <- err
	}()
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.entries[coordinatorKey{"device", "channel"}].consumerClaims) == 1
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-waiterDone, context.Canceled)
	close(finish)
	require.NoError(t, <-ownerDone)
	require.False(t, registry.HasLease("stream"))
}

func TestSourceConsumerFinalStopCheckObservesLeaseAcquiredDuringReaderProbe(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	s, c, registry, stops := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) { return consumerResult(), nil })
	require.True(t, c.Restore(Request{DeviceID: "device", ChannelID: "channel"}, consumerResult()))
	s.zlm = &blockedConsumerReaderZLM{entered: entered, finish: finish}
	stopDone := make(chan bool, 1)
	go func() {
		stopped, err := s.StopOnNoneReader(context.Background(), resultLiveRef(consumerResult()))
		if err != nil {
			stopDone <- true
			return
		}
		stopDone <- stopped
	}()
	<-entered
	_, lease, err := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:probe")
	require.NoError(t, err)
	close(finish)
	require.False(t, <-stopDone)
	require.Zero(t, stops.Load())
	require.NoError(t, lease.Release())
}

type blockedConsumerReaderZLM struct {
	mockZLM
	entered, finish chan struct{}
}

func (z *blockedConsumerReaderZLM) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	close(z.entered)
	<-z.finish
	return []zlm.MediaInfo{{ReaderCount: 0}}, nil
}

func TestSourceConsumerStartFailureDoesNotRetainLease(t *testing.T) {
	failure := errors.New("start failed")
	s, _, registry, _ := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) { return nil, failure })
	result, lease, err := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:failed")
	require.ErrorIs(t, err, failure)
	require.Nil(t, result)
	require.Nil(t, lease)
	require.False(t, registry.HasLease("stream"))
}

func TestSourceConsumerAuthorizationFailureReleasesOnlyOwnLease(t *testing.T) {
	s, c, registry, stops := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) { return consumerResult(), nil })
	require.True(t, c.Restore(Request{DeviceID: "device", ChannelID: "channel"}, consumerResult()))
	other := registry.Acquire("stream", 7, "recording:existing")
	defer other.Release()
	withPlayAuthorization(t, true, false)
	s.tokenIssuer = &consumerFailingBindIssuer{}
	result, lease, err := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:auth-denied")
	require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
	require.Nil(t, result)
	require.Nil(t, lease)
	require.Equal(t, 1, registry.LeaseCount("stream"), "failed caller must release only its own lease")
	require.Zero(t, stops.Load())
}

type consumerFailingBindIssuer struct{}

func (*consumerFailingBindIssuer) AuthorizeDeviceEpochContext(context.Context, string, int64) error {
	return nil
}
func (*consumerFailingBindIssuer) Prepare() (playauth.Prepared, error) {
	return playauth.Prepared{}, nil
}
func (*consumerFailingBindIssuer) BindContext(context.Context, playauth.Prepared, playauth.Binding) (playauth.Grant, error) {
	return playauth.Grant{}, errors.New("denied")
}
func (*consumerFailingBindIssuer) IssueDirectContext(context.Context, playauth.Binding) (playauth.Grant, error) {
	return playauth.Grant{}, errors.New("denied")
}

func TestSourceConsumerStoppingWinsBeforeNewConsumerJoins(t *testing.T) {
	stopEntered, stopFinish := make(chan struct{}), make(chan struct{})
	s, c, registry, _ := sourceConsumerFixture(t, func(context.Context, Request) (*Result, error) {
		r := consumerResult()
		r.Generation = 8
		return r, nil
	})
	require.True(t, c.Restore(Request{DeviceID: "device", ChannelID: "channel"}, consumerResult()))
	c.stop = func(context.Context, *Result) error { close(stopEntered); <-stopFinish; return nil }
	stopDone := make(chan error, 1)
	go func() {
		_, err := s.StopIfCurrent(context.Background(), resultLiveRef(consumerResult()))
		stopDone <- err
	}()
	<-stopEntered
	type answer struct {
		result *Result
		lease  *SourceLease
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		r, l, e := s.StartAuthorizedWithSourceLease(context.Background(), consumerRequest(), registry, "share:new-generation")
		done <- answer{r, l, e}
	}()
	require.Never(t, func() bool { return registry.HasLease("stream") }, 30*time.Millisecond, time.Millisecond)
	close(stopFinish)
	require.NoError(t, <-stopDone)
	got := <-done
	require.NoError(t, got.err)
	require.EqualValues(t, 8, got.result.Generation)
	require.EqualValues(t, 8, got.lease.generation)
	require.NoError(t, got.lease.Release())
}
