//go:build openapi_live

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	openapiclient "uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// Real client lifecycle/store, configured maintenance, TLS resolver and ZLM
// sockets. Admission and the U label are fixtures, NOT product Hook/JWT/SIP
// acceptance. No production playback gate is opened by this test.
func TestOpenAPIConfiguredRevocationActiveIsolation(t *testing.T) {
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		for _, action := range []string{"disabled", "revoked", "scope"} {
			t.Run(protocol+"/"+action, func(t *testing.T) {
				f := newActiveRevocationFixture(t)
				stream := "active-revocation"
				f.probe.publish(t, stream)
				generation := f.probe.generation(stream)
				players := make([]*probePlayer, 3)
				events := make([]probeEvent, 3)
				for i, label := range []string{"A", "B", "U"} {
					players[i] = f.probe.player(t, protocol, stream, label)
					require.Eventually(t, func() bool {
						var found bool
						events[i], found = f.probe.event("/play", label)
						return found
					}, time.Second, 10*time.Millisecond)
				}
				a := f.client(t, "A")
				b := f.client(t, "B")
				aViewer := f.bind(t, a, protocol, events[0])
				bViewer := f.bind(t, b, protocol, events[1])
				// U has no OpenAPI row, like an unrelated ordinary consumer.
				require.NotEqual(t, events[0].ID, events[1].ID)
				require.NotEqual(t, events[0].ID, events[2].ID)
				publisherID := f.publisher(t)
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				started := time.Now()
				var err error
				if action == "scope" {
					_, err = f.service.SetScope(ctx, a.ID, "play:live:apply", false, a.RowVersion, 7)
				} else {
					_, err = f.service.SetStatus(ctx, a.ID, action, a.RowVersion, 7)
				}
				require.NoError(t, err)
				store := playauth.NewOpenAPIRevocationStore(f.db, nil)
				progress, err := store.Progress(ctx, a.ID)
				require.NoError(t, err)
				require.Equal(t, playauth.OpenAPIRevocationStatusPending, progress.Status)
				require.EqualValues(t, 1, progress.Pending)
				f.checkAuthority(t, a, b, action)
				type report struct {
					result media.RevocationTickResult
					err    error
				}
				reports := make(chan report, 8)
				stop, err := media.StartConfiguredRevocation(ctx, f.db, liveRevocationRegistry{f.node}, f.bindingsPath,
					func(result media.RevocationTickResult, err error) {
						select {
						case reports <- report{result, err}:
						case <-ctx.Done():
						}
					})
				require.NoError(t, err)
				defer stop()
				deadline := time.NewTimer(5*time.Second - time.Since(started))
				defer deadline.Stop()
				first, closed := true, false
				for !closed {
					select {
					case got := <-reports:
						require.NoError(t, got.err)
						require.Zero(t, got.result.Alarms)
						if first {
							require.Equal(t, 1, got.result.Pending)
							require.Zero(t, got.result.Closed, "scheduling shutdown is not confirmed closure")
							first = false
						}
						closed = got.result.Closed == 1
					case <-deadline.C:
						t.Fatal("active target was not fresh-confirmed closed within five seconds")
					}
				}
				select {
				case <-players[0].done:
				case <-deadline.C:
					t.Fatal("A still receives media after the revocation deadline")
				}
				elapsed := time.Since(started)
				require.Less(t, elapsed, 5*time.Second)
				stop()
				require.NoError(t, f.db.First(&aViewer, aViewer.ID).Error)
				require.Equal(t, models.ViewerStateClosed, aViewer.State)
				require.Equal(t, media.RevocationErrorKicked, aViewer.LastErrorClass)
				require.GreaterOrEqual(t, aViewer.Attempts, 2)
				progress, err = store.Progress(ctx, a.ID)
				require.NoError(t, err)
				require.Equal(t, playauth.OpenAPIRevocationStatusClosed, progress.Status)
				require.EqualValues(t, 1, progress.Closed)
				require.Zero(t, progress.Pending)
				f.checkSurvivors(t, stream, events)
				// Restoring credentials or scope is not resurrection of this
				// already consumed grant. Permanent revocation cannot be undone.
				if action == "scope" {
					_, err = f.service.SetScope(ctx, a.ID, "play:live:apply", true, a.RowVersion+1, 7)
					require.NoError(t, err)
				} else {
					_, err = f.service.SetStatus(ctx, a.ID, models.StatusActive, a.RowVersion+1, 7)
					if action == "revoked" {
						require.ErrorIs(t, err, openapiclient.ErrRevoked)
					} else {
						require.NoError(t, err)
					}
				}
				var oldGrant models.PlayGrant
				require.NoError(t, f.db.First(&oldGrant, "grant_id = ?", aViewer.GrantID).Error)
				require.Equal(t, models.GrantStateRevoked, oldGrant.State)
				var restoredViewer models.Viewer
				require.NoError(t, f.db.First(&restoredViewer, aViewer.ID).Error)
				require.Equal(t, aViewer, restoredViewer)
				// No reconnect path exists in probePlayer. Every interval must carry
				// new payload on both original survivor sockets.
				for second := 0; second < 30; second++ {
					beforeB, beforeU := players[1].bytes.Load(), players[2].bytes.Load()
					time.Sleep(time.Second)
					for _, p := range players[1:] {
						select {
						case <-p.done:
							t.Fatal("unrelated original player disconnected")
						default:
						}
					}
					require.Greater(t, players[1].bytes.Load(), beforeB)
					require.Greater(t, players[2].bytes.Load(), beforeU)
				}
				require.Equal(t, generation, f.probe.generation(stream))
				require.Equal(t, publisherID, f.publisher(t))
				f.checkSurvivors(t, stream, events)
				var persistedB models.Viewer
				require.NoError(t, f.db.First(&persistedB, bViewer.ID).Error)
				require.Equal(t, bViewer, persistedB, "B's durable viewer must remain byte-for-byte unchanged")
				t.Logf("%s: lifecycle commit through configured worker; A EOF and fresh closed in %s; B/U original sockets + source continuous 30s", action, elapsed)
			})
		}
	}
}
