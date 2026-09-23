package zlm

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientMediaTrafficStatistic(t *testing.T) {
	client, server := newMockClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getMediaTrafficStatistic", r.URL.Path)
		require.Equal(t, "test-secret", r.URL.Query().Get("secret"))
		_, _ = w.Write([]byte(`{"code":0,"data":{"upstreamBytesPerSecond":1024,"downstreamBytesPerSecond":2048,"originSocketCount":1,"playerSocketCount":2,"unit":"bytes/s"}}`))
	})
	defer server.Close()

	statistic, err := client.GetMediaTrafficStatistic(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1024, statistic.UpstreamBytesPerSecond)
	require.EqualValues(t, 2048, statistic.DownstreamBytesPerSecond)
	require.Equal(t, "bytes/s", statistic.Unit)
	require.Equal(t, 1, statistic.OriginSocketCount)
	require.Equal(t, 2, statistic.PlayerSocketCount)
}
