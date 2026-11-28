package videoshare

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestViewerWatchDistinguishesRequestCancellationFromAuthorizationFailure(t *testing.T) {
	for _, scenario := range []string{"request-canceled", "permission-revoked", "authorization-store-failed", "authorization-query-timeout"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s, channel, _ := newServiceFixture(t)
				t.Cleanup(s.DrainViewers)
				s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
					return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
				}))
				link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
				require.NoError(t, err)
				viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
				require.NoError(t, err)
				access, err := s.LookupViewer(context.Background(), viewer.Token)
				require.NoError(t, err)
				original := s.authorize
				entered := make(chan struct{})
				s.authorize = func(ctx context.Context, _ *gorm.DB, _ uint, _ uint) (AuthorizationSnapshot, error) {
					close(entered)
					switch scenario {
					case "request-canceled", "authorization-query-timeout":
						// An HLS header timeout ends its handler while the five-second
						// watcher is already checking authorization with that request context.
						<-ctx.Done()
						return AuthorizationSnapshot{}, ctx.Err()
					case "permission-revoked":
						return AuthorizationSnapshot{}, ErrExpired
					default:
						return AuthorizationSnapshot{}, errors.New("authorization storage unavailable")
					}
				}
				requestCtx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				done := make(chan struct{})
				go s.watchViewer(requestCtx, cancel, viewer.Token, done)
				time.Sleep(5 * time.Second)
				synctest.Wait()
				select {
				case <-entered:
				default:
					t.Fatal("watcher did not enter authorization check")
				}
				if scenario == "request-canceled" {
					cancel()
				} else if scenario == "authorization-query-timeout" {
					time.Sleep(2 * time.Second)
				}
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Fatal("watcher did not finish")
				}
				s.authorize = original
				if scenario == "request-canceled" {
					require.NoError(t, access.Context.Err(), "ending one media request must not revoke its viewer")
					require.NotNil(t, s.viewerSession(viewer.Token), "request cancellation must leave the viewer registered for HLS retry")
				} else {
					require.ErrorIs(t, access.Context.Err(), context.Canceled, "real authorization failures remain fail-closed")
					_, err = s.LookupViewer(context.Background(), viewer.Token)
					require.ErrorIs(t, err, ErrExpired)
				}
			})
		})
	}
}
