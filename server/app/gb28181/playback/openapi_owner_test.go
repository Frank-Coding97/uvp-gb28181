package playback

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlaybackPeekForOwnerDoesNotTouchLeaseAndSeparatesClientNamespace(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }, IdleTimeout: time.Minute, MaxSession: time.Hour})
	created, err := registry.Create(context.Background(), CreateRequest{
		OwnerID: "openapi-client:31", DeviceID: "device", ChannelID: "71", RecordKey: "record",
		SegmentStart: now, SegmentEnd: now.Add(time.Minute),
	})
	require.NoError(t, err)
	service := &Service{registry: registry}
	before, ok := registry.Get(created.Session.ID)
	require.True(t, ok)

	peeked, ok := service.PeekForOwner(created.Session.ID, "openapi-client:31")
	require.True(t, ok)
	require.Equal(t, before.LastActivityAt, peeked.LastActivityAt)
	_, ok = service.PeekForOwner(created.Session.ID, "openapi-client:32")
	require.False(t, ok)
	_, ok = service.PeekForOwner(created.Session.ID, "31")
	require.False(t, ok, "a numeric client identifier is not a user owner")
}

func TestPlaybackIdempotencyKeyCannotBeReboundToAnotherRecordSegment(t *testing.T) {
	now := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := CreateRequest{
		OwnerID: "openapi-client:31", DeviceID: "device", ChannelID: "71", RecordKey: "record-one", IdempotencyKey: "same-key",
		SegmentStart: now, SegmentEnd: now.Add(time.Minute), PlayFrom: now.Add(time.Second),
	}
	_, err := registry.Create(context.Background(), request)
	require.NoError(t, err)
	request.RecordKey = "record-two"
	_, err = registry.Create(context.Background(), request)
	require.ErrorIs(t, err, ErrPlaybackBusy)
}
