package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDiskFullVolumeRejectsUnsafeTargets(t *testing.T) {
	tests := []struct {
		name  string
		input diskVolumeInfo
	}{
		{name: "empty root", input: diskVolumeInfo{Label: "UVP_P0_TEST", TotalBytes: 64 * 1024 * 1024}},
		{name: "missing label", input: diskVolumeInfo{Root: `V:\`, TotalBytes: 64 * 1024 * 1024}},
		{name: "wrong label", input: diskVolumeInfo{Root: `C:\`, Label: "SYSTEM", TotalBytes: 64 * 1024 * 1024}},
		{name: "large volume", input: diskVolumeInfo{Root: `V:\`, Label: "UVP_P0_TEST", TotalBytes: 129 * 1024 * 1024}},
		{name: "zero size", input: diskVolumeInfo{Root: `V:\`, Label: "UVP_P0_TEST"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDiskFullVolume(test.input); err == nil {
				t.Fatalf("validateDiskFullVolume(%#v) unexpectedly passed", test.input)
			}
		})
	}
}

func TestValidateDiskFullVolumeAcceptsSmallMarkedVolume(t *testing.T) {
	input := diskVolumeInfo{Root: `V:\`, Label: "UVP_P0_TEST", TotalBytes: 64 * 1024 * 1024}
	if err := validateDiskFullVolume(input); err != nil {
		t.Fatalf("validateDiskFullVolume(%#v): %v", input, err)
	}
}

func TestFillDiskStopsAtExplicitCapWithoutTreatingCapAsENOSPC(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "filler")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := fillDiskUntilFull(context.Background(), directory, 2*diskFillChunkBytes)
	if err != nil {
		t.Fatalf("fillDiskUntilFull: %v", err)
	}
	if result.ReachedNoSpace || result.BytesWritten != 2*uint64(diskFillChunkBytes) {
		t.Fatalf("fill result = %#v, want exact cap without ENOSPC", result)
	}
}

func TestIsNoSpaceErrorRecognizesNativeAndUnixMessages(t *testing.T) {
	for _, message := range []string{
		"no space left on device",
		"There is not enough space on the disk.",
		"disk full",
	} {
		if !isNoSpaceError(errors.New(message)) {
			t.Fatalf("isNoSpaceError(%q) = false", message)
		}
	}
	if isNoSpaceError(errors.New("permission denied")) {
		t.Fatal("permission error was classified as ENOSPC")
	}
}
