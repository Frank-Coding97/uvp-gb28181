//go:build openapi_live

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
	"uvplatform.cn/uvp-gb28181/app/openapi/media"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

// Real HMAC HTTP -> admission/quota -> application -> v3 -> ZLM -> authenticated
// product Hook -> durable viewer -> lifecycle/store -> configured revocation.
// SIP acquisition, deployment qualification and personnel login remain fixture
// boundaries; this does not turn on the production media gate.
func TestOpenAPIMediaProductLiveRevocation(t *testing.T) {
	for _, protocol := range []string{"https-flv", "wss-flv"} {
		for _, action := range []string{"disabled", "revoked", "scope"} {
			t.Run(protocol+"/"+action, func(t *testing.T) {
				f := newProductMediaFixture(t, protocol)
				a, aKey := f.machine(t, "A")
				_, bKey := f.machine(t, "B")
				body := []byte(`{"protocol":"` + protocol + `"}`)
				apply := func(key productMediaKey) auth.MediaAuthorization {
					status, raw := f.request(t, key, http.MethodPost, boundaryMediaPath, body)
					require.Equal(t, http.StatusOK, status, "media admission must succeed")
					var envelope struct {
						Data auth.MediaAuthorization `json:"data"`
					}
					require.NoError(t, json.Unmarshal(raw, &envelope))
					require.NotEmpty(t, envelope.Data.AuthorizationID)
					return envelope.Data
				}
				aGrant, bGrant := apply(aKey), apply(bKey)
				require.NotEqual(t, aGrant.AuthorizationID, bGrant.AuthorizationID)
				var issued models.PlayGrant
				require.NoError(t, f.db.First(&issued, "grant_id = ?", aGrant.AuthorizationID).Error)
				require.Equal(t, models.GrantStateIssued, issued.State)
				require.Equal(t, a.ID, issued.ClientID)
				f.rejectMedia(t, f.bareURL, protocol)
				aPlayer := f.probe.playerURL(t, protocol, aGrant.URL)
				bPlayer := f.probe.playerURL(t, protocol, bGrant.URL)
				uPlayer := f.probe.playerURL(t, protocol, f.backgroundURL)
				var aViewer, bViewer models.Viewer
				require.NoError(t, f.db.First(&aViewer, "grant_id = ?", aGrant.AuthorizationID).Error)
				require.NoError(t, f.db.First(&bViewer, "grant_id = ?", bGrant.AuthorizationID).Error)
				require.Equal(t, models.ViewerStateActive, aViewer.State)
				require.Equal(t, models.ViewerStateActive, bViewer.State)
				require.NotEqual(t, aViewer.Identifier, bViewer.Identifier)
				f.checkPlayers(t, aViewer.Identifier, bViewer.Identifier, 3)
				f.rejectMedia(t, aGrant.URL, protocol) // consumed grant cannot create a second viewer
				publisher := f.publisher(t)
				generation := f.probe.generationApp("rtp", f.stream)
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
				status, _ := f.request(t, aKey, http.MethodPost, boundaryMediaPath, body)
				if action == "scope" {
					require.Equal(t, http.StatusForbidden, status)
				} else {
					require.Equal(t, http.StatusUnauthorized, status)
				}
				status, _ = f.request(t, aKey, http.MethodGet, "/openapi/v1/devices", nil)
				if action == "scope" {
					require.Equal(t, http.StatusOK, status)
				} else {
					require.Equal(t, http.StatusUnauthorized, status)
				}
				status, _ = f.request(t, bKey, http.MethodGet, "/openapi/v1/devices", nil)
				require.Equal(t, http.StatusOK, status)
				f.rejectMedia(t, aGrant.URL, protocol)
				reports := make(chan error, 8)
				stop, err := media.StartConfiguredRevocation(ctx, f.db, liveRevocationRegistry{f.node}, f.bindingsPath,
					func(_ media.RevocationTickResult, err error) {
						select {
						case reports <- err:
						case <-ctx.Done():
						}
					})
				require.NoError(t, err)
				defer stop()
				deadline := time.NewTimer(5*time.Second - time.Since(started))
				defer deadline.Stop()
				closed := false
				for !closed {
					select {
					case err := <-reports:
						require.NoError(t, err)
						require.NoError(t, f.db.First(&aViewer, aViewer.ID).Error)
						closed = aViewer.State == models.ViewerStateClosed
					case <-deadline.C:
						t.Fatalf("product Hook + worker did not close A within five seconds: state=%s class=%s attempts=%d", aViewer.State, aViewer.LastErrorClass, aViewer.Attempts)
					}
				}
				select {
				case <-aPlayer.done:
				case <-deadline.C:
					t.Fatal("A did not disconnect")
				}
				elapsed := time.Since(started)
				require.Less(t, elapsed, 5*time.Second)
				stop()
				require.Equal(t, media.RevocationErrorKicked, aViewer.LastErrorClass)
				f.checkPlayers(t, "", bViewer.Identifier, 2)
				progress, err := playauth.NewOpenAPIRevocationStore(f.db, nil).Progress(ctx, a.ID)
				require.NoError(t, err)
				require.Equal(t, playauth.OpenAPIRevocationStatusClosed, progress.Status)
				for second := 0; second < 30; second++ {
					beforeB, beforeU := bPlayer.bytes.Load(), uPlayer.bytes.Load()
					time.Sleep(time.Second)
					for _, p := range []*probePlayer{bPlayer, uPlayer} {
						select {
						case <-p.done:
							t.Fatal("original survivor disconnected")
						default:
						}
					}
					require.Greater(t, bPlayer.bytes.Load(), beforeB)
					require.Greater(t, uPlayer.bytes.Load(), beforeU)
				}
				require.Equal(t, publisher, f.publisher(t))
				require.Equal(t, generation, f.probe.generationApp("rtp", f.stream))
				f.checkPlayers(t, "", bViewer.Identifier, 2)
				var persistedB models.Viewer
				require.NoError(t, f.db.First(&persistedB, bViewer.ID).Error)
				require.Equal(t, bViewer, persistedB)
				f.rejectMedia(t, aGrant.URL, protocol)
				if action != "revoked" {
					if action == "scope" {
						_, err = f.service.SetScope(ctx, a.ID, "play:live:apply", true, a.RowVersion+1, 7)
					} else {
						_, err = f.service.SetStatus(ctx, a.ID, models.StatusActive, a.RowVersion+1, 7)
					}
					require.NoError(t, err)
					f.rejectMedia(t, aGrant.URL, protocol)
					fresh := apply(aKey)
					require.NotEqual(t, aGrant.AuthorizationID, fresh.AuthorizationID)
					reopened := f.probe.playerURL(t, protocol, fresh.URL)
					reopened.close()
					var old models.Viewer
					require.NoError(t, f.db.First(&old, aViewer.ID).Error)
					require.Equal(t, models.ViewerStateClosed, old.State)
				}
				t.Logf("real HMAC/v3/product Hook + %s: A fresh closed in %s, B and backend token sockets continuous 30s", action, elapsed)
			})
		}
	}
}
