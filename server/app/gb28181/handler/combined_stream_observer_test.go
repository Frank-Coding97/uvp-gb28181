package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCombinedStreamObserverNotifiesEveryConsumer(t *testing.T) {
	first, second := &countingStreamObserver{}, &countingStreamObserver{}
	require.NoError(t, (CombinedStreamObserver{first, nil, second}).ObserveStream(context.Background(), "stream", false))
	require.Equal(t, 1, first.calls)
	require.Equal(t, 1, second.calls)
}

type countingStreamObserver struct{ calls int }

func (o *countingStreamObserver) ObserveStream(context.Context, string, bool) error {
	o.calls++
	return nil
}
