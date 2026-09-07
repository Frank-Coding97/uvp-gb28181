package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidAOFExitEvidenceRequiresOwnedExitAndDiskFullLog(t *testing.T) {
	validLog := "Error writing to the AOF file: No space left on device\n" +
		"Can't recover from AOF write error when the AOF fsync policy is 'always'. Exiting..."
	tests := []struct {
		name     string
		evidence processExitEvidence
		want     bool
	}{
		{name: "valid", evidence: processExitEvidence{Exited: true, ExitCode: 1, Log: validLog}, want: true},
		{name: "socket EOF without process evidence", evidence: processExitEvidence{Log: "EOF"}, want: false},
		{name: "wrong exit code", evidence: processExitEvidence{Exited: true, ExitCode: 0, Log: validLog}, want: false},
		{name: "missing disk-full log", evidence: processExitEvidence{Exited: true, ExitCode: 1, Log: "AOF write error; Exiting"}, want: false},
		{name: "missing AOF log", evidence: processExitEvidence{Exited: true, ExitCode: 1, Log: "No space left on device; Exiting"}, want: false},
		{name: "policy word without exit evidence", evidence: processExitEvidence{Exited: true, ExitCode: 1, Log: "AOF write error; No space left on device; always"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validAOFExitEvidence(test.evidence); got != test.want {
				t.Fatalf("validAOFExitEvidence(%#v) = %v, want %v", test.evidence, got, test.want)
			}
		})
	}
}

func TestProcessExitEvidenceRedactsManagedSecretAndWorkspace(t *testing.T) {
	done := make(chan struct{})
	close(done)
	instance := &redisInstance{
		password:  "probe-secret",
		workspace: `C:\probe workspace 中文`,
		done:      done,
		exitErr:   nil,
	}
	instance.output.WriteString("requirepass probe-secret\nworkspace C:\\probe workspace 中文")
	evidence := instance.processExitEvidence()
	if !evidence.Exited || evidence.ExitCode != 0 {
		t.Fatalf("process exit evidence = %#v", evidence)
	}
	if evidence.Log == "" || containsAny(evidence.Log, "probe-secret", `C:\probe workspace 中文`) {
		t.Fatalf("process log was not redacted: %q", evidence.Log)
	}
	if !containsAny(evidence.Log, "[REDACTED_SECRET]", "<probe-workspace>") {
		t.Fatalf("redacted markers missing: %q", evidence.Log)
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

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
