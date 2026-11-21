package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCascadeCleanupGuardRetainsSourceUntilSenderConfirmed(t *testing.T) {
	resources := &fakeCleanupResources{}
	r := NewRegistry(RegistryConfig{})
	request := playbackRequest(time.Now(), "cascade:1:dialog", "1", "segment")
	request.Resources = resources
	confirmed := false
	request.BeforeCleanup = func(context.Context) error {
		if !confirmed {
			return errors.New("sender stop not confirmed")
		}
		return nil
	}
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	for i := 0; i < playbackCleanupAttemptLimit+1; i++ {
		require.Error(t, r.Stop(context.Background(), created.Session.ID, "upstream gone"))
	}
	require.Zero(t, resources.teardown.Load())
	require.Zero(t, resources.close.Load())
	require.Zero(t, resources.unbind.Load())
	retained, ok := r.Get(created.Session.ID)
	require.True(t, ok)
	require.Equal(t, StateStopping, retained.State)
	confirmed = true
	require.NoError(t, r.Stop(context.Background(), created.Session.ID, "retry"))
	require.EqualValues(t, 1, resources.teardown.Load())
	require.EqualValues(t, 1, resources.close.Load())
	require.EqualValues(t, 1, resources.unbind.Load())
}
