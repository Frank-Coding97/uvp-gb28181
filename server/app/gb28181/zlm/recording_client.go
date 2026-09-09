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
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// RecorderType matches ZLMediaKit's recorder enum: HLS is 0 and MP4 is 1.
// The legacy StartRecord/StopRecord/IsRecording methods remain MP4-only
// wrappers for existing GB28181 recording callers.
type RecorderType int

const (
	RecorderHLS RecorderType = iota
	RecorderMP4
)

// RecorderTypeHLS and RecorderTypeMP4 are explicit aliases for callers that
// prefer the enum-style names in API contracts.
const (
	RecorderTypeHLS = RecorderHLS
	RecorderTypeMP4 = RecorderMP4
)

var ErrRecorderTypeInvalid = errors.New("zlm recorder type invalid")

var (
	ErrRecordingNotFound          = errors.New("zlm recording not found")
	ErrRecordingPathInvalid       = errors.New("zlm recording path invalid")
	ErrRecordingDeleteFailed      = errors.New("zlm recording delete failed")
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

func (t RecorderType) valid() bool {
	return t == RecorderHLS || t == RecorderMP4
}

func typedRecordingParams(vhost, appName, stream string, recorderType RecorderType) map[string]string {
	return map[string]string{
		"type":   strconv.Itoa(int(recorderType)),
		"vhost":  vhost,
		"app":    appName,
		"stream": stream,
	}
}

func validateTypedRecordingRequest(vhost, appName, stream string, recorderType RecorderType) error {
	if !recorderType.valid() {
		return fmt.Errorf("%w: %d", ErrRecorderTypeInvalid, recorderType)
	}
	if strings.TrimSpace(vhost) == "" || strings.TrimSpace(appName) == "" || strings.TrimSpace(stream) == "" {
		return fmt.Errorf("%w: vhost、app、stream 不能为空", ErrRecordingPathInvalid)
	}
	return nil
}

// StartRecordWithType starts either HLS or MP4 recording for an existing ZLM
// media source. maxSecond=0 delegates the slice duration to ZLM config.
func (c *Client) StartRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType RecorderType, maxSecond int) error {
	return c.startRecord(ctx, vhost, appName, stream, recorderType, maxSecond, "")
}

// StartMP4RecordInDirectory uses a server-chosen recording root. Work recording
// callers must persist a unique directory per job; never pass a browser path.
// Empty paths are rejected because silently falling back to the shared default
// can overwrite a previous job's file when two recordings start in one second.
func (c *Client) StartMP4RecordInDirectory(ctx context.Context, vhost, appName, stream string, maxSecond int, directory string) error {
	if strings.TrimSpace(directory) == "" || strings.ContainsRune(directory, '\x00') {
		return ErrRecordingPathInvalid
	}
	return c.startRecord(ctx, vhost, appName, stream, RecorderMP4, maxSecond, directory)
}

func (c *Client) startRecord(ctx context.Context, vhost, appName, stream string, recorderType RecorderType, maxSecond int, directory string) error {
	if err := validateTypedRecordingRequest(vhost, appName, stream, recorderType); err != nil {
		return err
	}
	if maxSecond < 0 {
		return fmt.Errorf("%w: max_second 不能为负数", ErrRecordingPathInvalid)
	}
	var response struct {
		baseResp
		Result bool `json:"result"`
	}
	params := typedRecordingParams(vhost, appName, stream, recorderType)
	params["max_second"] = strconv.Itoa(maxSecond)
	if directory != "" {
		params["customized_path"] = directory
	}
	if err := c.call(ctx, "startRecord", params, &response); err != nil {
		return err
	}
	if response.Code != 0 || !response.Result {
		return fmt.Errorf("startRecord type=%d code=%d msg=%s", recorderType, response.Code, response.Msg)
	}
	return nil
}

// StopRecordWithType stops either HLS or MP4 recording. Missing streams are
// treated as already stopped, matching the legacy MP4 wrapper.
func (c *Client) StopRecordWithType(ctx context.Context, vhost, appName, stream string, recorderType RecorderType) error {
	if err := validateTypedRecordingRequest(vhost, appName, stream, recorderType); err != nil {
		return err
	}
	var response struct {
		baseResp
		Result bool `json:"result"`
	}
	if err := c.call(ctx, "stopRecord", typedRecordingParams(vhost, appName, stream, recorderType), &response); err != nil {
		return err
	}
	if response.Code == -500 {
		return nil
	}
	if response.Code != 0 || !response.Result {
		return fmt.Errorf("stopRecord type=%d code=%d msg=%s", recorderType, response.Code, response.Msg)
	}
	return nil
}

// IsRecordingWithType reports the actual ZLM recorder state for either HLS or
// MP4. Missing streams are reported as false without an error.
func (c *Client) IsRecordingWithType(ctx context.Context, vhost, appName, stream string, recorderType RecorderType) (bool, error) {
	if err := validateTypedRecordingRequest(vhost, appName, stream, recorderType); err != nil {
		return false, err
	}
	var response struct {
		baseResp
		Status bool `json:"status"`
	}
	if err := c.call(ctx, "isRecording", typedRecordingParams(vhost, appName, stream, recorderType), &response); err != nil {
		return false, err
	}
	if response.Code == -500 {
		return false, nil
	}
	if response.Code != 0 {
		return false, fmt.Errorf("isRecording type=%d code=%d msg=%s", recorderType, response.Code, response.Msg)
	}
	return response.Status, nil
}

// StartRecorder/StopRecorder are descriptive aliases for the typed methods.
func (c *Client) StartRecorder(ctx context.Context, vhost, appName, stream string, recorderType RecorderType, maxSecond int) error {
	return c.StartRecordWithType(ctx, vhost, appName, stream, recorderType, maxSecond)
}

func (c *Client) StopRecorder(ctx context.Context, vhost, appName, stream string, recorderType RecorderType) error {
	return c.StopRecordWithType(ctx, vhost, appName, stream, recorderType)
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

// DeleteMP4RecordFile deletes one exact MP4 file. The name parameter is
// mandatory because omitting it makes ZLM delete the whole recording period.
func (c *Client) DeleteMP4RecordFile(ctx context.Context, vhost, appName, stream, period, name string) error {
	if strings.TrimSpace(vhost) == "" || strings.TrimSpace(appName) == "" || strings.TrimSpace(stream) == "" ||
		strings.TrimSpace(name) == "" || path.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return ErrRecordingPathInvalid
	}
	if parsed, err := time.Parse("2006-01-02", period); err != nil || parsed.Format("2006-01-02") != period {
		return ErrRecordingPathInvalid
	}
	var response struct {
		baseResp
		Result bool `json:"result"`
	}
	if err := c.call(ctx, "deleteRecordDirectory", map[string]string{
		"vhost": vhost, "app": appName, "stream": stream, "period": period, "name": name,
	}, &response); err != nil {
		return classifyRecordingControlError(err)
	}
	if response.Code != 0 {
		return classifyRecordingCode(response.Code)
	}
	if !response.Result {
		return ErrRecordingDeleteFailed
	}
	return nil
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
