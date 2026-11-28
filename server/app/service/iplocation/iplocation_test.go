package iplocation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolverPrivateIPDoesNotCallProviders(t *testing.T) {
	var calls atomic.Int32
	r := NewResolver(ResolverOptions{
		Local:  ProviderFunc(func(context.Context, string) (string, bool) { calls.Add(1); return "本地", true }),
		Remote: ProviderFunc(func(context.Context, string) (string, bool) { calls.Add(1); return "远程", true }),
	})

	require.Equal(t, "内网", r.Resolve(context.Background(), "192.168.1.2"))
	require.Zero(t, calls.Load())
}

func TestResolverLocalHitSkipsRemote(t *testing.T) {
	var remoteCalls atomic.Int32
	r := NewResolver(ResolverOptions{
		Local:  ProviderFunc(func(context.Context, string) (string, bool) { return "中国 广东 深圳", true }),
		Remote: ProviderFunc(func(context.Context, string) (string, bool) { remoteCalls.Add(1); return "远程", true }),
	})

	require.Equal(t, "中国 广东 深圳", r.Resolve(context.Background(), "1.1.1.1"))
	require.Zero(t, remoteCalls.Load())
}

func TestIPWhoIsProviderMapsFieldsAndResolverCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"country":"中国","region":"广东省","city":"深圳市","connection":{"isp":"Example ISP"}}`))
	}))
	defer srv.Close()

	provider := NewIPWhoIsProvider(srv.Client(), srv.URL, time.Second)
	r := NewResolver(ResolverOptions{Remote: provider})
	require.Equal(t, "中国 广东省 深圳市 Example ISP", r.Resolve(context.Background(), "8.8.8.8"))
	require.Equal(t, "中国 广东省 深圳市 Example ISP", r.Resolve(context.Background(), "8.8.8.8"))
	require.Equal(t, int32(1), calls.Load())
}

func TestIPWhoIsProviderFailureReturnsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"message":"Reserved range"}`))
	}))
	defer srv.Close()

	provider := NewIPWhoIsProvider(srv.Client(), srv.URL, time.Second)
	r := NewResolver(ResolverOptions{Remote: provider})
	require.Equal(t, "未知", r.Resolve(context.Background(), "8.8.8.8"))
}

func TestIPWhoIsProviderTimeoutReturnsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	defer srv.Close()

	provider := NewIPWhoIsProvider(srv.Client(), srv.URL, 10*time.Millisecond)
	r := NewResolver(ResolverOptions{Remote: provider})
	require.Equal(t, "未知", r.Resolve(context.Background(), "8.8.8.8"))
}

func TestNormalizeRegion(t *testing.T) {
	require.Equal(t, "中国 广东省 深圳市 电信", NormalizeRegion("中国|0|广东省|深圳市|电信"))
	require.Equal(t, "未知", NormalizeRegion("0|0|0|0|0"))
}

func TestIP2RegionProviderReadsBundledDatabase(t *testing.T) {
	provider := NewIP2RegionProvider(filepath.Join("..", "..", "..", "resource", "ipdb", "ip2region_v4.xdb"), "")
	require.NotNil(t, provider)
	value, ok := provider.Lookup(context.Background(), "1.1.1.1")
	require.True(t, ok)
	require.NotEqual(t, Unknown, NormalizeRegion(value))
}
