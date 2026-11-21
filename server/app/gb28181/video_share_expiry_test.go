package gb28181

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type expiryScannerStub struct {
	calls   atomic.Int32
	entered chan struct{}
}

func (s *expiryScannerStub) ScanExpired(ctx context.Context, limit int) (int, error) {
	s.calls.Add(1)
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, ctx.Err()
}
func TestVideoShareExpiryRuntimeCancelsActiveScanAndWaits(t *testing.T) {
	scanner := &expiryScannerStub{entered: make(chan struct{}, 1)}
	cancel, done := startVideoShareExpiryRuntime(scanner, time.Hour)
	select {
	case <-scanner.entered:
	case <-time.After(time.Second):
		t.Fatal("startup must recover missed expirations immediately")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expiry scan leaked on shutdown")
	}
	require.EqualValues(t, 1, scanner.calls.Load())
}
