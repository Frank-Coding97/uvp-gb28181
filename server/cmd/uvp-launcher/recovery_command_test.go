package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRestoreCommandRejectsExtraArgumentsAndTrustOverride(t *testing.T) {
	for _, test := range []struct {
		args       []string
		diagnostic string
	}{
		{args: []string{"unexpected"}, diagnostic: "用法"},
		{args: []string{"--trust", "anything"}, diagnostic: "trust"},
		{args: []string{"--install-dir", "root", "extra"}, diagnostic: "用法"},
	} {
		var out, diagnostic bytes.Buffer
		if code := runRestoreCommand(test.args, "missing-installation", &out, &diagnostic); code == 0 || out.Len() != 0 || !strings.Contains(diagnostic.String(), test.diagnostic) {
			t.Fatalf("args=%v: expected argument failure containing %q, diagnostic=%q", test.args, test.diagnostic, diagnostic.String())
		}
	}
}

func TestRestoreCommandDoesNotReportSuccessWhenRestoreFails(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := runRestoreCommand(nil, "missing-installation", &out, &diagnostic); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
		t.Fatal("failed restore must not report success")
	}
}

func TestRestoreSuccessMessageProvidesTheOperationBoundConfirmationCommand(t *testing.T) {
	message := restoreSuccessMessage("operation-123")
	if want := "恢复已完成，等待本机确认：operation-123\n请运行：UVP.exe recovery-confirm --operation operation-123\n"; message != want {
		t.Fatalf("restore success message = %q, want %q", message, want)
	}
}
