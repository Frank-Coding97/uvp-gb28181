package trace

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
)

func TestTraceModuleLeavesCleanupToPlatformScheduler(t *testing.T) {
	store := &t12HealthyPrunableStore{closed: make(chan struct{})}
	module := NewModule(gbconfig.TraceConfig{QueueCapacity: 1, RetentionDays: 7}, store, testPayloadCipher())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, module.Shutdown(ctx))
	})
	select {
	case <-module.prunerDone:
	default:
		t.Fatal("trace module still owns an automatic cleanup loop")
	}
	require.Zero(t, store.pruneCalls.Load())
}
