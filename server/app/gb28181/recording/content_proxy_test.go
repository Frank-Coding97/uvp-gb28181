package recording

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	recorder := httptest.NewRecorder()
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
