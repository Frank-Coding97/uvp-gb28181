package upgrade

import (
	"strings"
	"sync"
	"testing"
)

func TestSequenceGateRejectsReplayAndConflictingPayload(t *testing.T) {
	gate := NewSequenceGate()
	if err := gate.Accept("stable", 7, []byte("release-7")); err != nil {
		t.Fatal(err)
	}
	if err := gate.Accept("stable", 7, []byte("release-7")); err != nil {
		t.Fatalf("same signed payload should be idempotent: %v", err)
	}
	if err := gate.Accept("stable", 6, []byte("release-6")); err == nil {
		t.Fatal("older sequence was accepted")
	}
	if err := gate.Accept("stable", 7, []byte("different-release-7")); err == nil {
		t.Fatal("conflicting same sequence was accepted")
	}
	if err := gate.Accept("stable", 8, []byte("release-8")); err != nil {
		t.Fatalf("newer sequence was rejected: %v", err)
	}
}

func TestSequenceGateSeparatesChannelsAndIsConcurrent(t *testing.T) {
	gate := NewSequenceGate()
	if err := gate.Accept("beta", 1, []byte("beta-1")); err != nil {
		t.Fatal(err)
	}
	if err := gate.Accept("stable", 1, []byte("stable-1")); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- gate.Accept("stable", 2, []byte(strings.Repeat("x", 8)))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent idempotent accept failed: %v", err)
		}
	}
}
