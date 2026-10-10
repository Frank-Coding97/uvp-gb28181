package main

import (
	"errors"
	"testing"
	"time"
)

// Run with explicit source files so importing the application's bootstrap
// cannot initialize/migrate a configured business database in this unit test.
func TestWaitForSIPShutdownDoesNotExitAfterFailure(t *testing.T) {
	want := errors.New("fixture pending shutdown")
	attempts, reported := 0, 0
	waitForSIPShutdown(func() error {
		attempts++
		if attempts < 3 {
			return want
		}
		return nil
	}, func(err error) {
		if !errors.Is(err, want) {
			t.Errorf("unexpected failure: %v", err)
		}
		reported++
	}, time.Millisecond)
	if attempts != 3 || reported != 2 {
		t.Fatalf("exit bypassed retained shutdown: attempts=%d reports=%d", attempts, reported)
	}
}

func TestWaitForSIPShutdownWaitsForActualAttempt(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		waitForSIPShutdown(func() error { close(entered); <-release; return nil }, func(error) { t.Error("unexpected failure") }, time.Millisecond)
	}()
	<-entered
	select {
	case <-done:
		t.Error("attempt has not returned")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after actual drain")
	}
}
