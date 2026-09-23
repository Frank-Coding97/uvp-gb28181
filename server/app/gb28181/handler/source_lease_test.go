package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCombinedSourceLeaseCheckerUsesAnyNonBrowserConsumer(t *testing.T) {
	combined := CombinedSourceLeaseChecker{sourceLeaseAnswer(false), sourceLeaseAnswer(true)}
	require.True(t, combined.HasLease("stream"))
	combined = CombinedSourceLeaseChecker{sourceLeaseAnswer(false), nil}
	require.False(t, combined.HasLease("stream"))
}

type sourceLeaseAnswer bool

func (answer sourceLeaseAnswer) HasLease(string) bool { return bool(answer) }
