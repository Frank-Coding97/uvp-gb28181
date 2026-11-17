package videoshare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

const maxHLSManifestBytes = 1 << 20

var hlsURIAttribute = regexp.MustCompile(`URI="([^"]+)"`)

func (s *Service) RelayHLS(requestCtx context.Context, w http.ResponseWriter, token, resourceID string) {
	access, err := s.LookupViewer(requestCtx, token)
	if err != nil {
		writeRelayError(w, err)
		return
	}
	finishRequest, err := s.beginViewerRequest(token)
	if err != nil {
		writeRelayError(w, err)
		return
	}
	defer finishRequest(false)
	ctx, cancel := context.WithCancel(requestCtx)
	stopSessionCancel := context.AfterFunc(access.Context, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()
	monitorDone := make(chan struct{})
	go s.watchViewer(ctx, cancel, token, monitorDone)
	defer func() {
		cancel()
		<-monitorDone
	}()
	resourcePath := ""
	if resourceID != "" {
		var ok bool
		resourcePath, ok = s.resolveHLSResource(token, resourceID)
		if !ok {
			http.Error(w, "video share resource unavailable", http.StatusGone)
			return
		}
	}
	source, release, err := s.refreshViewerSource(ctx, access)
	if release != nil {
		defer release()
	}
	if err != nil {
		writeRelayError(w, err)
		return
	}
	root, err := url.Parse(source.HLSURL)
	if err != nil || !validMediaURL(source.HLSURL) || !strings.HasSuffix(root.Path, ".m3u8") {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	upstreamURL := *root
	if resourceID != "" {
		if !strings.HasPrefix(resourcePath, hlsResourcePrefix(root)) {
			http.Error(w, "video share resource unavailable", http.StatusGone)
			return
		}
		upstreamURL.Path = resourcePath
		upstreamURL.RawPath = ""
		upstreamURL.RawQuery = root.RawQuery
		upstreamURL.Fragment = ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL.String(), nil)
	if err != nil {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	request.Header.Set("Accept", "application/vnd.apple.mpegurl, video/mp2t, video/mp4, application/octet-stream")
	response, err := flvHTTPClient.Do(request)
	if err != nil {
		writeRelayError(w, err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	if resourceID == "" || strings.HasSuffix(upstreamURL.Path, ".m3u8") || strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "mpegurl") {
		body, err := io.ReadAll(io.LimitReader(response.Body, maxHLSManifestBytes+1))
		if err != nil || len(body) > maxHLSManifestBytes {
			http.Error(w, "video share playlist unavailable", http.StatusBadGateway)
			return
		}
		manifest, err := s.rewriteHLSManifest(token, &upstreamURL, body)
		if err != nil {
			http.Error(w, "video share playlist unavailable", http.StatusBadGateway)
			return
		}
		if ctx.Err() != nil {
			writeRelayError(w, ctx.Err())
			return
		}
		setHLSHeaders(w, "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(manifest)
		return
	}
	contentType := response.Header.Get("Content-Type")
	if !supportedHLSContentType(contentType) {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	setHLSHeaders(w, contentType)
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	var deadlineMu sync.Mutex
	stopAbort := context.AfterFunc(ctx, func() {
		deadlineMu.Lock()
		_ = controller.SetWriteDeadline(time.Now())
		deadlineMu.Unlock()
		_ = response.Body.Close()
	})
	defer stopAbort()
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := response.Body.Read(buffer)
		if ctx.Err() != nil {
			return
		}
		if count > 0 {
			deadlineMu.Lock()
			writeErr := controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if errors.Is(writeErr, http.ErrNotSupported) {
				writeErr = nil
			}
			deadlineMu.Unlock()
			if writeErr != nil {
				return
			}
			written, err := w.Write(buffer[:count])
			if err != nil || written != count {
				return
			}
			if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return
			}
		}
		if readErr != nil {
			// Normal EOF must leave time for the server to write the final HTTP chunk.
			// The deferred stopAbort runs before the deferred cancellation.
			if !errors.Is(readErr, io.EOF) {
				cancel()
			}
			return
		}
	}
}

func (s *Service) refreshViewerSource(ctx context.Context, access ViewerAccess) (PlaybackSource, func(), error) {
	s.viewerMu.Lock()
	preparer := s.prepare
	s.viewerMu.Unlock()
	if preparer == nil || access.Share == nil {
		return PlaybackSource{}, nil, ErrViewerUnavailable
	}
	source, err := preparer.Prepare(ctx, access.Share)
	if err != nil {
		return PlaybackSource{}, nil, err
	}
	release := source.Release
	if source.StreamID != access.Source.StreamID || source.Generation != access.Source.Generation {
		if release != nil {
			release()
		}
		return PlaybackSource{}, nil, ErrExpired
	}
	return source, release, nil
}

func (s *Service) rewriteHLSManifest(token string, base *url.URL, content []byte) ([]byte, error) {
	text := string(content)
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "#EXTM3U") {
		return nil, fmt.Errorf("invalid HLS manifest")
	}
	lines := strings.Split(text, "\n")
	var result strings.Builder
	result.Grow(len(text) + 128)
	for _, rawLine := range lines {
		line := strings.TrimSuffix(rawLine, "\r")
		if strings.HasPrefix(line, "#") {
			var rewriteErr error
			line = hlsURIAttribute.ReplaceAllStringFunc(line, func(match string) string {
				parts := hlsURIAttribute.FindStringSubmatch(match)
				if len(parts) != 2 {
					rewriteErr = errors.New("invalid HLS URI attribute")
					return match
				}
				path, err := s.rewriteHLSResource(token, base, parts[1])
				if err != nil {
					rewriteErr = err
					return match
				}
				return `URI="` + path + `"`
			})
			if rewriteErr != nil {
				return nil, rewriteErr
			}
		} else if strings.TrimSpace(line) != "" {
			resourceURL, err := s.resolveHLSReference(base, strings.TrimSpace(line))
			if err != nil {
				return nil, err
			}
			resourceID, err := s.mapHLSResource(token, resourceURL.Path)
			if err != nil {
				return nil, err
			}
			line = hlsViewerResourcePath(token, resourceID)
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return []byte(result.String()), nil
}

func (s *Service) rewriteHLSResource(token string, base *url.URL, reference string) (string, error) {
	resolved, err := s.resolveHLSReference(base, reference)
	if err != nil {
		return "", err
	}
	resourceID, err := s.mapHLSResource(token, resolved.Path)
	if err != nil {
		return "", err
	}
	return hlsViewerResourcePath(token, resourceID), nil
}

func (s *Service) resolveHLSReference(base *url.URL, reference string) (*url.URL, error) {
	ref, err := url.Parse(reference)
	if err != nil || ref.Scheme != "" || ref.Host != "" || ref.User != nil || ref.Opaque != "" || ref.RawQuery != "" || ref.Fragment != "" || strings.Contains(reference, "\\") {
		return nil, errors.New("unsupported HLS resource reference")
	}
	for _, segment := range strings.Split(ref.Path, "/") {
		if segment == ".." || segment == "." {
			return nil, errors.New("HLS path traversal rejected")
		}
	}
	resolved := base.ResolveReference(ref)
	if !strings.EqualFold(resolved.Scheme, base.Scheme) || !strings.EqualFold(resolved.Host, base.Host) || !strings.HasPrefix(path.Clean(resolved.Path)+"/", hlsResourcePrefix(base)) {
		return nil, errors.New("HLS resource outside stream rejected")
	}
	resolved.RawQuery = ""
	resolved.Fragment = ""
	return resolved, nil
}

func hlsViewerResourcePath(token, resourceID string) string {
	return "/api/public/video-share/viewers/" + token + "/hls/" + resourceID
}

func setHLSHeaders(w http.ResponseWriter, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func supportedHLSContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return contentType == "video/mp2t" || contentType == "video/mp4" || contentType == "application/octet-stream" || contentType == "audio/mp4"
}
