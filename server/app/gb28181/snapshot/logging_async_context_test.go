package snapshot

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type snapshotRequestScopeKey struct{}

type loggingSnapshotClient struct {
	mu       sync.Mutex
	contexts []context.Context
	errs     []error
	called   chan struct{}
}

func (c *loggingSnapshotClient) GetSnap(ctx context.Context, _ string, _, _ int) ([]byte, error) {
	c.mu.Lock()
	c.contexts = append(c.contexts, ctx)
	c.errs = append(c.errs, ctx.Err())
	c.mu.Unlock()
	select {
	case c.called <- struct{}{}:
	default:
	}
	return []byte{0xff, 0xd8, 0xff}, nil
}

type loggingSnapshotRepo struct {
	mu       sync.Mutex
	contexts []context.Context
	errs     []error
	called   chan struct{}
}

func (r *loggingSnapshotRepo) UpdateSnapshot(ctx context.Context, _, _ string, _ string, _ time.Time) error {
	r.mu.Lock()
	r.contexts = append(r.contexts, ctx)
	r.errs = append(r.errs, ctx.Err())
	r.mu.Unlock()
	select {
	case r.called <- struct{}{}:
	default:
	}
	return nil
}

type expiredCaptureClient struct {
	deadlineErr chan error
}

func (c expiredCaptureClient) GetSnap(ctx context.Context, _ string, _, _ int) ([]byte, error) {
	<-ctx.Done()
	c.deadlineErr <- ctx.Err()
	return []byte{0xff, 0xd8, 0xff}, nil
}

func TestLoggingAsyncContextSnapshotKeepsScopeAfterRequestCancel(t *testing.T) {
	previous := app.ZapLog
	t.Cleanup(func() { app.ZapLog = previous })
	core, entries := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ZapLog = root

	client := &loggingSnapshotClient{called: make(chan struct{}, 1)}
	repo := &loggingSnapshotRepo{called: make(chan struct{}, 1)}
	var buildMu sync.Mutex
	var buildCtx context.Context
	var buildErr error
	svc := New(Config{
		UploadRoot:  t.TempDir(),
		DelayBefore: time.Millisecond,
		ZLMTimeout:  1,
		ZLMExpire:   1,
		GetClient:   func(string) (ZLMClient, error) { return client, nil },
		BuildStreamURL: func(ctx context.Context, _, streamID, _ string) (string, error) {
			buildMu.Lock()
			buildCtx = ctx
			buildErr = ctx.Err()
			buildMu.Unlock()
			return "rtsp://mock/" + streamID, nil
		},
		Repo:   repo,
		Logger: root,
	})

	requestCtx := logging.WithContext(
		context.WithValue(context.Background(), snapshotRequestScopeKey{}, "request-scope"),
		logging.WithIdentity(root, zap.String("request_id", "snapshot-request")),
	)
	requestCtx, cancel := context.WithCancel(requestCtx)
	cancel()
	svc.FireAfterPlay(requestCtx, "node-1", "stream-1", "device-1", "channel-1", "")

	select {
	case <-client.called:
	case <-time.After(time.Second):
		t.Fatal("snapshot capture did not continue after request cancellation")
	}
	select {
	case <-repo.called:
	case <-time.After(time.Second):
		t.Fatal("snapshot repository update did not continue after request cancellation")
	}

	buildMu.Lock()
	streamCtx := buildCtx
	buildMu.Unlock()
	client.mu.Lock()
	clientCtx := client.contexts[0]
	clientErr := client.errs[0]
	client.mu.Unlock()
	repo.mu.Lock()
	repoCtx := repo.contexts[0]
	repoErr := repo.errs[0]
	repo.mu.Unlock()
	buildMu.Lock()
	streamErr := buildErr
	buildMu.Unlock()
	require.Equal(t, "request-scope", streamCtx.Value(snapshotRequestScopeKey{}))
	require.NoError(t, streamErr)
	require.Equal(t, "request-scope", clientCtx.Value(snapshotRequestScopeKey{}))
	require.NoError(t, clientErr)
	require.Equal(t, "request-scope", repoCtx.Value(snapshotRequestScopeKey{}))
	require.NoError(t, repoErr)
	require.Eventually(t, func() bool { return entries.Len() > 0 }, time.Second, time.Millisecond)
	fields := entries.All()[entries.Len()-1].ContextMap()
	require.Equal(t, "snapshot-request", fields["request_id"])
	require.Equal(t, "gb28181.snapshot.captured", fields["event"])
}

func TestLoggingAsyncContextSnapshotRepoOutlivesCaptureDeadline(t *testing.T) {
	core, entries := observer.New(zap.InfoLevel)
	root := zap.New(core)
	requestCtx := logging.WithContext(
		context.WithValue(context.Background(), snapshotRequestScopeKey{}, "request-scope"),
		logging.WithIdentity(root, zap.String("request_id", "snapshot-deadline")),
	)
	repo := &loggingSnapshotRepo{called: make(chan struct{}, 1)}
	captureDeadlineErr := make(chan error, 1)
	svc := New(Config{
		UploadRoot:  t.TempDir(),
		DelayBefore: time.Millisecond,
		ZLMTimeout:  1,
		ZLMExpire:   1,
		GetClient: func(string) (ZLMClient, error) {
			return expiredCaptureClient{deadlineErr: captureDeadlineErr}, nil
		},
		BuildStreamURL: func(_ context.Context, _, streamID, _ string) (string, error) {
			return "rtsp://mock/" + streamID, nil
		},
		Repo:   repo,
		Logger: root,
	})

	require.NoError(t, svc.doCaptureContext(requestCtx, "node-1", "stream-1", "device-1", "channel-1", ""))
	require.ErrorIs(t, <-captureDeadlineErr, context.DeadlineExceeded)
	select {
	case <-repo.called:
	default:
		t.Fatal("snapshot repository update was not called")
	}

	repo.mu.Lock()
	repoCtx := repo.contexts[0]
	captureErr := repo.errs[0]
	repo.mu.Unlock()
	require.Equal(t, "request-scope", repoCtx.Value(snapshotRequestScopeKey{}))
	require.NoError(t, captureErr)
	require.Eventually(t, func() bool { return entries.Len() > 0 }, time.Second, time.Millisecond)
	fields := entries.All()[entries.Len()-1].ContextMap()
	require.Equal(t, "snapshot-deadline", fields["request_id"])
	require.Equal(t, "gb28181.snapshot.captured", fields["event"])
}

func TestLoggingAsyncContextSnapshotPanicUsesSafeFields(t *testing.T) {
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	svc := New(Config{
		UploadRoot:  t.TempDir(),
		DelayBefore: time.Millisecond,
		GetClient:   func(string) (ZLMClient, error) { return &loggingSnapshotClient{}, nil },
		BuildStreamURL: func(_ context.Context, _, _, _ string) (string, error) {
			panic("snapshot-secret")
		},
		Logger: root,
	})

	svc.FireAfterPlay(context.Background(), "node-1", "stream-1", "device-1", "channel-1", "")
	require.Eventually(t, func() bool { return entries.Len() > 0 }, time.Second, time.Millisecond)
	entry := entries.All()[0]
	require.Equal(t, zap.ErrorLevel, entry.Level)
	fields := entry.ContextMap()
	require.Equal(t, "gb28181.snapshot.panic", fields["event"])
	require.Equal(t, "string", fields["panic_type"])
	stack, ok := fields["stack"].(string)
	require.True(t, ok)
	require.NotEmpty(t, stack)
	require.NotContains(t, stack, "snapshot-secret")
}
