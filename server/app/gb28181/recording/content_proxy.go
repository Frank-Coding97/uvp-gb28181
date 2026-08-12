package recording

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

const contentProxyBufferSize = 32 * 1024

const downloadWriteDeadline = 90 * time.Second

var (
	ErrContentRangeInvalid = errors.New("invalid recording content range")
	ErrContentRedirect     = errors.New("recording content redirect refused")
	ErrContentUpstream     = errors.New("recording content upstream failure")
	ErrContentDeadline     = errors.New("recording content writer deadline unavailable")
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

type ContentProxy struct{}

func NewContentProxy() *ContentProxy { return &ContentProxy{} }

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
	response, err := downloader.DownloadFile(ctx, request.FilePath, "")
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
	if err := validateContentStatus(response.StatusCode, false); err != nil {
		return err
	}
	if err := validateDownloadLength(response.Header.Get("Content-Length"), request.FileSize); err != nil {
		return err
	}
	if err := setDownloadDeadline(writer); err != nil {
		return err
	}
	copyContentHeaders(writer.Header(), response.Header, response.StatusCode)
	writer.Header().Set("Content-Disposition", contentDisposition(CapabilityModeDownload, request.FileName))
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)

	buffer := make([]byte, contentProxyBufferSize)
	var copied uint64
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			written, writeErr := writer.Write(buffer[:count])
			if written > 0 {
				copied += uint64(written)
				if onWrite != nil {
					onWrite(uint64(written))
				}
				if deadlineErr := setDownloadDeadline(writer); deadlineErr != nil {
					return deadlineErr
				}
			}
			if writeErr != nil || written != count {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return ErrContentUpstream
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrContentUpstream
		}
	}
	if expected := expectedDownloadLength(response.Header.Get("Content-Length"), request.FileSize); expected != nil && copied != *expected {
		return ErrContentUpstream
	}
	return nil
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

func setDownloadDeadline(writer http.ResponseWriter) error {
	if err := http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(downloadWriteDeadline)); err != nil {
		return ErrContentDeadline
	}
	return nil
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
		zlm.ErrRecordingNotFound, zlm.ErrRecordingNodeUnavailable, zlm.ErrRecordingAccessUnavailable,
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
