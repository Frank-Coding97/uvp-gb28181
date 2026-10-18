package playback

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type rangePlaybackInviter struct {
	fakeInvite
	value string
	err   error
}

func (f *rangePlaybackInviter) ActionRange(_ context.Context, _ string, value string, _ time.Duration) error {
	f.value = value
	return f.err
}

func TestPlaybackRangeReachesDeviceWithoutDroppingEnd(t *testing.T) {
	now := time.Unix(1700000000, 0)
	invite := &rangePlaybackInviter{}
	s := NewService(NewRegistry(RegistryConfig{}), &fakeNodePicker{node: NodeInfo{ID: "1", RecvIP: "192.0.2.10", ServerID: "34020000002000000001"}}, &fakeRTP{}, invite, &fakeMedia{}, ServiceConfig{})
	created, err := s.Create(context.Background(), validCreate(now))
	require.NoError(t, err)
	value := "smpte=0:00:12:15-0:00:30:00"
	updated, err := s.Action(context.Background(), created.Session.ID, "u1", ActionRequest{Action: "seek", RangeValue: value})
	require.NoError(t, err)
	require.Equal(t, value, invite.value)
	require.InDelta(t, 12.5, updated.PositionSeconds, 0.000001)
}

func TestPlaybackRangeRejectsInvalidValuesBeforeDeviceAction(t *testing.T) {
	now := time.Unix(1700000000, 0)
	invite := &rangePlaybackInviter{}
	s := NewService(NewRegistry(RegistryConfig{}), &fakeNodePicker{node: NodeInfo{ID: "1", RecvIP: "192.0.2.10", ServerID: "34020000002000000001"}}, &fakeRTP{}, invite, &fakeMedia{}, ServiceConfig{})
	created, err := s.Create(context.Background(), validCreate(now))
	require.NoError(t, err)
	for _, request := range []ActionRequest{
		{Action: "seek", PositionSeconds: math.NaN()},
		{Action: "seek", RangeValue: "npt=10-61"},
		{Action: "seek", RangeValue: "npt=20-10"},
		{Action: "pause", RangeValue: "npt=10-20"},
	} {
		_, err = s.Action(context.Background(), created.Session.ID, "u1", request)
		require.Error(t, err, "%+v", request)
	}
	require.Empty(t, invite.value)
	require.Zero(t, invite.actionCalls.Load())
}

func TestPlaybackRangeOwnerDeviceFailureAndMissingCapability(t *testing.T) {
	now := time.Unix(1700000000, 0)
	invite := &rangePlaybackInviter{err: ErrRTPUnavailable}
	s := NewService(NewRegistry(RegistryConfig{}), &fakeNodePicker{node: NodeInfo{ID: "1", RecvIP: "192.0.2.10", ServerID: "34020000002000000001"}}, &fakeRTP{}, invite, &fakeMedia{}, ServiceConfig{})
	created, err := s.Create(context.Background(), validCreate(now))
	require.NoError(t, err)
	request := ActionRequest{Action: "seek", RangeValue: "npt=12-30"}
	_, err = s.Action(context.Background(), created.Session.ID, "wrong-owner", request)
	require.ErrorIs(t, err, ErrPlaybackNotFound)
	require.Empty(t, invite.value)
	_, err = s.Action(context.Background(), created.Session.ID, "u1", request)
	require.ErrorIs(t, err, ErrRTPUnavailable)
	actual, _ := s.registry.Get(created.Session.ID)
	require.Zero(t, actual.PositionSeconds, "device rejection must not change local position")
	s.inviter = &fakeInvite{}
	_, err = s.Action(context.Background(), created.Session.ID, "u1", request)
	require.ErrorIs(t, err, ErrPlaybackRangeUnsupported)
}
