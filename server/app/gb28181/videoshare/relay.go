package videoshare

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var flvHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *Service) RelayHTTPFLV(requestCtx context.Context, w http.ResponseWriter, token string) {
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
	established := false
	defer func() { finishRequest(established) }()
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
	source, release, err := s.refreshViewerSource(ctx, access)
	if release != nil {
		defer release()
	}
	if err != nil {
		writeRelayError(w, err)
		return
	}
	if source.HTTPFLVURL == "" || !validMediaURL(source.HTTPFLVURL) {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.HTTPFLVURL, nil)
	if err != nil {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	request.Header.Set("Accept", "video/x-flv, application/octet-stream")
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
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if contentType != "video/x-flv" && contentType != "application/octet-stream" {
		http.Error(w, "video share media unavailable", http.StatusBadGateway)
		return
	}
	if !s.confirmViewerMedia(token) {
		_ = s.CloseViewer(token)
		writeRelayError(w, ErrExpired)
		return
	}
	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	established = true
	controller := http.NewResponseController(w)
	_ = controller.Flush()
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
			cancel()
			return
		}
	}
}

func (s *Service) watchViewer(ctx context.Context, cancel context.CancelFunc, token string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
			_, err := s.LookupViewer(checkCtx, token)
			checkCancel()
			if err != nil {
				// A completed or canceled media request also cancels its in-flight
				// authorization query. It is not a revocation of the viewer shared
				// by subsequent HLS requests; the idle lease still bounds cleanup.
				if ctx.Err() != nil {
					return
				}
				_ = s.CloseViewer(token)
				cancel()
				return
			}
		}
	}
}

func validMediaURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}

func writeRelayError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, ErrExpired) || errors.Is(err, ErrInvalidCredential) || errors.Is(err, ErrNotFound) {
		status = http.StatusGone
	} else if errors.Is(err, context.Canceled) {
		status = http.StatusGone
	} else if errors.Is(err, ErrViewerUnavailable) {
		status = http.StatusServiceUnavailable
	}
	http.Error(w, http.StatusText(status), status)
}
