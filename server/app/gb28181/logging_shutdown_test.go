package gb28181

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type loggingFailingSIPServer struct {
	fakeSIPRuntimeServer
	failure error
}

func (s *loggingFailingSIPServer) Shutdown(ctx context.Context) error {
	_ = s.fakeSIPRuntimeServer.Shutdown(ctx)
	return s.failure
}

func TestLoggingShutdownSIPReportsEveryFailure(t *testing.T) {
	oldServer, oldCascade, oldLog := sipServer, cascadeRuntimeManager, app.ZapLog
	t.Cleanup(func() { sipServer, cascadeRuntimeManager, app.ZapLog = oldServer, oldCascade, oldLog })
	app.ZapLog = zap.NewNop()
	events := []string{}
	cascadeErr := errors.New("cascade stopped incompletely")
	sipErr := errors.New("transport stopped incompletely")
	cascadeRuntimeManager = cascadeRuntimeLifecycleFunc(func(context.Context) error { events = append(events, "cascade.shutdown"); return cascadeErr })
	sipServer = &loggingFailingSIPServer{fakeSIPRuntimeServer: fakeSIPRuntimeServer{events: &events}, failure: sipErr}
	err := stopSIPDependencies(context.Background())
	require.ErrorIs(t, err, cascadeErr)
	require.ErrorIs(t, err, sipErr)
	require.Equal(t, []string{"record.sink.clear", "cascade.shutdown", "sip.shutdown"}, events)
}
