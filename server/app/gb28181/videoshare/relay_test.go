package videoshare

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestHTTPFLVRelayStreamsWithoutLeakingUpstreamHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-flv")
		w.Header().Set("Set-Cookie", "secret=internal")
		w.Header().Set("Location", "http://internal/admin")
		_, _ = io.WriteString(w, "FLV-media-bytes")
	}))
	defer upstream.Close()
	service, viewer := relayViewer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "FLV-media-bytes", recorder.Body.String())
	require.Equal(t, "video/x-flv", recorder.Header().Get("Content-Type"))
	require.Equal(t, "no-store, private", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Empty(t, recorder.Header().Get("Location"))
}

func TestHTTPFLVRelayCancelsWhenViewerIsRevoked(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-flv")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "FLV")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	service, viewer := relayViewer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	require.NoError(t, service.CloseViewer(viewer.Token))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop after viewer revocation")
	}
}

func TestHTTPFLVRelayStopsAtNaturalShareExpiry(t *testing.T) {
	started := make(chan struct{})
	upstreamStopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamStopped)
		w.Header().Set("Content-Type", "video/x-flv")
		flusher, _ := w.(http.Flusher)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		close(started)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if _, err := io.WriteString(w, "FLV"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	service, channelPK, _ := newServiceFixture(t)
	link, expiresAt := shortLivedShare(t, service, channelPK, 900*time.Millisecond)
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HTTPFLVURL: upstream.URL}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	writer := &timestampResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		service.RelayHTTPFLV(request.Context(), writer, viewer.Token)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay remained active after the share expired")
	}
	select {
	case <-upstreamStopped:
	case <-time.After(time.Second):
		t.Fatal("upstream body remained open after the share expired")
	}
	require.WithinDuration(t, expiresAt, writer.lastWriteAt, 350*time.Millisecond)
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.ErrorIs(t, err, ErrExpired)
}

func TestHTTPFLVRelayReturnsUnavailableWhenAuthorizationStoreFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = io.WriteString(w, "FLV")
	}))
	defer upstream.Close()
	service, viewer := relayViewer(t, upstream.URL)
	require.NoError(t, service.db.Migrator().DropTable(&gbmodels.GbChannelVideoShare{}))

	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "no such table")
}

func TestHTTPFLVRelayStopsWhenAuthorizationStoreFails(t *testing.T) {
	started := make(chan struct{})
	upstreamStopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamStopped)
		w.Header().Set("Content-Type", "video/x-flv")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "FLV")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	service, viewer := relayViewer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	require.NoError(t, service.db.Migrator().DropTable(&gbmodels.GbChannelVideoShare{}))
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("relay remained active after authorization storage failed")
	}
	select {
	case <-upstreamStopped:
	case <-time.After(time.Second):
		t.Fatal("upstream body remained open after authorization storage failed")
	}
}

func TestHLSManifestReadStopsWhenAuthorizationStoreFails(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(stopped)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.RelayHLS(requestCtx, httptest.NewRecorder(), viewer.Token, "")
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("playlist request did not start")
	}
	require.NoError(t, service.db.Migrator().DropTable(&gbmodels.GbChannelVideoShare{}))
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		cancel()
		<-done
		t.Fatal("playlist read remained active after authorization storage failed")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("playlist upstream remained open")
	}
}

func TestHLSRelayStopsInFlightSegmentAtNaturalShareExpiry(t *testing.T) {
	started := make(chan struct{})
	upstreamStopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rtp/stream-1/hls.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:2,\nsegment.ts\n")
			return
		}
		defer close(upstreamStopped)
		w.Header().Set("Content-Type", "video/mp2t")
		flusher, _ := w.(http.Flusher)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		close(started)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				if _, err := io.WriteString(w, "segment"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	service, channelPK, _ := newServiceFixture(t)
	link, expiresAt := shortLivedShare(t, service, channelPK, 900*time.Millisecond)
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HLSURL: upstream.URL + "/rtp/stream-1/hls.m3u8"}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	resourceID, err := service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	require.NoError(t, err)
	writer := &timestampResponseWriter{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		service.RelayHLS(context.Background(), writer, viewer.Token, resourceID)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HLS segment request did not start")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HLS segment relay remained active after the share expired")
	}
	select {
	case <-upstreamStopped:
	case <-time.After(time.Second):
		t.Fatal("HLS segment body remained open after the share expired")
	}
	require.WithinDuration(t, expiresAt, writer.lastWriteAt, 350*time.Millisecond)
	_, err = service.LookupViewer(context.Background(), viewer.Token)
	require.ErrorIs(t, err, ErrExpired)
}

func shortLivedShare(t *testing.T, service *Service, channelPK uint, lifetime time.Duration) (Link, time.Time) {
	t.Helper()
	service.now = time.Now
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	var share gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("channel_pk = ?", channelPK).Take(&share).Error)
	expiresAt := time.Now().Add(lifetime).UTC()
	require.NoError(t, service.db.Model(&share).Update("expires_at", expiresAt).Error)
	link.Signature, err = service.signer.Sign(Claims{PublicID: share.PublicID, Generation: share.Generation, ExpiresAt: expiresAt.Unix(), KeyID: share.KeyID})
	require.NoError(t, err)
	return link, expiresAt
}

type timestampResponseWriter struct {
	*httptest.ResponseRecorder
	lastWriteAt time.Time
}

func (w *timestampResponseWriter) Write(data []byte) (int, error) {
	count, err := w.ResponseRecorder.Write(data)
	if count > 0 {
		w.lastWriteAt = time.Now()
	}
	return count, err
}

func TestHTTPFLVRelayDoesNotFollowUpstreamRedirect(t *testing.T) {
	var redirected atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Store(true) }))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	service, viewer := relayViewer(t, upstream.URL)
	request := httptest.NewRequest(http.MethodGet, viewer.HTTPFLVPath, nil)
	recorder := httptest.NewRecorder()
	service.RelayHTTPFLV(request.Context(), recorder, viewer.Token)
	require.NotEqual(t, http.StatusOK, recorder.Code)
	require.False(t, redirected.Load())
	require.NotContains(t, recorder.Body.String(), destination.URL)
}

func TestHLSRelayRewritesPlaylistAndProxiesViewerBoundResources(t *testing.T) {
	var requested atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested.Add(1)
		if r.URL.Path == "/rtp/stream-1/hls.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2,\nseg.ts\n")
			return
		}
		if r.URL.Path == "/rtp/stream-1/init.mp4" || r.URL.Path == "/rtp/stream-1/seg.ts" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "media-segment")
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	manifest := httptest.NewRecorder()
	service.RelayHLS(context.Background(), manifest, viewer.Token, "")
	require.Equal(t, http.StatusOK, manifest.Code, manifest.Body.String())
	require.NotContains(t, manifest.Body.String(), upstream.URL)
	require.Contains(t, manifest.Body.String(), "/hls/")
	paths := strings.Fields(manifest.Body.String())
	var resourceID string
	for _, line := range paths {
		if strings.Contains(line, "/hls/") && !strings.Contains(line, "index.m3u8") {
			resourceID = strings.Trim(line[strings.LastIndex(line, "/hls/")+5:], `"`)
			break
		}
	}
	require.NotEmpty(t, resourceID)
	segment := httptest.NewRecorder()
	service.RelayHLS(context.Background(), segment, viewer.Token, resourceID)
	require.Equal(t, http.StatusOK, segment.Code, segment.Body.String())
	require.Equal(t, "media-segment", segment.Body.String())
	require.EqualValues(t, 2, requested.Load())
}

func TestHLSRelayRejectsResourceAfterShareIsRevoked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if r.URL.Path == "/rtp/stream-1/hls.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\nsegment.ts\n")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "media-segment")
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	manifest := httptest.NewRecorder()
	service.RelayHLS(context.Background(), manifest, viewer.Token, "")
	require.Equal(t, http.StatusOK, manifest.Code, manifest.Body.String())
	resourceID := strings.TrimSpace(strings.Split(manifest.Body.String(), "\n")[1])
	resourceID = resourceID[strings.LastIndex(resourceID, "/")+1:]

	var share gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("public_id = ?", service.viewerSession(viewer.Token).publicID).Take(&share).Error)
	_, err := service.SetEnabled(context.Background(), share.ChannelPK, 9, false, share.Revision)
	require.NoError(t, err)

	segment := httptest.NewRecorder()
	service.RelayHLS(context.Background(), segment, viewer.Token, resourceID)
	require.Equal(t, http.StatusGone, segment.Code)
	require.NotContains(t, segment.Body.String(), "media-segment")
}

func TestHLSRelayStopsAnInFlightSegmentAfterShareIsRevoked(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "segment-start")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	resourceID := ""
	service.viewerSession(viewer.Token).resourceMu.Lock()
	for id := range service.viewerSession(viewer.Token).resources {
		resourceID = id
	}
	service.viewerSession(viewer.Token).resourceMu.Unlock()
	require.NotEmpty(t, resourceID)

	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		service.RelayHLS(context.Background(), response, viewer.Token, resourceID)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HLS segment request did not start")
	}
	var share gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("public_id = ?", service.viewerSession(viewer.Token).publicID).Take(&share).Error)
	_, err := service.SetEnabled(context.Background(), share.ChannelPK, 9, false, share.Revision)
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("in-flight HLS segment continued after share revocation")
	}
}

func TestViewerSessionsAreIndependent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/x-flv")
		_, _ = io.WriteString(w, "FLV-media-bytes")
	}))
	defer upstream.Close()
	service, first := relayViewer(t, upstream.URL)
	var share gbmodels.GbChannelVideoShare
	require.NoError(t, service.db.Where("public_id = ?", service.viewerSession(first.Token).publicID).Take(&share).Error)
	link := Link{}
	require.NoError(t, service.link(&share, &link))
	second, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	require.NoError(t, service.CloseViewer(first.Token))

	_, err = service.LookupViewer(context.Background(), second.Token)
	require.NoError(t, err)
	segment := httptest.NewRecorder()
	service.RelayHTTPFLV(httptest.NewRequest(http.MethodGet, second.HTTPFLVPath, nil).Context(), segment, second.Token)
	require.Equal(t, http.StatusOK, segment.Code, segment.Body.String())
	require.Equal(t, "FLV-media-bytes", segment.Body.String())
}

func TestHLSRelayRejectsExternalManifestReferences(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\nhttps://attacker.invalid/stream.ts\n")
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	manifest := httptest.NewRecorder()
	service.RelayHLS(context.Background(), manifest, viewer.Token, "")
	require.Equal(t, http.StatusBadGateway, manifest.Code)
	require.NotContains(t, manifest.Body.String(), "attacker.invalid")
}

func relayViewer(t *testing.T, flvURL string) (*Service, Viewer) {
	t.Helper()
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HTTPFLVURL: flvURL}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	return service, viewer
}

func relayViewerWithHLS(t *testing.T, hlsURL string) (*Service, Viewer) {
	t.Helper()
	service, channelPK, _ := newServiceFixture(t)
	link, err := service.SetEnabled(context.Background(), channelPK, 9, true, 0)
	require.NoError(t, err)
	service.SetPlaybackPreparer(playbackPreparerFunc(func(context.Context, *gbmodels.GbChannelVideoShare) (PlaybackSource, error) {
		return PlaybackSource{StreamID: "stream-1", Generation: 3, HLSURL: hlsURL}, nil
	}))
	viewer, err := service.CreateViewer(context.Background(), link.PublicID, link.Signature)
	require.NoError(t, err)
	return service, viewer
}
