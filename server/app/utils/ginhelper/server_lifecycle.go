package ginhelper

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

// Keep the same HTTP server until ordinary handlers have actually returned.
// Shutdown does not own hijacked connections or handler-created background
// work; those must be drained by their own lifecycle owners after this gate.
func serveHTTPUntilShutdown(server *http.Server, quit <-chan os.Signal, report func(error), timeout, retryDelay time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe() }()
	var serveErr error
	joined := false
	select {
	case serveErr = <-served:
		joined = true
	case <-quit:
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err := server.Shutdown(ctx)
		cancel()
		if err == nil {
			break
		}
		report(err)
		time.Sleep(retryDelay)
	}
	if !joined {
		serveErr = <-served
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
