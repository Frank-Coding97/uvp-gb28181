package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func newRuntimeReaderFixture(t *testing.T, handler http.HandlerFunc) (*RuntimeReader, *node.Node, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	registry := node.NewRegistry(newExecutorMemoryRepo())
	current := addExecutorNode(t, registry, node.Node{
		Host:      parsed.Hostname(),
		APIPort:   port,
		APISecret: "runtime-secret",
		State:     node.StateActive,
	})
	executor := NewNodeExecutor(registry, nil)
	reader := NewRuntimeReader(executor, WithRuntimeCacheTTL(50*time.Millisecond))
	return reader, current, server.Close
}

func TestRuntimeReaderStatisticSingleFlightAndTTL(t *testing.T) {
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":7,"TcpSession":3}}`))
	})
	defer cleanup()

	const workers = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statistic, err := reader.GetStatistic(context.Background(), current.ID)
			require.NoError(t, err)
			require.Equal(t, uint64(7), statistic.MediaSource)
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())

	time.Sleep(80 * time.Millisecond)
	_, err := reader.GetStatistic(context.Background(), current.ID)
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}

func TestRuntimeReaderCacheRequiresFreshAuthorizationAndNodeState(t *testing.T) {
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":13}}`))
	})
	defer cleanup()

	var allowed atomic.Bool
	allowed.Store(true)
	reader.executor.authorizer = func(context.Context, *node.Node, NodeOperation) error {
		if !allowed.Load() {
			return ErrNodeForbidden
		}
		return nil
	}

	statistic, err := reader.GetStatistic(context.Background(), current.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(13), statistic.MediaSource)
	require.Equal(t, int32(1), calls.Load())

	allowed.Store(false)
	statistic, err = reader.GetStatistic(context.Background(), current.ID)
	got, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeForbidden, got.Code)
	require.Equal(t, http.StatusForbidden, got.HTTPStatus())
	require.Zero(t, statistic)
	require.Equal(t, int32(1), calls.Load(), "denied cache hit must not reach ZLM")

	allowed.Store(true)
	registry := reader.executor.registry.(*node.Registry)
	updated := *current
	updated.State = node.StateOffline
	require.NoError(t, registry.Update(context.Background(), updated))
	statistic, err = reader.GetStatistic(context.Background(), current.ID)
	got, ok = AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeNodeOffline, got.Code)
	require.Equal(t, http.StatusServiceUnavailable, got.HTTPStatus())
	require.Zero(t, statistic)
	require.Equal(t, int32(1), calls.Load(), "offline cache hit must not reach ZLM")

	updated.State = node.StateMaintenance
	require.NoError(t, registry.Update(context.Background(), updated))
	statistic, err = reader.GetStatistic(context.Background(), current.ID)
	got, ok = AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeNodeMaintenance, got.Code)
	require.Equal(t, http.StatusConflict, got.HTTPStatus())
	require.Zero(t, statistic)
	require.Equal(t, int32(1), calls.Load(), "maintenance cache hit must not reach ZLM")
}

type runtimePermissionContextKey struct{}

func TestRuntimeReaderConcurrentPermissionsCannotShareCachedResult(t *testing.T) {
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":17}}`))
	})
	defer cleanup()

	reader.executor.authorizer = func(ctx context.Context, _ *node.Node, _ NodeOperation) error {
		if ctx.Value(runtimePermissionContextKey{}) != "allow" {
			return ErrNodeForbidden
		}
		return nil
	}
	allowed := context.WithValue(context.Background(), runtimePermissionContextKey{}, "allow")
	_, err := reader.GetStatistic(allowed, current.ID)
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())

	start := make(chan struct{})
	allowedDone := make(chan error, 1)
	deniedDone := make(chan struct {
		statistic zlm.Statistic
		err       error
	}, 1)
	go func() {
		<-start
		_, callErr := reader.GetStatistic(allowed, current.ID)
		allowedDone <- callErr
	}()
	go func() {
		<-start
		statistic, callErr := reader.GetStatistic(context.Background(), current.ID)
		deniedDone <- struct {
			statistic zlm.Statistic
			err       error
		}{statistic: statistic, err: callErr}
	}()
	close(start)

	require.NoError(t, <-allowedDone)
	denied := <-deniedDone
	got, ok := AsManagementError(denied.err)
	require.True(t, ok)
	require.Equal(t, CodeForbidden, got.Code)
	require.Zero(t, denied.statistic, "denied caller must not receive the shared cached value")
	require.Equal(t, int32(1), calls.Load())
}

func TestRuntimeReaderLeaderCancellationDoesNotCancelSharedRead(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getStatistic", r.URL.Path)
		calls.Add(1)
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":11}}`))
	})
	defer cleanup()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := reader.GetStatistic(firstCtx, current.ID)
		firstDone <- err
	}()
	<-entered

	secondDone := make(chan error, 1)
	secondValue := make(chan zlm.Statistic, 1)
	go func() {
		statistic, err := reader.GetStatistic(context.Background(), current.ID)
		if err == nil {
			secondValue <- statistic
		}
		secondDone <- err
	}()
	cancelFirst()
	select {
	case err := <-firstDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled leader did not return")
	}
	close(release)

	select {
	case err := <-secondDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("shared waiter did not complete")
	}
	select {
	case statistic := <-secondValue:
		require.Equal(t, uint64(11), statistic.MediaSource)
	default:
		t.Fatal("shared waiter did not receive the typed result")
	}
	require.Equal(t, int32(1), calls.Load())
}

func TestRuntimeReaderSessionsCacheKeyIncludesTypedFilter(t *testing.T) {
	var calls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getAllSession", r.URL.Path)
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	})
	defer cleanup()

	_, err := reader.GetAllSessions(context.Background(), current.ID, zlm.SessionFilter{LocalPort: 1935})
	require.NoError(t, err)
	_, err = reader.GetAllSessions(context.Background(), current.ID, zlm.SessionFilter{LocalPort: 1936})
	require.NoError(t, err)
	require.Equal(t, int32(2), calls.Load())
}

func TestRuntimeReaderCapabilityUnsupportedBlocksTypedRead(t *testing.T) {
	var sessionCalls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/getApiList":
			_, _ = w.Write([]byte(`{"code":0,"data":["/index/api/getStatistic","/index/api/getApiList"]}`))
		case "/index/api/getAllSession":
			sessionCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer cleanup()

	profile, err := reader.GetCapabilityProfile(context.Background(), current.ID)
	require.NoError(t, err)
	require.Equal(t, zlm.CapabilityUnsupported, profile.GetAllSession)

	_, err = reader.GetAllSessions(context.Background(), current.ID, zlm.SessionFilter{})
	got, ok := AsManagementError(err)
	require.True(t, ok)
	require.Equal(t, CodeUnsupportedCapability, got.Code)
	require.Equal(t, http.StatusUnprocessableEntity, got.HTTPStatus())
	require.Zero(t, sessionCalls.Load())
}

func TestRuntimeReaderUnknownCapabilityAllowsControlledTypedRead(t *testing.T) {
	var statisticCalls atomic.Int32
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/api/getApiList":
			_, _ = w.Write([]byte(`{"code":-1,"msg":"not available"}`))
		case "/index/api/getStatistic":
			statisticCalls.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"data":{"MediaSource":9}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer cleanup()

	profile, err := reader.GetCapabilityProfile(context.Background(), current.ID)
	require.Error(t, err)
	require.Equal(t, zlm.CapabilityUnknown, profile.GetStatistic)

	statistic, err := reader.GetStatistic(context.Background(), current.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(9), statistic.MediaSource)
	require.Equal(t, int32(1), statisticCalls.Load())
}

func TestRuntimeReaderCapabilityProfileIsCopiedFromCache(t *testing.T) {
	reader, current, cleanup := newRuntimeReaderFixture(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/index/api/getApiList", r.URL.Path)
		body, _ := json.Marshal(map[string]any{
			"code": 0,
			"data": []string{"/index/api/getStatistic"},
		})
		_, _ = w.Write(body)
	})
	defer cleanup()

	first, err := reader.GetCapabilityProfile(context.Background(), current.ID)
	require.NoError(t, err)
	first.APIs[0] = "mutated"
	first.Reasons["getStatistic"] = "mutated"
	second, err := reader.GetCapabilityProfile(context.Background(), current.ID)
	require.NoError(t, err)
	require.Equal(t, "/index/api/getStatistic", second.APIs[0])
	require.NotEqual(t, "mutated", second.Reasons["getStatistic"])
}
