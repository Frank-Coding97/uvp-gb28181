package recording

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

type contentDownloaderFunc func(context.Context, string, string) (*zlm.DownloadResponse, error)

func (f contentDownloaderFunc) DownloadFile(ctx context.Context, path, byteRange string) (*zlm.DownloadResponse, error) {
	return f(ctx, path, byteRange)
}

func TestContentProxyStreamsAndWhitelistsHeaders(t *testing.T) {
	for _, tc := range []struct {
		name       string
		byteRange  string
		statusCode int
	}{
		{name: "full", statusCode: http.StatusOK},
		{name: "range", byteRange: "bytes=10-19", statusCode: http.StatusPartialContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			downloaded := false
			downloader := contentDownloaderFunc(func(_ context.Context, path, byteRange string) (*zlm.DownloadResponse, error) {
				downloaded = true
				require.Equal(t, "/private/camera.mp4", path)
				require.Equal(t, tc.byteRange, byteRange)
				header := http.Header{
					"Content-Type":         []string{"video/mp4"},
					"Content-Length":       []string{"9"},
					"Content-Range":        []string{"bytes 10-18/100"},
					"Accept-Ranges":        []string{"bytes"},
					"Etag":                 []string{"recording-etag"},
					"Set-Cookie":           []string{"secret=cookie"},
					"Authorization":        []string{"Bearer secret"},
					"X-Internal-Node-Host": []string{"10.0.0.5"},
				}
				return &zlm.DownloadResponse{StatusCode: tc.statusCode, Header: header, Body: io.NopCloser(strings.NewReader("mp4-bytes"))}, nil
			})

			recorder := httptest.NewRecorder()
			err := NewContentProxy().Stream(context.Background(), recorder, downloader, ContentRequest{
				FilePath: "/private/camera.mp4", FileName: "camera.mp4", Mode: CapabilityModePlay,
				Range: tc.byteRange, FileSize: uint64Ptr(100),
			})
			require.NoError(t, err)
			require.True(t, downloaded)
			require.Equal(t, tc.statusCode, recorder.Code)
			require.Equal(t, "mp4-bytes", recorder.Body.String())
			require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
			require.Equal(t, "bytes", recorder.Header().Get("Accept-Ranges"))
			require.Equal(t, "recording-etag", recorder.Header().Get("ETag"))
			require.Empty(t, recorder.Header().Get("Set-Cookie"))
			require.Empty(t, recorder.Header().Get("Authorization"))
			require.Empty(t, recorder.Header().Get("X-Internal-Node-Host"))
			require.Contains(t, recorder.Header().Get("Content-Disposition"), "inline")
		})
	}
}

func TestContentProxyRejectsInvalidRangesBeforeUpstream(t *testing.T) {
	for _, byteRange := range []string{
		"items=0-1", "bytes=0-1,3-4", "bytes=-10", "bytes=20-10", "bytes=100-", "bytes=0-100", "bytes=abc-def",
	} {
		t.Run(byteRange, func(t *testing.T) {
			called := false
			downloader := contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
				called = true
				return nil, nil
			})
			err := NewContentProxy().Stream(context.Background(), httptest.NewRecorder(), downloader, ContentRequest{
				FilePath: "/record/file.mp4", FileName: "file.mp4", Mode: CapabilityModePlay,
				Range: byteRange, FileSize: uint64Ptr(100),
			})
			require.ErrorIs(t, err, ErrContentRangeInvalid)
			require.False(t, called)
		})
	}
}

func TestContentProxyRejectsRedirectAndMapsUpstreamStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{status: http.StatusFound, want: ErrContentRedirect},
		{status: http.StatusNotFound, want: zlm.ErrRecordingNotFound},
		{status: http.StatusRequestedRangeNotSatisfiable, want: ErrContentRangeInvalid},
		{status: http.StatusForbidden, want: zlm.ErrRecordingAccessUnavailable},
		{status: http.StatusBadGateway, want: zlm.ErrRecordingNodeUnavailable},
	} {
		downloader := contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
			return &zlm.DownloadResponse{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://internal/final"}}, Body: io.NopCloser(strings.NewReader("private"))}, nil
		})
		recorder := httptest.NewRecorder()
		err := NewContentProxy().Stream(context.Background(), recorder, downloader, ContentRequest{FilePath: "/record/file.mp4", FileName: "file.mp4", Mode: CapabilityModePlay})
		require.ErrorIs(t, err, tc.want, "status=%d", tc.status)
		require.Empty(t, recorder.Header().Get("Location"))
		require.Empty(t, recorder.Body.String())
	}
}

func TestContentProxyUsesBoundedBufferAndPropagatesCancellation(t *testing.T) {
	reader := &boundedReader{remaining: 2 * 1024 * 1024}
	downloader := contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
		return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}, nil
	})
	recorder := deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	require.NoError(t, NewContentProxy().Stream(context.Background(), recorder, downloader, ContentRequest{FilePath: "/record/large.mp4", FileName: "large.mp4", Mode: CapabilityModeDownload}))
	require.LessOrEqual(t, reader.maxRead, contentProxyBufferSize)
	require.Contains(t, recorder.Header().Get("Content-Disposition"), "attachment")

	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan struct{})
	downloader = contentDownloaderFunc(func(ctx context.Context, _, _ string) (*zlm.DownloadResponse, error) {
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() {
		result <- NewContentProxy().Stream(ctx, httptest.NewRecorder(), downloader, ContentRequest{FilePath: "/record/file.mp4", FileName: "file.mp4", Mode: CapabilityModePlay})
	}()
	cancel()
	<-canceled
	require.ErrorIs(t, <-result, context.Canceled)
}

type boundedReader struct {
	remaining int
	maxRead   int
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	r.remaining -= n
	return n, nil
}

func (r *boundedReader) Close() error { return nil }

func uint64Ptr(value uint64) *uint64 { return &value }

func TestContentProxyDoesNotLeakSensitiveInputInErrors(t *testing.T) {
	downloadErr := errors.New("transport failure")
	downloader := contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
		return nil, downloadErr
	})
	err := NewContentProxy().Stream(context.Background(), httptest.NewRecorder(), downloader, ContentRequest{
		FilePath: "/secret/node/path.mp4", FileName: "path.mp4", Mode: CapabilityModePlay,
	})
	require.ErrorIs(t, err, ErrContentUpstream)
	require.NotContains(t, err.Error(), "/secret/node/path.mp4")
}

func TestContentProxyDownloadAllowsOnlyInitialRangeAndChecksLength(t *testing.T) {
	for _, byteRange := range []string{"bytes=1-", "bytes=-10", "bytes=0-9", "bytes=0-1,3-4"} {
		require.ErrorIs(t, validateDownloadRange(byteRange), ErrContentRangeInvalid)
	}
	require.NoError(t, validateDownloadRange(""))
	require.NoError(t, validateDownloadRange("bytes=0-"))

	fileSize := uint64(9)
	called := false
	err := NewContentProxy().StreamDownload(context.Background(), deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}, contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
		called = true
		return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": []string{"8"}}, Body: io.NopCloser(strings.NewReader("too-short"))}, nil
	}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", FileSize: &fileSize, Mode: CapabilityModeDownload}, nil)
	require.True(t, called)
	require.ErrorIs(t, err, ErrContentUpstream)
}

func TestContentProxyDownloadUsesSingleFullResponseAndProgress(t *testing.T) {
	progress := uint64(0)
	fileSize := uint64(9)
	recorder := deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	err := NewContentProxy().StreamDownload(context.Background(), recorder, contentDownloaderFunc(func(_ context.Context, path, byteRange string) (*zlm.DownloadResponse, error) {
		require.Equal(t, "/private/camera.mp4", path)
		require.Empty(t, byteRange)
		return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}, "Content-Length": []string{"9"}, "Set-Cookie": []string{"hidden"}}, Body: io.NopCloser(strings.NewReader("mp4-bytes"))}, nil
	}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", FileSize: &fileSize, Mode: CapabilityModeDownload}, func(written uint64) { progress += written })
	require.NoError(t, err)
	require.Equal(t, "mp4-bytes", recorder.Body.String())
	require.EqualValues(t, 9, progress)
	require.Equal(t, "no", recorder.Header().Get("X-Accel-Buffering"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Contains(t, recorder.Header().Get("Content-Disposition"), "attachment")
}

func TestContentProxyDownloadRequiresDeadlineCapableWriter(t *testing.T) {
	called := false
	err := NewContentProxy().StreamDownload(context.Background(), httptest.NewRecorder(), contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
		called = true
		return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": []string{"1"}}, Body: io.NopCloser(strings.NewReader("x"))}, nil
	}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", Mode: CapabilityModeDownload}, nil)

	require.ErrorIs(t, err, ErrContentDeadline)
	require.False(t, called, "unsupported writers must fail before opening the upstream response")
}

func TestContentProxyDownloadTimesOutWhenUpstreamBodyStopsMakingProgress(t *testing.T) {
	body := newBlockingReadCloser()
	downloaderContextCancelled := make(chan struct{})
	proxy := &ContentProxy{writeDeadline: time.Second, inactivityTimeout: 20 * time.Millisecond}
	result := make(chan error, 1)
	go func() {
		result <- proxy.StreamDownload(context.Background(), deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}, contentDownloaderFunc(func(ctx context.Context, _ string, _ string) (*zlm.DownloadResponse, error) {
			go func() {
				<-ctx.Done()
				close(downloaderContextCancelled)
			}()
			return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
		}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", Mode: CapabilityModeDownload}, nil)
	}()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("upstream body was not read")
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, ErrContentTimeout)
	case <-time.After(time.Second):
		t.Fatal("stalled upstream body was not cancelled")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("stalled upstream body was not closed")
	}
	select {
	case <-downloaderContextCancelled:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not cancel the context passed to the downloader")
	}
}

func TestContentProxyDownloadCancelsAndClosesUpstreamOnRequestCancellation(t *testing.T) {
	body := newBlockingReadCloser()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy := &ContentProxy{writeDeadline: time.Second, inactivityTimeout: time.Second}
	result := make(chan error, 1)
	go func() {
		result <- proxy.StreamDownload(ctx, deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}, contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
			return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
		}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", Mode: CapabilityModeDownload}, nil)
	}()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("upstream body was not read")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop the upstream body")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("request cancellation did not close the upstream body")
	}
}

func TestContentProxyRefreshesDeadlineDuringLongRunningHTTPDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		chunkCount = 8
		chunkDelay = 50 * time.Millisecond
	)
	body := &slowChunkReadCloser{chunk: bytes.Repeat([]byte("x"), contentProxyBufferSize), remaining: chunkCount, delay: chunkDelay}
	proxy := &ContentProxy{writeDeadline: 500 * time.Millisecond, inactivityTimeout: 500 * time.Millisecond}
	router := gin.New()
	router.GET("/download", func(ctx *gin.Context) {
		err := proxy.StreamDownload(ctx.Request.Context(), ctx.Writer, contentDownloaderFunc(func(context.Context, string, string) (*zlm.DownloadResponse, error) {
			return &zlm.DownloadResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": []string{strconv.Itoa(chunkCount * contentProxyBufferSize)}}, Body: body}, nil
		}), ContentRequest{FilePath: "/private/camera.mp4", FileName: "camera.mp4", Mode: CapabilityModeDownload}, nil)
		if err != nil {
			ctx.Error(err)
		}
	})
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 150 * time.Millisecond
	server.Start()
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/download")
	require.NoError(t, err)
	defer response.Body.Close()
	downloaded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Len(t, downloaded, chunkCount*contentProxyBufferSize)
	require.Greater(t, time.Duration(chunkCount)*chunkDelay, server.Config.WriteTimeout)
}

type blockingReadCloser struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{started: make(chan struct{}), closed: make(chan struct{})}
}

func (r *blockingReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-r.closed
	return 0, errors.New("body closed")
}

func (r *blockingReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

type slowChunkReadCloser struct {
	chunk     []byte
	remaining int
	delay     time.Duration
}

func (r *slowChunkReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	r.remaining--
	return copy(buffer, r.chunk), nil
}

func (*slowChunkReadCloser) Close() error { return nil }

type deadlineRecorder struct{ *httptest.ResponseRecorder }

func (deadlineRecorder) SetWriteDeadline(time.Time) error { return nil }
