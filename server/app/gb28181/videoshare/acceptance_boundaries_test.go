package videoshare

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestAcceptanceGlobalTTLSnapshotsIssuedLinks(t *testing.T) {
	for _, seconds := range []int{180, 300, 1800, 3600, 420} {
		for _, issuance := range []string{"manual", "pending-first-link"} {
			t.Run(fmt.Sprintf("%d/%s", seconds, issuance), func(t *testing.T) {
				service, channel, now := newServiceFixture(t)
				ttl := time.Duration(seconds) * time.Second
				service.ttl = func() time.Duration { return ttl }
				var link Link
				var err error
				if issuance == "manual" {
					link, err = service.SetEnabled(context.Background(), channel, 9, true, 0)
				} else {
					require.NoError(t, service.EnsurePending(context.Background(), channel, 1, "34020000002000000001", 7, nil, 3))
					pending, statusErr := service.Status(context.Background(), channel, 9)
					require.NoError(t, statusErr)
					require.True(t, pending.Pending)
					require.Nil(t, pending.ExpiresAt)
					link, err = service.AcquireLink(context.Background(), channel, 9, pending.Revision)
				}
				require.NoError(t, err)
				require.Equal(t, now.Add(ttl), *link.ExpiresAt)
				ttl = 2 * time.Hour
				again, err := service.AcquireLink(context.Background(), channel, 9, link.Revision)
				require.NoError(t, err)
				require.Equal(t, link.ExpiresAt, again.ExpiresAt)
				require.Equal(t, link.Signature, again.Signature)
				stored, err := service.Resolve(context.Background(), link.PublicID, link.Signature)
				require.NoError(t, err)
				require.Equal(t, link.ExpiresAt, stored.ExpiresAt)
			})
		}
	}
}

func TestAcceptanceExactExpiryBoundary(t *testing.T) {
	for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		t.Run(offset.String(), func(t *testing.T) {
			service, channel, now := newServiceFixture(t)
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			service.now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			link, err := service.SetEnabled(context.Background(), channel, 9, true, 0)
			require.NoError(t, err)
			service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
				return PlaybackSource{StreamID: "stream-1", Generation: 3, HLSURL: "http://media.invalid/rtp/stream-1/hls.m3u8"}, nil
			}))
			t.Cleanup(service.DrainViewers)
			viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
			require.NoError(t, err)
			clock.Store(link.ExpiresAt.Add(offset).UnixNano())
			_, resolveErr := service.Resolve(context.Background(), link.PublicID, link.Signature)
			_, viewerErr := service.LookupViewer(context.Background(), viewer.Token)
			status, err := service.Status(context.Background(), channel, 9)
			require.NoError(t, err)
			if offset < 0 {
				require.NoError(t, resolveErr)
				require.NoError(t, viewerErr)
				require.True(t, status.Enabled)
				require.False(t, status.Expired)
			} else {
				require.ErrorIs(t, resolveErr, ErrExpired)
				require.ErrorIs(t, viewerErr, ErrExpired)
				require.False(t, status.Enabled)
				require.True(t, status.Expired)
			}
		})
	}
}

func TestAcceptanceOldGenerationCancellationKeepsNewMediaAlive(t *testing.T) {
	started, allowBody := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(allowBody) }) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-allowBody:
			_, _ = io.WriteString(w, "new-generation-media")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer release.Do(func() { close(allowBody) })
	service, oldViewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	t.Cleanup(service.DrainViewers)
	old, err := service.LookupViewer(context.Background(), oldViewer.Token)
	require.NoError(t, err)
	disabled, err := service.SetEnabled(context.Background(), old.Share.ChannelPK, 9, false, old.Share.Revision)
	require.NoError(t, err)
	link, err := service.SetEnabled(context.Background(), old.Share.ChannelPK, 9, true, disabled.Revision)
	require.NoError(t, err)
	require.NotEqual(t, old.Generation, link.Generation)
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	resource, err := service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	require.NoError(t, err)
	response, done := httptest.NewRecorder(), make(chan struct{})
	go func() {
		defer close(done)
		service.RelayHLS(context.Background(), response, viewer.Token, resource)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("new generation media did not start")
	}
	_, err = service.LookupViewer(context.Background(), oldViewer.Token)
	require.ErrorIs(t, err, ErrExpired)
	select {
	case <-old.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("old generation was not cancelled")
	}
	require.NoError(t, service.CloseViewer(oldViewer.Token))
	release.Do(func() { close(allowBody) })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("new generation media did not complete")
	}
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "new-generation-media", response.Body.String())
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.NoError(t, err)
}

func acceptanceHLSResource(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "/api/public/video-share/viewers/") {
			return line[strings.LastIndex(line, "/")+1:]
		}
	}
	t.Fatal("rewritten playlist has no viewer resource")
	return ""
}

func TestAcceptanceHLSNestedPlaylistAndViewerResourceBoundaries(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/rtp/stream-1/hls.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\nlow/index.m3u8\n")
		case "/rtp/stream-1/low/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:2,\nsegment.ts\n")
		case "/rtp/stream-1/low/segment.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = io.WriteString(w, "nested-segment")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	t.Cleanup(service.DrainViewers)
	manifest := httptest.NewRecorder()
	service.RelayHLS(context.Background(), manifest, viewer.Token, "")
	require.Equal(t, http.StatusOK, manifest.Code)
	nestedID := acceptanceHLSResource(t, manifest.Body.String())
	nested := httptest.NewRecorder()
	service.RelayHLS(context.Background(), nested, viewer.Token, nestedID)
	require.Equal(t, http.StatusOK, nested.Code)
	require.NotContains(t, nested.Body.String(), upstream.URL)
	segmentID := acceptanceHLSResource(t, nested.Body.String())
	segment := httptest.NewRecorder()
	service.RelayHLS(context.Background(), segment, viewer.Token, segmentID)
	require.Equal(t, http.StatusOK, segment.Code)
	require.Equal(t, "nested-segment", segment.Body.String())

	access, err := service.LookupViewer(context.Background(), viewer.Token)
	require.NoError(t, err)
	var link Link
	require.NoError(t, service.link(access.Share, &link))
	other, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	before := requests.Load()
	for _, resource := range []struct{ token, id string }{{viewer.Token, "unknown-resource"}, {other.Token, segmentID}} {
		response := httptest.NewRecorder()
		service.RelayHLS(context.Background(), response, resource.token, resource.id)
		require.Equal(t, http.StatusGone, response.Code)
		require.NotContains(t, response.Body.String(), "nested-segment")
	}
	require.Equal(t, before, requests.Load(), "rejected resource requests must not contact upstream")
}

func TestAcceptanceHLSRejectsTraversalReferencesBeforeMediaFetch(t *testing.T) {
	for _, reference := range []string{"../outside.ts", "low/../../outside.ts", "%2e%2e/outside.ts", "low/%2e%2e/outside.ts", "/rtp/other/segment.ts", `low\segment.ts`} {
		t.Run(reference, func(t *testing.T) {
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
				_, _ = io.WriteString(w, "#EXTM3U\n"+reference+"\n")
			}))
			defer upstream.Close()
			service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
			t.Cleanup(service.DrainViewers)
			response := httptest.NewRecorder()
			service.RelayHLS(context.Background(), response, viewer.Token, "")
			require.Equal(t, http.StatusBadGateway, response.Code)
			require.EqualValues(t, 1, requests.Load(), "unsafe URI must not trigger any media request")
			require.NotContains(t, response.Body.String(), reference)
		})
	}
}

func TestAcceptanceHLSRangeRequestsReturnCompleteAuthorizedResource(t *testing.T) {
	var forwardedRanges atomic.Int32
	const payload = "complete-authorized-segment"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			forwardedRanges.Add(1)
		}
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = io.WriteString(w, payload)
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	t.Cleanup(service.DrainViewers)
	resourceID, err := service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	require.NoError(t, err)
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.RelayHLS(r.Context(), w, viewer.Token, resourceID)
	}))
	defer downstream.Close()
	client := downstream.Client()
	client.Timeout = 3 * time.Second
	for _, requestedRange := range []string{"bytes=0-3", "bytes=1000000-", "bytes=0-1,4-5"} {
		request, err := http.NewRequest(http.MethodGet, downstream.URL, nil)
		require.NoError(t, err)
		request.Header.Set("Range", requestedRange)
		response, err := client.Do(request)
		require.NoError(t, err)
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		require.NoError(t, readErr)
		// Ignoring Range and returning the complete representation with 200 is
		// valid HTTP behavior; this relay does not promise partial responses.
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Empty(t, response.Header.Get("Content-Range"))
		require.Equal(t, payload, string(body))
	}
	require.Zero(t, forwardedRanges.Load())
}
