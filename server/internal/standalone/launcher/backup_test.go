package launcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestBackupWaitsForFinalizedOwnerLockRelease(t *testing.T) {
	attempts, stops := 0, 0
	err := backupAfterStop(context.Background(), time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return standalone.ErrInstanceRunning
		}
		if stops != 1 {
			t.Fatal("copied before confirmed stop")
		}
		return nil
	}, func() error { stops++; return nil })
	if err != nil || attempts != 3 || stops != 1 {
		t.Fatalf("attempts=%d stops=%d err=%v", attempts, stops, err)
	}
}

func TestBackupDoesNotCopyAfterFailedStop(t *testing.T) {
	want := errors.New("stop not finalized")
	attempts := 0
	err := backupAfterStop(context.Background(), time.Millisecond, func() error { attempts++; return standalone.ErrInstanceRunning }, func() error { return want })
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestBackupCompetingOwnerRemainsBusy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	stops := 0
	err := backupAfterStop(ctx, time.Millisecond, func() error { return standalone.ErrInstanceRunning }, func() error { stops++; return nil })
	if !errors.Is(err, standalone.ErrInstanceRunning) || stops != 1 {
		t.Fatalf("stops=%d err=%v", stops, err)
	}
}

func TestBackupDoesNotRetryIntegrityFailure(t *testing.T) {
	want := errors.New("integrity failed")
	attempts := 0
	err := backupAfterStop(context.Background(), time.Millisecond, func() error { attempts++; return want }, func() error { t.Fatal("unexpected stop"); return nil })
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
