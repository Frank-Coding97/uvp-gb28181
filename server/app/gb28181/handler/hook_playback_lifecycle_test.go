package handler

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

type lifecyclePlaybackSink chan string

func (s lifecyclePlaybackSink) OnPlaybackStreamEnded(_ context.Context, id, _ string) error {
	s <- id
	return nil
}

func TestPlaybackNotificationConcurrentDetach(t *testing.T) {
	h := NewHookController(nil)
	sink := make(lifecyclePlaybackSink, 1000)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			h.SetPlaybackMediaSink(sink)
			h.SetPlaybackMediaSink(nil)
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 1000; i++ {
			h.notifyPlaybackEnded("stream", "media-offline")
		}
	}()
	workers.Wait()
}

func TestPlaybackNotificationRetainsSinkAcrossDetach(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	h := NewHookController(nil)
	sink := make(lifecyclePlaybackSink, 1)
	h.SetPlaybackMediaSink(sink)
	h.notifyPlaybackEnded("recording-stream", "media-offline")
	h.SetPlaybackMediaSink(nil)
	select {
	case id := <-sink:
		if id != "recording-stream" {
			t.Fatalf("unexpected stream %q", id)
		}
	case <-time.After(time.Second):
		t.Fatal("queued notification lost its captured runtime")
	}
	h.notifyPlaybackEnded("detached-stream", "media-offline")
}
