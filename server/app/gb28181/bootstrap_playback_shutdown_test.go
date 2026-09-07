package gb28181

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbplayback "uvplatform.cn/uvp-gb28181/app/gb28181/playback"
)

type retryPlaybackRootCleanup struct{ err error }

func (r *retryPlaybackRootCleanup) Teardown(context.Context) error { return r.err }
func (r *retryPlaybackRootCleanup) CloseRTP(context.Context) error { return nil }
func (r *retryPlaybackRootCleanup) Unbind(context.Context) error   { return nil }

func TestStopSIPDependenciesRetainsFailedPlayback(t *testing.T) {
	for _, withSIP := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial-root", true: "with-sip"}[withSIP], func(t *testing.T) {
			isolateSIPShutdownRoot(t)
			previousService, previousRegistry, previousMetrics := playbackService, playbackRegistry, playbackMetrics
			defer func() {
				playbackService, playbackRegistry, playbackMetrics = previousService, previousRegistry, previousMetrics
			}()
			events := []string{}
			server := &fakeSIPRuntimeServer{events: &events}
			if withSIP {
				sipServer = server
			}
			want := errors.New("fixture resource closure unknown")
			resource := &retryPlaybackRootCleanup{err: want}
			registry := gbplayback.NewRegistry(gbplayback.RegistryConfig{})
			_, err := registry.Create(context.Background(), gbplayback.CreateRequest{
				OwnerID: "1", ChannelID: "2", RecordKey: "record-1",
				SegmentStart: time.Now(), SegmentEnd: time.Now().Add(time.Minute), Resources: resource,
			})
			require.NoError(t, err)
			service := gbplayback.NewService(registry, nil, nil, nil, nil, gbplayback.ServiceConfig{})
			metrics := &gbplayback.Metrics{}
			playbackService, playbackRegistry, playbackMetrics = service, registry, metrics
			require.ErrorIs(t, stopSIPDependencies(context.Background()), want)
			require.Same(t, service, playbackService)
			require.Same(t, registry, playbackRegistry)
			require.Same(t, metrics, playbackMetrics)
			require.Empty(t, events, "SIP and query dependencies must remain usable for retry")
			called := 0
			err = startSIPDependenciesWithFactory(gbconfig.Config{}, func(gbconfig.Config) (sipRuntimeServer, error) {
				called++
				return &fakeSIPRuntimeServer{}, nil
			})
			require.Error(t, err)
			require.Zero(t, called, "even a partial root must retain its playback owner")
			resource.err = nil
			require.NoError(t, stopSIPDependencies(context.Background()))
			require.Nil(t, playbackService)
			require.Nil(t, playbackRegistry)
			require.Nil(t, playbackMetrics)
			if withSIP {
				require.Contains(t, events, "sip.shutdown")
			}
		})
	}
}
