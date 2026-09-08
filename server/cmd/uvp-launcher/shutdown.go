package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func shutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	// Windows maps console close/logoff/shutdown events to SIGTERM, but still
	// enforces its own exit deadline; interrupted cleanup retains the run marker.
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}
