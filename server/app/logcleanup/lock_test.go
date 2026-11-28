package logcleanup

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAcquireSerializesSameKindAndAllowsDifferentKinds(t *testing.T) {
	release, err := Acquire(context.Background(), Operation)
	require.NoError(t, err)
	otherRelease, err := Acquire(context.Background(), Login)
	require.NoError(t, err)
	otherRelease()
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Acquire(ctx, Operation)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAcquireRejectsUnknownKind(t *testing.T) {
	_, err := Acquire(context.Background(), Kind("unknown"))
	require.Error(t, err)
}
