//go:build !windows

package winprocess

import (
	"errors"
	"testing"
)

func TestUnsupportedOutsideWindows(t *testing.T) {
	job, err := NewJob()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("NewJob error = %v, want ErrUnsupported", err)
	}
	if job != nil {
		t.Fatal("NewJob returned a job on a non-Windows platform")
	}
	if _, err := (*Job)(nil).Start(StartSpec{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Start error = %v, want ErrUnsupported", err)
	}
	if err := (*Job)(nil).Close(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Job.Close error = %v, want ErrUnsupported", err)
	}
	if got := (*Process)(nil).PID(); got != 0 {
		t.Fatalf("Process.PID = %d, want 0", got)
	}
	if _, err := (*Process)(nil).Wait(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Process.Wait error = %v, want ErrUnsupported", err)
	}
	if err := (*Process)(nil).Close(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Process.Close error = %v, want ErrUnsupported", err)
	}
}
