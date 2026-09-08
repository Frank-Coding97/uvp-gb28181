package main

import (
	"bytes"
	"testing"
)

func TestBackupCommandRejectsInvalidArgumentsBeforeOpeningInstallation(t *testing.T) {
	for _, args := range [][]string{nil, {"--output"}, {"--unknown"}, {"--output", "target", "unexpected"}} {
		var out, diagnostic bytes.Buffer
		if code := runBackupCommand(args, "missing-installation", &out, &diagnostic); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatalf("args=%v: expected failure without success output", args)
		}
	}
}
