package main

import (
	"bytes"
	"testing"
)

func TestUpgradeCommandRejectsIncompleteArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "next"}, {"--backup", "target"}, {"--version"}, {"--unknown"}, {"--version", "next", "--backup", "target", "unexpected"}} {
		var out, diagnostic bytes.Buffer
		if code := runUpgradeCommand(args, "missing-installation", &out, &diagnostic); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("args=%v: expected failure without success output", args)
		}
	}
}

func TestUpgradeCommandDoesNotReportUnqualifiedBuildSuccess(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := runUpgradeCommand([]string{"--version", "next", "--backup", t.TempDir()}, "missing-installation", &out, &diagnostic)
	if code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
		t.Fatal("failed upgrade must not report success")
	}
}
