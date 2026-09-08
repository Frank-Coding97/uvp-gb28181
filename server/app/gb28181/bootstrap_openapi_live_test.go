package gb28181

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	openapimedia "uvplatform.cn/uvp-gb28181/app/openapi/media"
)

type heldOpenAPIPlayer struct{ started, release chan struct{} }

func (p *heldOpenAPIPlayer) EnsureLive(context.Context, play.Request) (*play.Result, error) {
	close(p.started)
	<-p.release
	return &play.Result{StreamID: "late-old-generation"}, nil
}

func TestOpenAPILiveRetirementPreventsDependencyTeardown(t *testing.T) {
	previousRuntime, previousService := openAPILivePlayer, playSvc
	openAPILivePlayer = openapimedia.NewLivePlayerRuntime()
	playSvc = &play.Service{}
	sentinel := playSvc
	p := &heldOpenAPIPlayer{started: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-p.release:
		default:
			close(p.release)
		}
		_ = openAPILivePlayer.Retire(context.Background())
		openAPILivePlayer, playSvc = previousRuntime, previousService
	}()
	require.NoError(t, openAPILivePlayer.Publish(p))
	done := make(chan error, 1)
	go func() { _, err := OpenAPILivePlayer().EnsureLive(context.Background(), play.Request{}); done <- err }()
	<-p.started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, stopSIPDependencies(ctx))
	require.Same(t, sentinel, playSvc, "timeout must retain actual old dependencies")
	require.Error(t, openAPILivePlayer.Publish(p))
	close(p.release)
	require.Error(t, <-done, "late success from a retired service is not a valid application result")
	require.NoError(t, openAPILivePlayer.Retire(context.Background()))
}
