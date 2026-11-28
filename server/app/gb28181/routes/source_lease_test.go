package routes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOtherConsumerSourceLeaseCheckerIncludesCascadeAndRecordCacheOnly(t *testing.T) {
	sourceLeaseMu.Lock()
	previousCascade := cascadeSourceLeaseChecker
	previousPlan := recordingPlanSourceLeaseChecker
	previousCache := recordCacheSourceLeaseChecker
	sourceLeaseMu.Unlock()
	t.Cleanup(func() {
		SetCascadeSourceLeaseChecker(previousCascade)
		SetRecordingPlanSourceLeaseChecker(previousPlan)
		SetRecordCacheSourceLeaseChecker(previousCache)
	})

	SetCascadeSourceLeaseChecker(sourceLeaseAnswer(false))
	SetRecordingPlanSourceLeaseChecker(sourceLeaseAnswer(true))
	SetRecordCacheSourceLeaseChecker(sourceLeaseAnswer(false))
	checker := OtherConsumerSourceLeaseChecker()
	require.False(t, checker.HasLease("stream"), "the plan must not count its own lease during release")

	SetCascadeSourceLeaseChecker(sourceLeaseAnswer(true))
	require.True(t, checker.HasLease("stream"), "cascade ownership must preserve the source")

	SetCascadeSourceLeaseChecker(sourceLeaseAnswer(false))
	SetRecordCacheSourceLeaseChecker(sourceLeaseAnswer(true))
	require.True(t, checker.HasLease("stream"), "record-cache ownership must preserve the source")
}

type sourceLeaseAnswer bool

func (answer sourceLeaseAnswer) HasLease(string) bool { return bool(answer) }
