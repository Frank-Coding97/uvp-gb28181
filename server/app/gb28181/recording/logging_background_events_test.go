package recording

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestLoggingBackgroundEvents(t *testing.T) {
	core, entries := observer.New(zap.DebugLevel)
	root := zap.New(core)
	previous := app.ZapLog
	app.ZapLog = root
	t.Cleanup(func() { app.ZapLog = previous })

	scheduler := &CatalogReconcileScheduler{}
	scheduler.recordFailure("run-node:node=7", errors.New("catalog failure"), context.Background())

	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "recording.catalog", entry.LoggerName)
	require.Equal(t, "recording.catalog.reconcile_failed", entry.ContextMap()["event"])
	require.Equal(t, "run-node:node=7", entry.ContextMap()["scope"])
	require.NotContains(t, entry.Message, "catalog failure")
	count, last := scheduler.FailureStats()
	require.EqualValues(t, 1, count)
	require.Contains(t, last, "run-node:node=7")
}
