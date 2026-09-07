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

func TestMediaConfigProducerCancellationWaitsBeforeMediaStop(t *testing.T) {
	oldCancel, oldStartupCancel := heartbeatCancel, startupProbeCancel
	oldHeartbeat, oldThread, oldNode, oldStartup := heartbeatDone, threadPollerDone, nodeConvergenceDone, startupProbeDone
	oldCore := zlmManagementCore
	defer func() {
		heartbeatCancel, startupProbeCancel = oldCancel, oldStartupCancel
		heartbeatDone, threadPollerDone, nodeConvergenceDone, startupProbeDone = oldHeartbeat, oldThread, oldNode, oldStartup
		zlmManagementCore = oldCore
	}()
	producer, cancelProducer := context.WithCancel(context.Background())
	defer cancelProducer()
	heartbeatCancel = cancelProducer
	startupProbeCancel = nil
	heartbeatDone = nil
	threadPollerDone = nil
	startupProbeDone = nil
	zlmManagementCore = nil
	finished := make(chan struct{})
	nodeConvergenceDone = finished
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := quiesceZLMBackground(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("producer drain=%v", err)
	}
	if producer.Err() == nil {
		t.Fatal("media configuration producer was not canceled")
	}
	close(finished)
	if err := quiesceZLMBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
}
