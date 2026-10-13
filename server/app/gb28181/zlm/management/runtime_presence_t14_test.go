package management

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimePresenceT14_UsesFreshExactReadAndDistinguishesAbsence(t *testing.T) {
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getMediaInfo", r.URL.Path)
		require.Equal(t, "rtsp", r.URL.Query().Get("schema"))
		require.Equal(t, "__defaultVhost__", r.URL.Query().Get("vhost"))
		require.Equal(t, "live", r.URL.Query().Get("app"))
		require.Equal(t, "camera-1", r.URL.Query().Get("stream"))
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"code":0,"schema":"rtsp","vhost":"__defaultVhost__","app":"live","stream":"camera-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":-500,"msg":"can not find the stream"}`))
	})
	defer cleanup()

	presence := NewRuntimePresenceReader(reader)
	target := OwnershipTarget{NodeID: current.ID, Media: MediaIdentity{
		Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera-1",
	}}
	present, err := presence.IsPresent(context.Background(), target)
	require.NoError(t, err)
	require.True(t, present)

	present, err = presence.IsPresent(context.Background(), target)
	require.NoError(t, err)
	require.False(t, present)
	require.Equal(t, int32(2), calls.Load(), "ownership preflight must bypass runtime cache")
}

func TestRuntimePresenceT14_UpstreamFailureRemainsUnknown(t *testing.T) {
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream secret must stay internal", http.StatusBadGateway)
	})
	defer cleanup()

	presence := NewRuntimePresenceReader(reader)
	present, err := presence.IsPresent(context.Background(), OwnershipTarget{
		NodeID: current.ID,
		Media:  MediaIdentity{Schema: "rtsp", Vhost: "__defaultVhost__", App: "live", Stream: "camera-1"},
	})
	require.Error(t, err)
	require.False(t, present)
	require.NotContains(t, err.Error(), "upstream secret")
}
