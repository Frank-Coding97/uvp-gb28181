package recording

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

const contentProxyBufferSize = 32 * 1024

const (
	defaultDownloadWriteDeadline     = 90 * time.Second
	defaultDownloadInactivityTimeout = 90 * time.Second
)

var (
	ErrContentRangeInvalid = errors.New("invalid recording content range")
	ErrContentRedirect     = errors.New("recording content redirect refused")
	ErrContentUpstream     = errors.New("recording content upstream failure")
	ErrContentDeadline     = errors.New("recording content writer deadline unavailable")
	ErrContentTimeout      = errors.New("recording content transfer timeout")
)

type ContentDownloader interface {
	DownloadFile(context.Context, string, string) (*zlm.DownloadResponse, error)
}

type ContentRequest struct {
	FilePath string
	FileName string
	FileSize *uint64
	Mode     string
	Range    string
}

type ContentProxy struct {
	writeDeadline     time.Duration
	inactivityTimeout time.Duration
}

func NewContentProxy() *ContentProxy {
	return &ContentProxy{
		writeDeadline:     defaultDownloadWriteDeadline,
		inactivityTimeout: defaultDownloadInactivityTimeout,
	}
}

func (p *ContentProxy) Stream(ctx context.Context, writer http.ResponseWriter, downloader ContentDownloader, request ContentRequest) error {
	if writer == nil || downloader == nil || request.FilePath == "" || !validCapabilityMode(request.Mode) {
		return ErrContentUpstream
	}
	if err := validateSingleRange(request.Range, request.FileSize); err != nil {
		return err
	}

	response, err := downloader.DownloadFile(ctx, request.FilePath, request.Range)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return classifyContentDownloadError(err)
	}
	if response == nil || response.Body == nil {
		return ErrContentUpstream
	}
	defer response.Body.Close()

	if err := validateContentStatus(response.StatusCode, request.Range != ""); err != nil {
		return err
	}
	copyContentHeaders(writer.Header(), response.Header, response.StatusCode)
	writer.Header().Set("Content-Disposition", contentDisposition(request.Mode, request.FileName))
	writer.WriteHeader(response.StatusCode)

	_, err = io.CopyBuffer(writer, response.Body, make([]byte, contentProxyBufferSize))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrContentUpstream
	}
	return nil
}

// StreamDownload forwards exactly one complete response and deliberately never
// passes Range upstream. A later retry therefore starts from byte zero.
func (p *ContentProxy) StreamDownload(ctx context.Context, writer http.ResponseWriter, downloader ContentDownloader, request ContentRequest, onWrite func(uint64)) error {
	if writer == nil || downloader == nil || request.FilePath == "" || request.Mode != CapabilityModeDownload {
		return ErrContentUpstream
	}
	if err := p.setDownloadDeadline(writer); err != nil {
		return err
	}
	transferCtx, transferCancel := context.WithCancel(ctx)
	defer transferCancel()

	response, err := downloader.DownloadFile(transferCtx, request.FilePath, "")
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return classifyContentDownloadError(err)
	}
	if response == nil || response.Body == nil {
		return ErrContentUpstream
	}
	if err := validateContentStatus(response.StatusCode, false); err != nil {
		_ = response.Body.Close()
		return err
	}
	if err := validateDownloadLength(response.Header.Get("Content-Length"), request.FileSize); err != nil {
		_ = response.Body.Close()
		return err
	}
	return p.copyDownload(ctx, transferCancel, writer, response, request, onWrite)
}

func (p *ContentProxy) copyDownload(parentCtx context.Context, transferCancel context.CancelFunc, writer http.ResponseWriter, response *zlm.DownloadResponse, request ContentRequest, onWrite func(uint64)) error {
	defer transferCancel()
	closeBody := sync.OnceFunc(func() { _ = response.Body.Close() })
	defer closeBody()

	progress := make(chan struct{}, 1)
	done := make(chan struct{})
	defer close(done)
	var timedOut bool
	lastProgress := time.Now()
	var watchdogMu sync.Mutex
	stopTransfer := sync.OnceFunc(func() {
		transferCancel()
		closeBody()
	})
	markProgress := func() {
		watchdogMu.Lock()
		lastProgress = time.Now()
		watchdogMu.Unlock()
		select {
		case progress <- struct{}{}:
		default:
		}
	}
	go func() {
		timeout := p.downloadInactivityTimeout()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-parentCtx.Done():
				stopTransfer()
				return
			case <-progress:
				resetDownloadTimer(timer, timeout)
			case <-timer.C:
				watchdogMu.Lock()
				idle := time.Since(lastProgress)
				if idle < timeout {
					watchdogMu.Unlock()
					timer.Reset(timeout - idle)
					continue
				}
				timedOut = true
				watchdogMu.Unlock()
				stopTransfer()
				return
			}
		}
	}()
	timedOutTransfer := func() bool {
		watchdogMu.Lock()
		defer watchdogMu.Unlock()
		return timedOut
	}

	var copied uint64
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		copyContentHeaders(writer.Header(), response.Header, response.StatusCode)
		writer.Header().Set("Content-Disposition", contentDisposition(CapabilityModeDownload, request.FileName))
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Accel-Buffering", "no")
		writer.WriteHeader(http.StatusOK)
		headersWritten = true
	}

	buffer := make([]byte, contentProxyBufferSize)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			writeHeaders()
			written, writeErr := writer.Write(buffer[:count])
			if written > 0 {
				copied += uint64(written)
				markProgress()
				if onWrite != nil {
					onWrite(uint64(written))
				}
				if deadlineErr := p.setDownloadDeadline(writer); deadlineErr != nil {
					stopTransfer()
					return deadlineErr
				}
			}
			if writeErr != nil || written != count {
				stopTransfer()
				if parentCtx.Err() != nil {
					return parentCtx.Err()
				}
				if timedOutTransfer() || networkTimeout(writeErr) {
					return ErrContentTimeout
				}
				return ErrContentUpstream
			}
		}
		if readErr == io.EOF {
			if !headersWritten {
				writeHeaders()
			}
			if expected := expectedDownloadLength(response.Header.Get("Content-Length"), request.FileSize); expected != nil && copied != *expected {
				return ErrContentUpstream
			}
			return nil
		}
		if readErr != nil {
			if parentCtx.Err() != nil {
				return parentCtx.Err()
			}
			if timedOutTransfer() {
				return ErrContentTimeout
			}
			return ErrContentUpstream
		}
	}
}

func networkTimeout(err error) bool {
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func resetDownloadTimer(timer *time.Timer, timeout time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(timeout)
}

func validateDownloadRange(value string) error {
	if value == "" || value == "bytes=0-" {
		return nil
	}
	return ErrContentRangeInvalid
}

func validateDownloadLength(contentLength string, fileSize *uint64) error {
	if contentLength == "" {
		return nil
	}
	length, err := strconv.ParseUint(contentLength, 10, 64)
	if err != nil || (fileSize != nil && length != *fileSize) {
		return ErrContentUpstream
	}
	return nil
}

func expectedDownloadLength(contentLength string, fileSize *uint64) *uint64 {
	if contentLength != "" {
		if length, err := strconv.ParseUint(contentLength, 10, 64); err == nil {
			return &length
		}
	}
	return fileSize
}

func (p *ContentProxy) setDownloadDeadline(writer http.ResponseWriter) error {
	if err := http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(p.downloadWriteDeadline())); err != nil {
		return ErrContentDeadline
	}
	return nil
}

func (p *ContentProxy) downloadWriteDeadline() time.Duration {
	if p == nil || p.writeDeadline <= 0 {
		return defaultDownloadWriteDeadline
	}
	return p.writeDeadline
}

func (p *ContentProxy) downloadInactivityTimeout() time.Duration {
	if p == nil || p.inactivityTimeout <= 0 {
		return defaultDownloadInactivityTimeout
	}
	return p.inactivityTimeout
}

func validateSingleRange(value string, size *uint64) error {
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return ErrContentRangeInvalid
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 || parts[0] == "" {
		return ErrContentRangeInvalid
	}
	start, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return ErrContentRangeInvalid
	}
	if size != nil && start >= *size {
		return ErrContentRangeInvalid
	}
	if parts[1] == "" {
		return nil
	}
	end, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || end < start || (size != nil && end >= *size) {
		return ErrContentRangeInvalid
	}
	return nil
}

func validateContentStatus(status int, ranged bool) error {
	if status >= http.StatusMultipleChoices && status < http.StatusBadRequest {
		return ErrContentRedirect
	}
	switch status {
	case http.StatusOK:
		if ranged {
			return ErrContentUpstream
		}
		return nil
	case http.StatusPartialContent:
		if !ranged {
			return ErrContentUpstream
		}
		return nil
	case http.StatusNotFound, http.StatusGone:
		return zlm.ErrRecordingNotFound
	case http.StatusRequestedRangeNotSatisfiable:
		return ErrContentRangeInvalid
	case http.StatusUnauthorized, http.StatusForbidden:
		return zlm.ErrRecordingAccessUnavailable
	default:
		if status >= http.StatusInternalServerError {
			return zlm.ErrRecordingNodeUnavailable
		}
		return ErrContentUpstream
	}
}

func classifyContentDownloadError(err error) error {
	for _, known := range []error{
		context.Canceled, context.DeadlineExceeded,
		zlm.ErrRecordingNotFound, zlm.ErrRecordingNodeUnavailable, zlm.ErrRecordingAccessUnavailable, zlm.ErrRecordingResponseTimeout,
	} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrContentUpstream
}

func copyContentHeaders(target, source http.Header, status int) {
	if contentType := source.Get("Content-Type"); contentType != "" {
		if _, _, err := mime.ParseMediaType(contentType); err == nil {
			target.Set("Content-Type", contentType)
		}
	}
	if length := source.Get("Content-Length"); length != "" {
		if _, err := strconv.ParseUint(length, 10, 64); err == nil {
			target.Set("Content-Length", length)
		}
	}
	if status == http.StatusPartialContent {
		if contentRange := source.Get("Content-Range"); validContentRange(contentRange) {
			target.Set("Content-Range", contentRange)
		}
	}
	if strings.EqualFold(source.Get("Accept-Ranges"), "bytes") {
		target.Set("Accept-Ranges", "bytes")
	}
	if etag := source.Get("ETag"); etag != "" {
		target.Set("ETag", etag)
	}
	if modified := source.Get("Last-Modified"); modified != "" {
		if parsed, err := http.ParseTime(modified); err == nil {
			target.Set("Last-Modified", parsed.UTC().Format(http.TimeFormat))
		}
	}
}

func validContentRange(value string) bool {
	if !strings.HasPrefix(value, "bytes ") || strings.Contains(value, ",") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return false
	}
	span := strings.Split(parts[0], "-")
	if len(span) != 2 {
		return false
	}
	start, startErr := strconv.ParseUint(span[0], 10, 64)
	end, endErr := strconv.ParseUint(span[1], 10, 64)
	if startErr != nil || endErr != nil || end < start {
		return false
	}
	if parts[1] == "*" {
		return true
	}
	total, err := strconv.ParseUint(parts[1], 10, 64)
	return err == nil && total > end
}

func contentDisposition(mode, fileName string) string {
	disposition := "inline"
	if mode == CapabilityModeDownload {
		disposition = "attachment"
	}
	name := path.Base(strings.ReplaceAll(fileName, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "recording.mp4"
	}
	return mime.FormatMediaType(disposition, map[string]string{"filename": name})
}
