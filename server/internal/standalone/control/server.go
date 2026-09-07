package control

import (
	"context"
	"errors"
	"net"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone/controlpipe"
)

// Call uses a fresh, mutually authenticated connection for one command.
func Call(ctx context.Context, name, key string, command Command) (Reply, error) {
	conn, err := controlpipe.Dial(ctx, name, key)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return Exchange(ctx, conn, command)
}

// Serve consumes a controlpipe listener, never a public network listener.
// Commands are serialized so two stop callers cannot interleave shutdown phases.
// A failed handshake or command does not remove the local control endpoint.
func Serve(ctx context.Context, listener net.Listener, handler func(context.Context, Command) (Reply, error)) error {
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var timed net.Error
			if errors.Is(err, controlpipe.ErrAuthentication) || errors.Is(err, controlpipe.ErrMalformedFrame) || errors.Is(err, controlpipe.ErrFrameTooLarge) || (errors.As(err, &timed) && timed.Timeout()) {
				continue
			}
			return err
		}
		commandCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		finalized := false
		handleErr := Handle(commandCtx, conn, func(ctx context.Context, command Command) (Reply, error) {
			reply, err := handler(ctx, command)
			finalized = reply == Finalized && err == nil
			return reply, err
		})
		cancel()
		_ = conn.Close()
		if finalized {
			return handleErr
		}
	}
}
