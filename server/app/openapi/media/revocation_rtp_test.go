package media

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

func TestTrustedFactoryResolvesIntentRTPOverPinnedTLS(t *testing.T) {
	f := newTrustedFactoryFixture(t)
	var resolver playback.IntentRTPResolver = f.factory
	runtime, err := resolver.ResolveRTP(context.Background(), f.n.MediaServerUUID)
	require.NoError(t, err)
	require.NotNil(t, runtime.Control)
	require.Equal(t, f.boot, runtime.BootNonce)
	require.NotNil(t, runtime.Release)
	runtime.Release()
	runtime.Release()
	f.mu.Lock()
	require.NotEmpty(t, f.requests, "must probe the real TLS peer")
	f.mu.Unlock()
	_, err = resolver.ResolveRTP(context.Background(), "other-node")
	require.Error(t, err)
	var absent *TrustedRevocationFactory
	_, err = absent.ResolveRTP(context.Background(), f.n.MediaServerUUID)
	require.Error(t, err)
}
