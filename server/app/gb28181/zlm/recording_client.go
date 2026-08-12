package zlm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"
)

var (
	ErrRecordingNotFound          = errors.New("zlm recording not found")
	ErrRecordingNodeUnavailable   = errors.New("zlm recording node unavailable")
	ErrRecordingAccessUnavailable = errors.New("zlm recording access unavailable")
	ErrRecordingResponseTimeout   = errors.New("zlm recording response timeout")
)

// MP4RecordFile is the partial file fact available from getMP4RecordFile.
// ZLM returns only the root directory and relative paths, so hook-only metadata
// remains nil until a later indexing step enriches the record.
type MP4RecordFile struct {
	FilePath   string
	FileName   string
	Folder     string
	URL        string
	StartTime  *int64
	DurationMS *int64
	FileSize   *int64
}

// DownloadResponse keeps the upstream body open for a caller to stream.
// The caller owns closing Body.
type DownloadResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

const recordingResponseHeaderTimeout = 15 * time.Second

var defaultRecordingDownloadHTTPClient = newRecordingDownloadHTTPClient(recordingResponseHeaderTimeout)

func newRecordingDownloadHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	return &http.Client{
		Transport: recordingDownloadTransport(responseHeaderTimeout),
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func recordingDownloadTransport(responseHeaderTimeout time.Duration) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return transport
}

func (c *Client) recordingDownloadHTTPClient() *http.Client {
	if c.downloadHTTP != nil {
		return c.downloadHTTP
	}
	return defaultRecordingDownloadHTTPClient
}

// GetMP4RecordFiles lists MP4 files for one known ZLM media tuple and date.
func (c *Client) GetMP4RecordFiles(ctx context.Context, vhost, appName, stream, period string) ([]MP4RecordFile, error) {
	var response struct {
		baseResp
		Data struct {
			RootPath string   `json:"rootPath"`
			Paths    []string `json:"paths"`
		} `json:"data"`
	}
	if err := c.call(ctx, "getMP4RecordFile", map[string]string{
		"vhost": vhost, "app": appName, "stream": stream, "period": period,
	}, &response); err != nil {
		return nil, classifyRecordingControlError(err)
	}
	if response.Code != 0 {
		return nil, classifyRecordingCode(response.Code)
	}

	files := make([]MP4RecordFile, 0, len(response.Data.Paths))
	for _, recordPath := range response.Data.Paths {
		files = append(files, MP4RecordFile{
			FilePath: joinRecordPath(response.Data.RootPath, recordPath),
			FileName: path.Base(recordPath),
			Folder:   response.Data.RootPath,
		})
	}
	return files, nil
}

func classifyRecordingCode(code int) error {
	if code == -500 {
		return fmt.Errorf("%w: zlm code %d", ErrRecordingNotFound, code)
	}
	return fmt.Errorf("%w: zlm code %d", ErrRecordingAccessUnavailable, code)
}

func classifyRecordingControlError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: request failed", ErrRecordingNodeUnavailable)
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		return fmt.Errorf("%w: malformed response", ErrRecordingAccessUnavailable)
	}
	return fmt.Errorf("%w: request failed", ErrRecordingNodeUnavailable)
}

func joinRecordPath(rootPath, recordPath string) string {
	if rootPath == "" {
		return recordPath
	}
	return strings.TrimRight(rootPath, "/") + "/" + strings.TrimLeft(recordPath, "/")
}

// DownloadFile opens ZLM's downloadFile response without buffering its body.
// It forwards only an explicitly supplied Range header and never follows redirects.
func (c *Client) DownloadFile(ctx context.Context, filePath, byteRange string) (*DownloadResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("secret", c.secret)
	query.Set("file_path", filePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/downloadFile?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: request build failed", ErrRecordingAccessUnavailable)
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	var gotConnection atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { gotConnection.Store(true) },
	}))

	resp, err := c.recordingDownloadHTTPClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var networkErr net.Error
		if gotConnection.Load() && errors.As(err, &networkErr) && networkErr.Timeout() {
			return nil, fmt.Errorf("%w: response headers timed out", ErrRecordingResponseTimeout)
		}
		return nil, fmt.Errorf("%w: download request failed", ErrRecordingNodeUnavailable)
	}
	return &DownloadResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
}
