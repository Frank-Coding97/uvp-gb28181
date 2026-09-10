package ginhelper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

// ShutdownStep stops admission and waits for that component's accepted work.
// Steps run in dependency order, sharing the existing shutdown deadline.
type ShutdownStep struct {
	Component string
	Stop      func(context.Context) error
}

func Shutdown(ctx context.Context, root *zap.Logger, steps ...ShutdownStep) error {
	var errs []error
	for _, step := range steps {
		if err := step.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step.Component, err))
			root.Named("lifecycle").Error("Component shutdown incomplete", zap.String("event", "lifecycle.shutdown_incomplete"), zap.String("shutdown_component", step.Component), zap.Bool("shutdown_timeout", errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)), logging.Error(err))
			var unfinished interface{ UnfinishedComponents() []string }
			if errors.As(err, &unfinished) {
				for _, component := range unfinished.UnfinishedComponents() {
					root.Named("lifecycle").Error("Component work remains active", zap.String("event", "lifecycle.shutdown_incomplete"), zap.String("shutdown_component", component), zap.Bool("shutdown_timeout", true))
				}
			}
		}
	}
	return errors.Join(errs...)
}

func serveUntilCanceled(ctx context.Context, server *http.Server, root *zap.Logger, stop func(context.Context) error) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		root.Named("http").Error("HTTP listen failed", zap.String("event", "http.listen_failed"), logging.Error(err))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return errors.Join(err, stop(shutdownCtx))
	}
	root.Named("http").Info("HTTP listener ready", zap.String("event", "http.ready"), zap.String("address", listener.Addr().String()))
	serving := make(chan error, 1)
	go func() { serving <- server.Serve(listener) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serving:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = Shutdown(shutdownCtx, root,
		ShutdownStep{Component: "http", Stop: func(ctx context.Context) error {
			err := server.Shutdown(ctx)
			if err != nil {
				_ = server.Close()
			}
			return err
		}},
		ShutdownStep{Component: "application", Stop: stop},
	)
	return errors.Join(serveErr, err)
}
