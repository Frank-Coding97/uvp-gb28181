package videoshare

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestHTTPFLVRefreshesUpstreamCredentialBeforeOpeningMedia(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("play_token") != "fresh-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = io.WriteString(w, "FLV")
	}))
	defer upstream.Close()
	service, channelPK, _ := newServiceFixture(t)
	t.Cleanup(service.DrainViewers)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	var prepares, released atomic.Int32
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HTTPFLVURL: fmt.Sprintf("%s?play_token=fresh-%d", upstream.URL, prepares.Add(1)), Release: func() { released.Add(1) }}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	service.RelayHTTPFLV(context.Background(), w, viewer.Token)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "FLV", w.Body.String())
	require.EqualValues(t, 2, released.Load(), "media connection exit must release both request and viewer leases")
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.ErrorIs(t, err, ErrExpired)
}

func TestViewerWithoutMediaActivityReleasesItsLease(t *testing.T) {
	service, channelPK, _ := newServiceFixture(t)
	t.Cleanup(service.DrainViewers)
	service.viewerIdleTimeout = 60 * time.Millisecond
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	var released atomic.Int32
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: "http://zlm/hls.m3u8", Release: func() { released.Add(1) }}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return released.Load() == 1 }, time.Second, 10*time.Millisecond)
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.ErrorIs(t, err, ErrExpired)
	// The public link remains reusable: idle cleanup only retires this viewer.
	_, err = service.Resolve(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
}

func TestHLSKeepsActiveRequestsAndReclaimsIdleViewer(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-finish:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\nsegment.ts\n")
	}))
	defer upstream.Close()
	service, channelPK, _ := newServiceFixture(t)
	t.Cleanup(service.DrainViewers)
	service.viewerIdleTimeout = 60 * time.Millisecond
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	var released atomic.Int32
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "s", Generation: 1, HLSURL: upstream.URL + "/rtp/s/hls.m3u8", Release: func() { released.Add(1) }}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	w, done := httptest.NewRecorder(), make(chan struct{})
	go func() { service.RelayHLS(context.Background(), w, viewer.Token, ""); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HLS upstream request did not start")
	}
	// An in-flight request must not be mistaken for a browser that has left.
	time.Sleep(3 * service.viewerIdleTimeout)
	require.Zero(t, released.Load())
	close(finish)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HLS relay did not finish")
	}
	require.Equal(t, http.StatusOK, w.Code)
	require.Eventually(t, func() bool { return released.Load() == 2 }, time.Second, 10*time.Millisecond)
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.ErrorIs(t, err, ErrExpired)
}
