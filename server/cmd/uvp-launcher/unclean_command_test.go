package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestUncleanRecoveryCommandPassesPathsAndSnapshotToCore(t *testing.T) {
	const (
		installDir  = "isolated-install"
		recordings  = "external-recordings"
		snapshot    = "outside-snapshot"
		operationID = "operation-123"
	)
	var (
		out, diagnostic                     bytes.Buffer
		gotRoot, gotRecordings, gotSnapshot string
	)
	runner := func(ctx context.Context, root, gotRecordingsDir, gotSnapshotDir string) (standalone.UncleanRecoveryResult, error) {
		require.NotNil(t, ctx)
		gotRoot, gotRecordings, gotSnapshot = root, gotRecordingsDir, gotSnapshotDir
		return standalone.UncleanRecoveryResult{OperationID: operationID, AwaitingLocalConfirmation: true}, nil
	}

	code := runUncleanRecoveryCommandWith([]string{
		"--install-dir", installDir,
		"--recordings-dir", recordings,
		"--snapshot", snapshot,
	}, "default-install", &out, &diagnostic, runner)

	require.Equal(t, 0, code)
	require.Equal(t, installDir, gotRoot)
	require.Equal(t, recordings, gotRecordings)
	require.Equal(t, snapshot, gotSnapshot)
	require.Empty(t, diagnostic.String())
	require.Contains(t, out.String(), "恢复已完成，等待本机确认："+operationID)
	require.Contains(t, out.String(), "UVP.exe recovery-confirm --operation "+operationID)
}

func TestUncleanRecoveryCommandAllowsResumeWithoutSnapshot(t *testing.T) {
	var (
		out, diagnostic bytes.Buffer
		gotSnapshot     string
	)
	runner := func(_ context.Context, _, _, snapshot string) (standalone.UncleanRecoveryResult, error) {
		gotSnapshot = snapshot
		return standalone.UncleanRecoveryResult{OperationID: "operation-resume", AwaitingLocalConfirmation: true}, nil
	}

	code := runUncleanRecoveryCommandWith([]string{"--install-dir", "isolated-install"}, "default-install", &out, &diagnostic, runner)

	require.Equal(t, 0, code)
	require.Empty(t, gotSnapshot)
	require.Empty(t, diagnostic.String())
	require.Contains(t, out.String(), "operation-resume")
}

func TestUncleanRecoveryCommandReportsPristineRestart(t *testing.T) {
	var out, diagnostic bytes.Buffer
	runner := func(context.Context, string, string, string) (standalone.UncleanRecoveryResult, error) {
		return standalone.UncleanRecoveryResult{OperationID: "operation-pristine", AwaitingLocalConfirmation: false}, nil
	}

	code := runUncleanRecoveryCommandWith(nil, "isolated-install", &out, &diagnostic, runner)

	require.Equal(t, 0, code)
	require.Empty(t, diagnostic.String())
	require.Contains(t, out.String(), "请重新启动 UVP 继续首装")
	require.NotContains(t, out.String(), "recovery-confirm")
}

func TestUncleanRecoveryCommandDoesNotReportSuccessWhenCoreFails(t *testing.T) {
	var out, diagnostic bytes.Buffer
	runner := func(context.Context, string, string, string) (standalone.UncleanRecoveryResult, error) {
		return standalone.UncleanRecoveryResult{}, errors.New("recovery precondition failed")
	}

	code := runUncleanRecoveryCommandWith(nil, "isolated-install", &out, &diagnostic, runner)

	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, diagnostic.String(), "恢复未完成")
	require.NotContains(t, diagnostic.String(), "recovery-confirm")
}

func TestUncleanRecoveryCommandRejectsArgumentsBeforeCore(t *testing.T) {
	for _, args := range [][]string{
		{"unexpected"},
		{"--trust", "anything"},
		{"--install-dir", "isolated-install", "extra"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			called := false
			runner := func(context.Context, string, string, string) (standalone.UncleanRecoveryResult, error) {
				called = true
				return standalone.UncleanRecoveryResult{}, nil
			}

			code := runUncleanRecoveryCommandWith(args, "default-install", &out, &diagnostic, runner)

			require.Equal(t, 1, code)
			require.Empty(t, out.String())
			require.NotEmpty(t, diagnostic.String())
			require.False(t, called)
		})
	}
}

func TestUncleanRecoveryMessagesKeepOperationBound(t *testing.T) {
	require.Equal(t,
		"恢复已完成，等待本机确认：operation-123\n请运行：UVP.exe recovery-confirm --operation operation-123\n",
		uncleanRecoveryConfirmationMessage("operation-123"))
	require.Contains(t, uncleanRecoveryPristineMessage(), "请重新启动 UVP 继续首装")
}
