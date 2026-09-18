package trace

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type reconnectStoreStub struct {
	err atomic.Bool
}

func (s *reconnectStoreStub) InsertBatch(context.Context, []StoredEvent) error {
	if s.err.Load() {
		return errors.New("connection lost")
	}
	return nil
}

func TestReconnectingStoreRecoversAfterFactoryFailure(t *testing.T) {
	var attempts atomic.Int32
	stub := &reconnectStoreStub{}
	store := NewReconnectingStore(func(context.Context) (Store, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("trace store is starting")
		}
		return stub, nil
	}, time.Millisecond, 5*time.Millisecond)

	require.Error(t, store.InsertBatch(context.Background(), []StoredEvent{{EventID: "first"}}))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{{EventID: "second"}}))
	require.GreaterOrEqual(t, attempts.Load(), int32(2))
}

func TestReconnectingStoreDropsBrokenConnectionAndReconnects(t *testing.T) {
	first := &reconnectStoreStub{}
	first.err.Store(true)
	second := &reconnectStoreStub{}
	var attempts atomic.Int32
	store := NewReconnectingStore(func(context.Context) (Store, error) {
		if attempts.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	}, time.Millisecond, 5*time.Millisecond)

	require.Error(t, store.InsertBatch(context.Background(), []StoredEvent{{EventID: "lost"}}))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, store.InsertBatch(context.Background(), []StoredEvent{{EventID: "recovered"}}))
}
