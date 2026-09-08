package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestLaunchFailurePromptsRecoveryAndDoesNotClaimStartup(t *testing.T) {
	var diagnostic bytes.Buffer

	writeLaunchFailure(&diagnostic, errors.Join(standalone.ErrUncleanRecoveryRequired, errors.New("marker retained")))

	require.Contains(t, diagnostic.String(), "UVP.exe recover")
	require.Contains(t, diagnostic.String(), "--snapshot")
	require.NotContains(t, diagnostic.String(), "启动器退出")
}

func TestLaunchFailureKeepsOrdinaryErrorDiagnostic(t *testing.T) {
	var diagnostic bytes.Buffer

	writeLaunchFailure(&diagnostic, errors.New("ordinary startup failure"))

	require.Contains(t, diagnostic.String(), "启动器退出")
	require.Contains(t, diagnostic.String(), "ordinary startup failure")
}
