package videoshare

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	basemodels "uvplatform.com/uvp-gb28181/app/models"
)

func TestNaturalExpiryPreservesStartedViewerAndManualClose(t *testing.T) {
	for _, scan := range []bool{false, true} {
		t.Run(fmt.Sprintf("scanned=%t", scan), func(t *testing.T) {
			s, channel, now := newServiceFixture(t)
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			t.Cleanup(s.DrainViewers)
			s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
			}))
			link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
			require.NoError(t, err)
			active, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			unused, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			_, err = s.LookupViewer(context.Background(), unused.Token)
			require.NoError(t, err, "polling a precreated viewer does not start media")
			finish, err := s.beginViewerRequest(active.Token)
			require.NoError(t, err)
			defer finish(false)
			require.True(t, s.confirmViewerMedia(active.Token))
			access, err := s.LookupViewer(context.Background(), active.Token)
			require.NoError(t, err)
			clock.Store(link.ExpiresAt.UnixNano())
			if scan {
				count, err := s.ScanExpired(context.Background(), 100)
				require.NoError(t, err)
				require.Equal(t, 1, count)
			}
			_, err = s.Resolve(context.Background(), link.PublicID, link.Signature)
			require.ErrorIs(t, err, ErrExpired)
			_, err = s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.ErrorIs(t, err, ErrExpired)
			_, err = s.LookupViewer(context.Background(), active.Token)
			require.NoError(t, err, "started media must survive natural expiry, including audit scan")
			require.NoError(t, access.Context.Err())
			_, err = s.beginViewerRequest(unused.Token)
			require.ErrorIs(t, err, ErrExpired, "a precreated viewer must not start media after expiry")
			status, err := s.Status(context.Background(), channel, 9)
			require.NoError(t, err)
			require.True(t, status.Enabled, "administrator must retain an explicit close operation")
			require.True(t, status.Expired)
			require.Equal(t, link.ExpiresAt, status.ExpiresAt)
			_, err = s.AcquireLink(context.Background(), channel, 9, status.Revision)
			require.ErrorIs(t, err, ErrExpired, "expired links must not renew implicitly")
			closed, err := s.SetEnabled(context.Background(), channel, 9, false, status.Revision)
			require.NoError(t, err)
			require.False(t, closed.Enabled)
			_, err = s.LookupViewer(context.Background(), active.Token)
			require.ErrorIs(t, err, ErrExpired)
			require.Error(t, access.Context.Err(), "manual close still cancels old media")
		})
	}
}

func TestExpiredStartedViewerStillRevokedPermanentlyByAuthorityWrites(t *testing.T) {
	for _, kind := range []string{"creator", "target", "permission", "regenerate"} {
		t.Run(kind, func(t *testing.T) {
			s, channel, now := newServiceFixture(t)
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			t.Cleanup(s.DrainViewers)
			s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
			}))
			link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
			require.NoError(t, err)
			viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			finish, err := s.beginViewerRequest(viewer.Token)
			require.NoError(t, err)
			defer finish(false)
			require.True(t, s.confirmViewerMedia(viewer.Token))
			clock.Store(link.ExpiresAt.UnixNano())
			_, err = s.ScanExpired(context.Background(), 100)
			require.NoError(t, err)
			status, err := s.Status(context.Background(), channel, 9)
			require.NoError(t, err)
			if kind == "permission" {
				require.NoError(t, s.db.AutoMigrate(&basemodels.User{}))
			}
			switch kind {
			case "regenerate":
				_, err = s.Regenerate(context.Background(), channel, 9, status.Revision)
			default:
				err = s.db.Transaction(func(tx *gorm.DB) error {
					switch kind {
					case "creator":
						return RevokeCreatorTx(context.Background(), tx, 9, "creator_disabled", s.now())
					case "target":
						return RevokeDeletedTargetTx(context.Background(), tx, 0, channel)
					default:
						return RevokeUnauthorizedSharesTx(context.Background(), tx, func(context.Context, uint, string, string) (bool, error) { return false, nil }, []uint{9}, "permission_removed", s.now())
					}
				})
			}
			require.NoError(t, err)
			_, err = s.LookupViewer(context.Background(), viewer.Token)
			require.ErrorIs(t, err, ErrExpired)
		})
	}
}

func TestExpiredCloseAuditFailureKeepsExistingViewer(t *testing.T) {
	s, channel, now := newServiceFixture(t)
	t.Cleanup(s.DrainViewers)
	s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
	}))
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	finish, err := s.beginViewerRequest(viewer.Token)
	require.NoError(t, err)
	defer finish(false)
	require.True(t, s.confirmViewerMedia(viewer.Token))
	s.now = func() time.Time { return now.Add(5 * time.Minute) }
	_, err = s.ScanExpired(context.Background(), 100)
	require.NoError(t, err)
	status, err := s.Status(context.Background(), channel, 9)
	require.NoError(t, err)
	boom := errors.New("audit unavailable")
	require.NoError(t, s.db.Callback().Create().Before("gorm:create").Register("test:close_audit", func(tx *gorm.DB) {
		if tx.Statement.Table == "sys_operation_logs" {
			tx.AddError(boom)
		}
	}))
	_, err = s.SetEnabled(context.Background(), channel, 9, false, status.Revision)
	require.ErrorIs(t, err, boom)
	_, err = s.LookupViewer(context.Background(), viewer.Token)
	require.NoError(t, err)
}

func TestHLSContinuesRequestingNewSegmentsAfterLinkExpiry(t *testing.T) {
	var segment atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:2,\nsegment-%d.ts\n", segment.Add(1))
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
			fmt.Fprint(w, r.URL.Path)
		}
	}))
	defer upstream.Close()
	s, channel, now := newServiceFixture(t)
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	t.Cleanup(s.DrainViewers)
	var prepares atomic.Int32
	s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: fmt.Sprintf("%s/rtp/s/hls.m3u8?token=%d", upstream.URL, prepares.Add(1))}, nil
	}))
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		manifest := httptest.NewRecorder()
		s.RelayHLS(context.Background(), manifest, viewer.Token, "")
		require.Equal(t, http.StatusOK, manifest.Code, manifest.Body.String())
		lines := strings.Split(strings.TrimSpace(manifest.Body.String()), "\n")
		resource := lines[len(lines)-1]
		piece := httptest.NewRecorder()
		s.RelayHLS(context.Background(), piece, viewer.Token, resource[strings.LastIndex(resource, "/")+1:])
		require.Equal(t, http.StatusOK, piece.Code, piece.Body.String())
		require.Contains(t, piece.Body.String(), fmt.Sprintf("segment-%d.ts", i+1))
		if i == 0 {
			clock.Store(link.ExpiresAt.Add(time.Minute).UnixNano())
			_, err = s.ScanExpired(context.Background(), 100)
			require.NoError(t, err)
		}
	}
	require.EqualValues(t, 7, prepares.Load(), "source credential refresh continues after link expiry")
}

func TestHLSResourceWindowRollsForwardDuringLongViewer(t *testing.T) {
	s, channel, _ := newServiceFixture(t)
	t.Cleanup(s.DrainViewers)
	s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://media.invalid/rtp/s/hls.m3u8"}, nil
	}))
	link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
	require.NoError(t, err)
	viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	// Production IDs are unbounded; the tiny fixture counter otherwise repeats after 256.
	s.random = func(dst []byte) error { _, err := rand.Read(dst); return err }
	persistent, err := s.mapHLSResource(viewer.Token, "/rtp/s/variant.m3u8")
	require.NoError(t, err)
	var first, last string
	for i := 0; i < 5000; i++ {
		// Native HLS can keep polling a child playlist without reloading master.
		resource, ok := s.resolveHLSResource(viewer.Token, persistent)
		require.True(t, ok, "a continuously read child playlist must remain available")
		require.Equal(t, "/rtp/s/variant.m3u8", resource)
		id, err := s.mapHLSResource(viewer.Token, fmt.Sprintf("/rtp/s/%d.ts", i))
		require.NoError(t, err)
		if i == 0 {
			first = id
		}
		last = id
	}
	session := s.viewerSession(viewer.Token)
	require.Len(t, session.resources, 4096)
	require.Len(t, session.resourceIDs, 4096)
	resource, ok := s.resolveHLSResource(viewer.Token, persistent)
	require.True(t, ok)
	require.Equal(t, "/rtp/s/variant.m3u8", resource)
	_, ok = s.resolveHLSResource(viewer.Token, first)
	require.False(t, ok)
	resource, ok = s.resolveHLSResource(viewer.Token, last)
	require.True(t, ok)
	require.Equal(t, "/rtp/s/4999.ts", resource)
}

func TestFailedMediaRequestsDoNotAdmitViewerPastLinkExpiry(t *testing.T) {
	for _, kind := range []string{"flv-upstream-failure", "hls-upstream-failure", "hls-invalid-resource"} {
		t.Run(kind, func(t *testing.T) {
			var failing atomic.Bool
			failing.Store(true)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failing.Load() {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				if strings.HasPrefix(kind, "flv") {
					w.Header().Set("Content-Type", "video/x-flv")
					fmt.Fprint(w, "FLV")
				} else {
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
				}
			}))
			defer upstream.Close()
			s, channel, now := newServiceFixture(t)
			t.Cleanup(s.DrainViewers)
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "s", Generation: 1, HTTPFLVURL: upstream.URL, HLSURL: upstream.URL + "/rtp/s/hls.m3u8"}, nil
			}))
			link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
			require.NoError(t, err)
			viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			response := httptest.NewRecorder()
			switch kind {
			case "flv-upstream-failure":
				s.RelayHTTPFLV(context.Background(), response, viewer.Token)
			case "hls-invalid-resource":
				s.RelayHLS(context.Background(), response, viewer.Token, "invalid")
			default:
				s.RelayHLS(context.Background(), response, viewer.Token, "")
			}
			require.NotEqual(t, http.StatusOK, response.Code)
			clock.Store(link.ExpiresAt.UnixNano())
			failing.Store(false)
			response = httptest.NewRecorder()
			if strings.HasPrefix(kind, "flv") {
				s.RelayHTTPFLV(context.Background(), response, viewer.Token)
			} else {
				s.RelayHLS(context.Background(), response, viewer.Token, "")
			}
			require.Equal(t, http.StatusGone, response.Code, "failed requests must not establish continuing playback")
		})
	}
}

func TestFirstSuccessfulMediaResponseMustArriveBeforeLinkExpiry(t *testing.T) {
	for _, kind := range []string{"flv", "hls"} {
		t.Run(kind, func(t *testing.T) {
			requested, release := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(requested)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if kind == "flv" {
					w.Header().Set("Content-Type", "video/x-flv")
					fmt.Fprint(w, "FLV")
				} else {
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
				}
			}))
			defer upstream.Close()
			s, channel, now := newServiceFixture(t)
			defer s.DrainViewers()
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			s.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			s.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "s", Generation: 1, HTTPFLVURL: upstream.URL, HLSURL: upstream.URL + "/rtp/s/hls.m3u8"}, nil
			}))
			link, err := s.SetEnabled(context.Background(), channel, 9, true, 0)
			require.NoError(t, err)
			viewer, err := s.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				if kind == "flv" {
					s.RelayHTTPFLV(context.Background(), response, viewer.Token)
				} else {
					s.RelayHLS(context.Background(), response, viewer.Token, "")
				}
			}()
			select {
			case <-requested:
			case <-time.After(time.Second):
				t.Fatal("upstream did not start")
			}
			clock.Store(link.ExpiresAt.UnixNano())
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("late initial media did not finish")
			}
			require.Equal(t, http.StatusGone, response.Code, "a pending request is not already watching")
		})
	}
}
