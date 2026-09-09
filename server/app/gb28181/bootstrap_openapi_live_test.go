package gb28181

import (
	"context"
	"os"
	"strings"
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

func TestOpenAPIMediaRootProductionAssemblyOrder(t *testing.T) {
	data, err := os.ReadFile("bootstrap.go")
	require.NoError(t, err)
	source := string(data)
	begin := strings.Index(source, "func startSIPDependenciesWithFactory(")
	end := strings.Index(source, "func buildPlaySigner(")
	require.Greater(t, end, begin)
	assembly := source[begin:end]

	prepare := strings.Index(assembly, "prepareOpenAPIMediaRoot(")
	validate := strings.Index(assembly, "play.WithQualifiedNodeValidator(openAPIValidator)")
	player := strings.Index(assembly, "openAPILivePlayer.Publish(playSvc)")
	publish := strings.Index(assembly, "publishOpenAPIMediaRoot(openAPICandidate)")
	require.GreaterOrEqual(t, prepare, 0)
	require.Greater(t, validate, prepare)
	require.Greater(t, player, validate)
	require.Greater(t, publish, player)

	rootData, err := os.ReadFile("bootstrap_openapi_live.go")
	require.NoError(t, err)
	rootSource := string(rootData)
	hooks := strings.Index(rootSource, "gbroutes.SetOpenAPIMediaAuthorization(openAPIMediaRoot, openAPIMediaRoot, openAPIMediaRoot)")
	rootPublish := strings.Index(rootSource, "openAPIMediaRoot.Publish(")
	require.GreaterOrEqual(t, hooks, 0)
	require.Greater(t, rootPublish, hooks, "install the fail-closed root at Hooks before making Gateway ready")
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
