package play

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type playRequestScopeKey struct{}

type loggingCleanupZLM struct {
	mu         sync.Mutex
	context    context.Context
	contextErr error
}

func (z *loggingCleanupZLM) OpenRtpServerWithSSRC(context.Context, zlm.OpenRtpServerRequest) (*zlm.OpenRtpServerResult, error) {
	return nil, errors.New("unused")
}

func (z *loggingCleanupZLM) IsMediaOnline(context.Context, string, string) (bool, error) {
	return false, nil
}

func (z *loggingCleanupZLM) CloseRtpServer(ctx context.Context, _ string) error {
	z.mu.Lock()
	z.context = ctx
	z.contextErr = ctx.Err()
	z.mu.Unlock()
	return nil
}

type loggingCleanupInviter struct {
	mu         sync.Mutex
	context    context.Context
	contextErr error
}

func (i *loggingCleanupInviter) InviteTracked(context.Context, *uac.SessionManager, *uac.Session, string) (uac.InviteOutcome, error) {
	return uac.InviteOutcome{}, errors.New("unused")
}

func (i *loggingCleanupInviter) Bye(ctx context.Context, _ *uac.SessionManager, _ string) error {
	i.mu.Lock()
	i.context = ctx
	i.contextErr = ctx.Err()
	i.mu.Unlock()
	return nil
}

func TestLoggingAsyncContextPlayRollbackKeepsScopeAfterRequestCancel(t *testing.T) {
	requestCtx := logging.WithContext(
		context.WithValue(context.Background(), playRequestScopeKey{}, "request-scope"),
		logging.WithIdentity(zap.NewNop(), zap.String("request_id", "play-request")),
	)
	requestCtx, cancel := context.WithCancel(requestCtx)
	cancel()

	z := &loggingCleanupZLM{}
	inviter := &loggingCleanupInviter{}
	svc := &Service{inviter: inviter}
	releaseSSRC := true
	cause := errors.New("start failed")
	_, err := svc.rollbackFailedStart(requestCtx, Request{DeviceID: "device-1", ChannelID: "channel-1"},
		&Result{StreamID: "stream-1"},
		stream.LiveRef{StreamID: "stream-1", SSRC: "0200000001"}, z, cause, true, &releaseSSRC)
	require.ErrorIs(t, err, cause)

	z.mu.Lock()
	closeCtx := z.context
	closeErr := z.contextErr
	z.mu.Unlock()
	inviter.mu.Lock()
	byeCtx := inviter.context
	byeErr := inviter.contextErr
	inviter.mu.Unlock()
	require.Equal(t, "request-scope", closeCtx.Value(playRequestScopeKey{}))
	require.NoError(t, closeErr)
	require.Equal(t, "request-scope", byeCtx.Value(playRequestScopeKey{}))
	require.NoError(t, byeErr)
}

func TestLoggingGBHTTPPlayProbeUsesRequestLogger(t *testing.T) {
	core, entries := observer.New(zap.InfoLevel)
	root := zap.New(core)
	previous := app.ZapLog
	t.Cleanup(func() { app.ZapLog = previous })
	app.ZapLog = root
	requestCtx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("request_id", "probe-request")))

	probeErr := errors.New("probe unavailable")
	z := &mockZLM{onlineErr: probeErr}
	svc := &Service{zlm: z}
	_, err := svc.tryReuseStream(requestCtx, &gbmodels.GbChannel{StreamID: "stream-1"})
	require.NoError(t, err)
	require.Len(t, entries.All(), 1)
	fields := entries.All()[0].ContextMap()
	require.Equal(t, "probe-request", fields["request_id"])
	require.Equal(t, "gb28181.play.reuse_probe_failed", fields["event"])
}
