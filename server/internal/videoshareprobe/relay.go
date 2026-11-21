// Package videoshareprobe contains the T01 transport feasibility fixture only.
// It is not wired into the application and provides no sharing authorization.
package videoshareprobe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// relay accepts an upstream chosen by the test fixture, never a public request.
// Authorization, HLS rewriting, source leases and periodic rechecks are T11–T14.
func relay(ctx context.Context, w http.ResponseWriter, upstream string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("probe upstream status rejected")
	}
	controller := http.NewResponseController(w)
	if err = controller.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	// Closing the body interrupts an upstream Read; advancing the network write
	// deadline interrupts a blocked Write/Flush even when the receiver never reads.
	var deadlineMu sync.Mutex
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		deadlineMu.Lock()
		_ = controller.SetWriteDeadline(time.Now())
		deadlineMu.Unlock()
		_ = resp.Body.Close()
		close(stopped)
	})
	defer func() {
		if !stop() {
			<-stopped
		}
	}()
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if err = controller.Flush(); err != nil {
		return err
	}
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buffer)
		if err = ctx.Err(); err != nil {
			return err
		}
		if n > 0 {
			deadlineMu.Lock()
			err = ctx.Err()
			if err == nil {
				err = controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
			}
			deadlineMu.Unlock()
			if err != nil {
				return err
			}
			written, writeErr := w.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			if err = controller.Flush(); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}
