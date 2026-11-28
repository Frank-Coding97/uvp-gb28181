package videoshare

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Observe the real socket deadline before the handler returns, so the server's
// final chunk cannot win the race against a cancellation callback.
type hlsDeadlineObserver struct {
	http.ResponseWriter
	expiredApplied chan struct{}
	once           sync.Once
}

func (w *hlsDeadlineObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *hlsDeadlineObserver) SetWriteDeadline(deadline time.Time) error {
	err := http.NewResponseController(w.ResponseWriter).SetWriteDeadline(deadline)
	if !deadline.After(time.Now()) {
		w.once.Do(func() { close(w.expiredApplied) })
	}
	return err
}

func TestHLSRelayCompletesSegmentWithNormalHTTPEOF(t *testing.T) {
	payload := bytes.Repeat([]byte{0x47}, 73696)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	resourceID, err := service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	require.NoError(t, err)

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observer := &hlsDeadlineObserver{ResponseWriter: w, expiredApplied: make(chan struct{})}
		service.RelayHLS(r.Context(), observer, viewer.Token, resourceID)
		select {
		case <-observer.expiredApplied:
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer downstream.Close()
	client := downstream.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Get(downstream.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, response.TransferEncoding, "chunked")
	body, err := io.ReadAll(response.Body)
	require.Equal(t, payload, body, "all segment bytes must arrive before validating HTTP completion")
	require.NoError(t, err, "a complete HLS segment must end with normal HTTP EOF")
}

func TestHLSRelayPreservesTruncatedUpstreamFailure(t *testing.T) {
	payload := bytes.Repeat([]byte{0x47}, 73696)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)+188))
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()
	service, viewer := relayViewerWithHLS(t, upstream.URL+"/rtp/stream-1/hls.m3u8")
	resourceID, err := service.mapHLSResource(viewer.Token, "/rtp/stream-1/segment.ts")
	require.NoError(t, err)

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observer := &hlsDeadlineObserver{ResponseWriter: w, expiredApplied: make(chan struct{})}
		service.RelayHLS(r.Context(), observer, viewer.Token, resourceID)
		select {
		case <-observer.expiredApplied:
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer downstream.Close()
	client := downstream.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Get(downstream.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Contains(t, response.TransferEncoding, "chunked")
	body, err := io.ReadAll(response.Body)
	require.Equal(t, payload, body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "an incomplete upstream segment must not become a successful response")
}
