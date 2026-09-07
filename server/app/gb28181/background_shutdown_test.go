package gb28181

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackgroundDrainWaitsForFinalWork(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	done := runBackground(func() { close(entered); <-release })
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitBackground(ctx, done); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("final write not awaited: %v", err)
	}
	close(release)
	if err := waitBackground(context.Background(), nil, done); err != nil {
		t.Fatal(err)
	}
}
